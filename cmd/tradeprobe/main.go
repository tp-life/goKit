package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"goKit/internal/appconfig"
	"goKit/internal/infrastructure/exchange"

	xproxy "golang.org/x/net/proxy"
)

type options struct {
	action          string
	exchange        string
	symbol          string
	canonicalSymbol string
	assetID         string
	side            string
	orderType       string
	timeInForce     string
	clientOrderID   string
	venueOrderID    string
	qty             float64
	price           float64
	reduceOnly      bool
	confirm         bool
	forceEnable     bool
	timeout         time.Duration
	waitStatus      bool
	statusTimeout   time.Duration
	statusInterval  time.Duration
	showEgressIP    bool
}

func main() {
	opts := parseFlags()
	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "tradeprobe error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.action, "action", "inspect", "inspect|place|status|position|account|preflight")
	flag.StringVar(&opts.exchange, "exchange", "", "exchange name: binance|aster|bybit|hyperliquid")
	flag.StringVar(&opts.symbol, "symbol", "", "venue symbol, e.g. BTCUSDT")
	flag.StringVar(&opts.canonicalSymbol, "canonical-symbol", "", "canonical symbol, defaults to a best-effort value from --symbol")
	flag.StringVar(&opts.assetID, "asset-id", "", "venue asset id, required by hyperliquid order placement")
	flag.StringVar(&opts.side, "side", "", "BUY or SELL")
	flag.StringVar(&opts.orderType, "type", "MARKET", "order type, e.g. MARKET or LIMIT")
	flag.StringVar(&opts.timeInForce, "tif", "", "time in force, e.g. GTC/IOC/GTX")
	flag.StringVar(&opts.clientOrderID, "client-order-id", "", "client order id for place/status")
	flag.StringVar(&opts.venueOrderID, "venue-order-id", "", "venue order id for status")
	flag.Float64Var(&opts.qty, "qty", 0, "order quantity")
	flag.Float64Var(&opts.price, "price", 0, "order price; required for LIMIT orders and hyperliquid probes")
	flag.BoolVar(&opts.reduceOnly, "reduce-only", false, "whether the order is reduce-only")
	flag.BoolVar(&opts.confirm, "confirm", false, "actually send the live order for action=place")
	flag.BoolVar(&opts.forceEnable, "force-enable", false, "temporarily enable the target exchange even if configs/config.yaml has enabled=false")
	flag.DurationVar(&opts.timeout, "timeout", 20*time.Second, "request timeout")
	flag.BoolVar(&opts.waitStatus, "wait-status", false, "after a confirmed place, poll order status until terminal or timeout")
	flag.DurationVar(&opts.statusTimeout, "status-timeout", 30*time.Second, "maximum time to wait for order status polling")
	flag.DurationVar(&opts.statusInterval, "status-interval", 1500*time.Millisecond, "poll interval for --wait-status")
	flag.BoolVar(&opts.showEgressIP, "show-egress-ip", false, "detect and print the outbound IP used with this exchange's proxy/network settings")
	flag.Parse()
	return opts
}

func run(opts options) error {
	cfg, err := appconfig.Load()
	if err != nil {
		return err
	}

	name := normalizeName(opts.exchange)
	if opts.forceEnable {
		if name == "" {
			return fmt.Errorf("--force-enable requires --exchange")
		}
		if err := setExchangeEnabled(cfg, name, true); err != nil {
			return err
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	adapters, err := buildTradeAdapters(cfg.Exchanges, logger)
	if err != nil {
		return err
	}
	markets, err := buildMarketAdapters(cfg.Exchanges, logger)
	if err != nil {
		return err
	}
	adapterMap := exchange.BuildTradeMap(adapters)
	marketMap := exchange.BuildMarketMap(markets)

	switch strings.ToLower(strings.TrimSpace(opts.action)) {
	case "inspect":
		return runInspect(cfg.Exchanges, adapterMap, adapters, name)
	case "preflight":
		return runPreflight(cfg.Exchanges, opts)
	case "place":
		return runPlace(cfg, adapterMap, marketMap, opts)
	case "status":
		return runStatus(cfg.Exchanges, adapterMap, opts)
	case "position":
		return runPosition(cfg.Exchanges, adapterMap, opts)
	case "account":
		return runAccount(cfg.Exchanges, adapterMap, opts)
	default:
		return fmt.Errorf("unsupported action %q", opts.action)
	}
}

func runPreflight(cfg exchange.ConfigSet, opts options) error {
	name := normalizeName(opts.exchange)
	if name == "" {
		name = "binance"
	}
	exCfg, ok := lookupExchangeConfig(cfg, name)
	if !ok {
		return fmt.Errorf("exchange %s not found in config", name)
	}
	switch name {
	case "binance":
		ctx, cancel := context.WithTimeout(context.Background(), minDuration(opts.timeout, 20*time.Second))
		defer cancel()
		result := exchange.RunBinancePreflight(ctx, name, exCfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
		return printJSON(result)
	default:
		return fmt.Errorf("preflight is currently implemented only for binance, got %s", name)
	}
}

func runInspect(cfg exchange.ConfigSet, adapterMap map[string]exchange.TradeAdapter, adapters []exchange.TradeAdapter, name string) error {
	inspections := exchange.InspectTradeAdapters(cfg, adapters)
	if name != "" {
		filtered := inspections[:0]
		for _, item := range inspections {
			if item.Exchange == name {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("exchange %s not found in config", name)
		}
		inspections = filtered
	}
	if name != "" {
		if _, ok := adapterMap[name]; !ok {
			return fmt.Errorf("exchange %s trade adapter not built", name)
		}
	}
	return printJSON(inspections)
}

func runPlace(cfg *appconfig.AppConfig, adapterMap map[string]exchange.TradeAdapter, marketMap map[string]exchange.MarketAdapter, opts options) error {
	name, adapter, err := requireAdapter(adapterMap, opts.exchange)
	if err != nil {
		return err
	}
	if err := validatePlaceOptions(name, opts); err != nil {
		return err
	}
	probe, probeErr := maybeDetectEgressIP(cfg.Exchanges, name, opts)

	req := exchange.TradeOrderRequest{
		CanonicalSymbol: resolveCanonicalSymbol(opts.canonicalSymbol, opts.symbol),
		VenueSymbol:     strings.TrimSpace(opts.symbol),
		AssetID:         strings.TrimSpace(opts.assetID),
		Side:            strings.ToUpper(strings.TrimSpace(opts.side)),
		OrderType:       strings.ToUpper(strings.TrimSpace(opts.orderType)),
		TimeInForce:     strings.ToUpper(strings.TrimSpace(opts.timeInForce)),
		Quantity:        opts.qty,
		Price:           opts.price,
		ReduceOnly:      opts.reduceOnly,
		ClientOrderID:   firstNonEmpty(strings.TrimSpace(opts.clientOrderID), buildProbeClientOrderID(name)),
		Reason:          "manual_probe",
	}

	if !opts.confirm {
		payload := map[string]any{
			"action":                "place_preview",
			"exchange":              name,
			"adapter_enabled":       adapter.Enabled(),
			"wait_status_requested": opts.waitStatus,
			"request":               req,
			"message":               "preview only; rerun with --confirm to place the live order",
		}
		attachProbe(payload, probe, probeErr)
		return printJSON(payload)
	}
	if !adapter.Enabled() {
		return wrapWithProbe(fmt.Errorf("exchange %s adapter is not enabled at runtime; run --action inspect --exchange %s to see missing config/credentials", name, name), probe, probeErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	if err := validateVenueOrderRules(ctx, cfg, marketMap, name, req, opts); err != nil {
		return wrapWithProbe(err, probe, probeErr)
	}

	result, err := adapter.PlaceOrder(ctx, req)
	if err != nil {
		return wrapWithProbe(err, probe, probeErr)
	}

	if opts.waitStatus {
		poll, pollErr := awaitOrderStatus(adapter, exchange.OrderLookupRequest{
			CanonicalSymbol: req.CanonicalSymbol,
			VenueSymbol:     req.VenueSymbol,
			AssetID:         req.AssetID,
			ClientOrderID:   firstNonEmpty(result.ClientOrderID, req.ClientOrderID),
			VenueOrderID:    result.VenueOrderID,
		}, opts.statusTimeout, opts.statusInterval)
		if pollErr != nil {
			payload := map[string]any{
				"action": "place",
				"result": result,
				"status_poll": map[string]any{
					"ok":    false,
					"error": pollErr.Error(),
				},
			}
			attachProbe(payload, probe, probeErr)
			return printJSON(payload)
		}
		payload := map[string]any{
			"action":      "place",
			"result":      result,
			"status_poll": poll,
		}
		attachProbe(payload, probe, probeErr)
		return printJSON(payload)
	}

	payload := map[string]any{
		"action": "place",
		"result": result,
	}
	attachProbe(payload, probe, probeErr)
	return printJSON(payload)
}

func runStatus(cfg exchange.ConfigSet, adapterMap map[string]exchange.TradeAdapter, opts options) error {
	name, adapter, err := requireAdapter(adapterMap, opts.exchange)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.clientOrderID) == "" && strings.TrimSpace(opts.venueOrderID) == "" {
		return errors.New("status requires --client-order-id or --venue-order-id")
	}
	if !adapter.Enabled() {
		return fmt.Errorf("exchange %s adapter is not enabled at runtime", name)
	}
	probe, probeErr := maybeDetectEgressIP(cfg, name, opts)

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	result, err := adapter.GetOrderStatus(ctx, exchange.OrderLookupRequest{
		CanonicalSymbol: resolveCanonicalSymbol(opts.canonicalSymbol, opts.symbol),
		VenueSymbol:     strings.TrimSpace(opts.symbol),
		AssetID:         strings.TrimSpace(opts.assetID),
		ClientOrderID:   strings.TrimSpace(opts.clientOrderID),
		VenueOrderID:    strings.TrimSpace(opts.venueOrderID),
	})
	if err != nil {
		return wrapWithProbe(err, probe, probeErr)
	}
	payload := map[string]any{
		"action": "status",
		"result": result,
	}
	attachProbe(payload, probe, probeErr)
	return printJSON(payload)
}

func runPosition(cfg exchange.ConfigSet, adapterMap map[string]exchange.TradeAdapter, opts options) error {
	name, adapter, err := requireAdapter(adapterMap, opts.exchange)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.symbol) == "" {
		return errors.New("position requires --symbol")
	}
	if !adapter.Enabled() {
		return fmt.Errorf("exchange %s adapter is not enabled at runtime", name)
	}
	probe, probeErr := maybeDetectEgressIP(cfg, name, opts)

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	result, err := adapter.GetPosition(ctx, resolveCanonicalSymbol(opts.canonicalSymbol, opts.symbol), strings.TrimSpace(opts.symbol), strings.TrimSpace(opts.assetID))
	if err != nil {
		return wrapWithProbe(err, probe, probeErr)
	}
	payload := map[string]any{
		"action": "position",
		"result": result,
	}
	attachProbe(payload, probe, probeErr)
	return printJSON(payload)
}

func runAccount(cfg exchange.ConfigSet, adapterMap map[string]exchange.TradeAdapter, opts options) error {
	name, adapter, err := requireAdapter(adapterMap, opts.exchange)
	if err != nil {
		return err
	}
	if !adapter.Enabled() {
		return fmt.Errorf("exchange %s adapter is not enabled at runtime", name)
	}
	probe, probeErr := maybeDetectEgressIP(cfg, name, opts)

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	result, err := adapter.GetAccountSnapshot(ctx)
	if err != nil {
		return wrapWithProbe(err, probe, probeErr)
	}
	payload := map[string]any{
		"action": "account",
		"result": result,
	}
	attachProbe(payload, probe, probeErr)
	return printJSON(payload)
}

func buildTradeAdapters(cfg exchange.ConfigSet, logger *slog.Logger) ([]exchange.TradeAdapter, error) {
	registry := exchange.NewAdapterRegistry(exchange.DefaultAdapterFactories()...)
	return registry.BuildTrades(cfg, logger)
}

func buildMarketAdapters(cfg exchange.ConfigSet, logger *slog.Logger) ([]exchange.MarketAdapter, error) {
	registry := exchange.NewAdapterRegistry(exchange.DefaultAdapterFactories()...)
	return registry.BuildMarkets(cfg, logger)
}

func maybeDetectEgressIP(cfg exchange.ConfigSet, exchangeName string, opts options) (*exchange.NetworkProbe, error) {
	if !opts.showEgressIP {
		return nil, nil
	}
	name := normalizeName(exchangeName)
	if name == "" {
		return nil, errors.New("missing exchange for egress probe")
	}
	exCfg, ok := lookupExchangeConfig(cfg, name)
	if !ok {
		return nil, fmt.Errorf("exchange %s not found in config", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), minDuration(opts.timeout, 10*time.Second))
	defer cancel()
	probe, err := exchange.DetectTradeEgressIP(ctx, name, exCfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		return &probe, err
	}
	return &probe, nil
}

func requireAdapter(adapterMap map[string]exchange.TradeAdapter, exchangeName string) (string, exchange.TradeAdapter, error) {
	name := normalizeName(exchangeName)
	if name == "" {
		return "", nil, errors.New("missing --exchange")
	}
	adapter, ok := adapterMap[name]
	if !ok || adapter == nil {
		return "", nil, fmt.Errorf("exchange %s trade adapter not found", name)
	}
	return name, adapter, nil
}

func validatePlaceOptions(name string, opts options) error {
	if strings.TrimSpace(opts.symbol) == "" {
		return errors.New("place requires --symbol")
	}
	if strings.TrimSpace(opts.side) == "" {
		return errors.New("place requires --side")
	}
	if opts.qty <= 0 {
		return errors.New("place requires --qty > 0")
	}
	orderType := strings.ToUpper(strings.TrimSpace(opts.orderType))
	if orderType == "LIMIT" && opts.price <= 0 {
		return errors.New("LIMIT order requires --price > 0")
	}
	if name == "hyperliquid" {
		if strings.TrimSpace(opts.assetID) == "" {
			return errors.New("hyperliquid place probe requires --asset-id")
		}
		if opts.price <= 0 {
			return errors.New("hyperliquid place probe requires --price > 0")
		}
	}
	if opts.waitStatus {
		if opts.statusTimeout <= 0 {
			return errors.New("--status-timeout must be > 0 when --wait-status is set")
		}
		if opts.statusInterval <= 0 {
			return errors.New("--status-interval must be > 0 when --wait-status is set")
		}
	}
	return nil
}

type statusPollResult struct {
	OK            bool                        `json:"ok"`
	Terminal      bool                        `json:"terminal"`
	Polls         int                         `json:"polls"`
	Elapsed       string                      `json:"elapsed"`
	OrderLookupID exchange.OrderLookupRequest `json:"order_lookup"`
	LastStatus    *exchange.OrderStatus       `json:"last_status,omitempty"`
}

func awaitOrderStatus(adapter exchange.TradeAdapter, req exchange.OrderLookupRequest, timeout, interval time.Duration) (statusPollResult, error) {
	start := time.Now()
	result := statusPollResult{
		OK:            true,
		OrderLookupID: req,
	}
	if adapter == nil {
		return result, errors.New("nil trade adapter")
	}
	if strings.TrimSpace(req.ClientOrderID) == "" && strings.TrimSpace(req.VenueOrderID) == "" {
		return result, errors.New("no client or venue order id available for status polling")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		status, err := adapter.GetOrderStatus(ctx, req)
		if err == nil {
			result.Polls++
			result.LastStatus = &status
			result.Terminal = status.Terminal
			result.Elapsed = time.Since(start).String()
			if status.Terminal {
				return result, nil
			}
		}

		select {
		case <-ctx.Done():
			result.Elapsed = time.Since(start).String()
			if err == nil {
				return result, fmt.Errorf("status polling timed out after %s", timeout)
			}
			return result, fmt.Errorf("status polling failed before timeout: %w", err)
		case <-time.After(interval):
		}
	}
}

func setExchangeEnabled(cfg *appconfig.AppConfig, name string, enabled bool) error {
	switch normalizeName(name) {
	case "binance":
		cfg.Exchanges.Binance.Enabled = enabled
	case "aster":
		cfg.Exchanges.Aster.Enabled = enabled
	case "hyperliquid":
		cfg.Exchanges.Hyperliquid.Enabled = enabled
	default:
		if cfg.Exchanges.Additional == nil {
			return fmt.Errorf("exchange %s not found in config", name)
		}
		item, ok := cfg.Exchanges.Additional[name]
		if !ok {
			return fmt.Errorf("exchange %s not found in config", name)
		}
		item.Enabled = enabled
		cfg.Exchanges.Additional[name] = item
	}
	return nil
}

func lookupExchangeConfig(cfg exchange.ConfigSet, name string) (exchange.ExchangeConfig, bool) {
	items := cfg.Items()
	item, ok := items[normalizeName(name)]
	return item, ok
}

func resolveCanonicalSymbol(explicit, symbol string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.ToUpper(strings.TrimSpace(explicit))
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	for _, suffix := range []string{"USDT", "USDC", "USD"} {
		if strings.HasSuffix(symbol, suffix) && len(symbol) > len(suffix) {
			return strings.TrimSuffix(symbol, suffix)
		}
	}
	return symbol
}

func buildProbeClientOrderID(exchangeName string) string {
	return fmt.Sprintf("probe-%s-%d", normalizeName(exchangeName), time.Now().UnixMilli())
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func attachProbe(payload map[string]any, probe *exchange.NetworkProbe, probeErr error) {
	if probe == nil && probeErr == nil {
		return
	}
	if probe != nil {
		payload["egress_probe"] = probe
	}
	if probeErr != nil {
		payload["egress_probe_error"] = probeErr.Error()
	}
}

func wrapWithProbe(err error, probe *exchange.NetworkProbe, probeErr error) error {
	if err == nil {
		return nil
	}
	if probe == nil && probeErr == nil {
		return err
	}
	parts := []string{err.Error()}
	if probe != nil {
		if probe.EgressIP != "" {
			parts = append(parts, fmt.Sprintf("egress_ip=%s", probe.EgressIP))
		}
		if probe.ProxyURL != "" {
			parts = append(parts, fmt.Sprintf("proxy=%s", probe.ProxyURL))
		}
		if probe.ServiceURL != "" {
			parts = append(parts, fmt.Sprintf("probe_service=%s", probe.ServiceURL))
		}
	}
	if probeErr != nil {
		parts = append(parts, fmt.Sprintf("egress_probe_error=%v", probeErr))
	}
	return errors.New(strings.Join(parts, "; "))
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type venueOrderRules struct {
	VenueSymbol   string
	MinQty        float64
	StepSize      float64
	MinNotional   float64
	ReferencePx   float64
	OrderNotional float64
}

func validateVenueOrderRules(ctx context.Context, cfg *appconfig.AppConfig, marketMap map[string]exchange.MarketAdapter, exchangeName string, req exchange.TradeOrderRequest, opts options) error {
	if cfg == nil || req.ReduceOnly {
		return nil
	}
	switch exchangeName {
	case "binance", "aster":
	default:
		return nil
	}

	market := marketMap[exchangeName]
	if market == nil {
		return nil
	}

	rules, err := fetchVenueOrderRules(ctx, market, cfg.Strategy.QuoteAsset, req, opts)
	if err != nil || rules == nil {
		return nil
	}
	if rules.MinNotional <= 0 || rules.ReferencePx <= 0 || req.Quantity <= 0 {
		return nil
	}
	if rules.OrderNotional+1e-9 >= rules.MinNotional {
		return nil
	}

	suggestedQty := suggestQuantityForMinNotional(rules.MinNotional, rules.ReferencePx, rules.StepSize, rules.MinQty)
	return fmt.Errorf(
		"%s local order validation failed: %s estimated notional is %.2f USDT, below min_notional %.2f USDT; qty_step=%.6f min_qty=%.6f; try --qty %.3f or higher",
		exchangeName,
		req.VenueSymbol,
		rules.OrderNotional,
		rules.MinNotional,
		rules.StepSize,
		rules.MinQty,
		suggestedQty,
	)
}

func fetchVenueOrderRules(ctx context.Context, market exchange.MarketAdapter, quoteAsset string, req exchange.TradeOrderRequest, opts options) (*venueOrderRules, error) {
	allowed := map[string]struct{}{
		strings.ToUpper(strings.TrimSpace(req.VenueSymbol)):     {},
		strings.ToUpper(strings.TrimSpace(req.CanonicalSymbol)): {},
	}
	symbols, err := market.FetchTradableSymbols(ctx, quoteAsset, allowed)
	if err != nil {
		return nil, err
	}

	var matched *venueOrderRules
	for _, item := range symbols {
		if !strings.EqualFold(item.VenueSymbol, req.VenueSymbol) {
			continue
		}
		matched = &venueOrderRules{
			VenueSymbol: item.VenueSymbol,
			MinQty:      parseFloatOrZero(item.MinQty),
			StepSize:    parseFloatOrZero(item.StepSize),
			MinNotional: parseFloatOrZero(item.MinNotional),
		}
		break
	}
	if matched == nil {
		return nil, nil
	}

	orderType := strings.ToUpper(strings.TrimSpace(req.OrderType))
	if orderType == "LIMIT" && req.Price > 0 {
		matched.ReferencePx = req.Price
	} else {
		referencePx, err := fetchBinanceLikeReferencePrice(ctx, market, req.VenueSymbol, opts.timeout)
		if err != nil {
			return matched, nil
		}
		matched.ReferencePx = referencePx
	}
	if matched.ReferencePx > 0 {
		matched.OrderNotional = req.Quantity * matched.ReferencePx
	}
	return matched, nil
}

func fetchBinanceLikeReferencePrice(ctx context.Context, market exchange.MarketAdapter, venueSymbol string, timeout time.Duration) (float64, error) {
	cfg := market.Config()
	client := newProbeHTTPClient(cfg, minDuration(timeout, 10*time.Second))
	endpoint := strings.TrimRight(cfg.RestBaseURL, "/") + "/fapi/v1/premiumIndex?symbol=" + url.QueryEscape(strings.TrimSpace(venueSymbol))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("premiumIndex status=%d", resp.StatusCode)
	}
	var payload struct {
		MarkPrice string `json:"markPrice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return parseFloatOrZero(payload.MarkPrice), nil
}

func newProbeHTTPClient(cfg exchange.ExchangeConfig, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 30 * time.Second,
	}
	applyProbeProxyToHTTPTransport(transport, cfg.Proxy)
	return &http.Client{Timeout: timeout, Transport: transport}
}

func applyProbeProxyToHTTPTransport(transport *http.Transport, proxyCfg exchange.ProxyConfig) {
	if !proxyCfg.Enabled || strings.TrimSpace(proxyCfg.URL) == "" {
		return
	}
	proxyURL, err := url.Parse(proxyCfg.URL)
	if err != nil {
		return
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		dialer, err := xproxy.FromURL(proxyURL, xproxy.Direct)
		if err != nil {
			return
		}
		transport.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}
}

func suggestQuantityForMinNotional(minNotional, referencePrice, stepSize, minQty float64) float64 {
	if referencePrice <= 0 {
		return minQty
	}
	qty := minNotional / referencePrice
	if stepSize > 0 {
		qty = math.Ceil(qty/stepSize) * stepSize
	}
	if qty < minQty {
		qty = minQty
	}
	if stepSize > 0 && qty*referencePrice < minNotional {
		qty += stepSize
	}
	return qty
}

func parseFloatOrZero(v string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0
	}
	return value
}
