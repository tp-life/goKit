package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CEXTradeClient struct {
	name         string
	cfg          ExchangeConfig
	logger       *slog.Logger
	httpClient   *http.Client
	apiKey       string
	apiSecret    string
	orderPath    string
	positionPath string
	accountPath  string
}

func NewBinanceTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig("binance", cfg.Binance)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://fapi.binance.com"
	}
	appCfg := loadAppConfig()
	return &CEXTradeClient{
		name:         "binance",
		cfg:          c,
		logger:       logger,
		httpClient:   newHTTPClient(c, appCfg, logger, "binance-trade"),
		apiKey:       readEnvByName(c.Auth.APIKeyEnv),
		apiSecret:    readEnvByName(c.Auth.APISecretEnv),
		orderPath:    "/fapi/v1/order",
		positionPath: "/fapi/v2/positionRisk",
		accountPath:  "/fapi/v2/account",
	}
}

func NewAsterTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig("aster", cfg.Aster)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://fapi.asterdex.com"
	}
	appCfg := loadAppConfig()
	return &CEXTradeClient{
		name:         "aster",
		cfg:          c,
		logger:       logger,
		httpClient:   newHTTPClient(c, appCfg, logger, "aster-trade"),
		apiKey:       readEnvByName(c.Auth.APIKeyEnv),
		apiSecret:    readEnvByName(c.Auth.APISecretEnv),
		orderPath:    "/fapi/v3/order",
		positionPath: "/fapi/v3/positionRisk",
		accountPath:  "/fapi/v3/account",
	}
}

func (c *CEXTradeClient) Name() string  { return c.name }
func (c *CEXTradeClient) Enabled() bool { return c.cfg.Enabled && c.apiKey != "" && c.apiSecret != "" }

func (c *CEXTradeClient) PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	if !c.Enabled() {
		return TradeOrderResult{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	params.Set("side", strings.ToUpper(req.Side))
	params.Set("quantity", formatFloat(req.Quantity, 8))
	params.Set("newOrderRespType", "RESULT")
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if req.ClientOrderID != "" {
		params.Set("newClientOrderId", req.ClientOrderID)
	}
	orderType := strings.ToUpper(req.OrderType)
	if orderType == "" {
		orderType = "MARKET"
	}
	params.Set("type", orderType)
	if orderType == "LIMIT" {
		params.Set("price", formatFloat(req.Price, 8))
		tif := strings.ToUpper(strings.TrimSpace(req.TimeInForce))
		if tif == "" {
			tif = "GTC"
		}
		params.Set("timeInForce", tif)
	}
	if req.ReduceOnly {
		params.Set("reduceOnly", "true")
	}
	var respPayload map[string]any
	raw, err := c.signedPOST(ctx, c.orderPath, params, &respPayload)
	if err != nil {
		return TradeOrderResult{}, err
	}
	return TradeOrderResult{
		Exchange:        c.name,
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   asString(respPayload["clientOrderId"]),
		VenueOrderID:    asString(respPayload["orderId"]),
		Status:          asString(respPayload["status"]),
		ExecutedQty:     parseNullableFloat(respPayload["executedQty"]),
		AveragePrice:    parseNullableFloat(respPayload["avgPrice"]),
		RawResponse:     raw,
	}, nil
}

func (c *CEXTradeClient) ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	pos, err := c.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err != nil {
		return TradeOrderResult{}, err
	}
	qty := pos.Quantity
	if qty == 0 {
		return TradeOrderResult{Exchange: c.name, CanonicalSymbol: req.CanonicalSymbol, VenueSymbol: req.VenueSymbol, Status: "NO_POSITION"}, nil
	}
	side := "SELL"
	if qty < 0 {
		side = "BUY"
		qty = -qty
	}
	return c.PlaceOrder(ctx, TradeOrderRequest{
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		Side:            side,
		OrderType:       firstNonEmpty(req.OrderType, "MARKET"),
		TimeInForce:     req.TimeInForce,
		Quantity:        qty,
		Price:           req.Price,
		ReduceOnly:      true,
		ClientOrderID:   req.ClientOrderID,
		Reason:          req.Reason,
	})
}

func (c *CEXTradeClient) GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error) {
	if !c.Enabled() {
		return OrderStatus{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if req.VenueOrderID != "" {
		params.Set("orderId", req.VenueOrderID)
	} else if req.ClientOrderID != "" {
		params.Set("origClientOrderId", req.ClientOrderID)
	} else {
		return OrderStatus{}, fmt.Errorf("missing order lookup id")
	}
	var payload map[string]any
	raw, err := c.signedGET(ctx, c.orderPath, params, &payload)
	if err != nil {
		return OrderStatus{}, err
	}
	status := strings.ToUpper(asString(payload["status"]))
	return OrderStatus{
		Exchange:      c.name,
		Status:        status,
		ExecutedQty:   parseNullableFloat(payload["executedQty"]),
		AveragePrice:  parseNullableFloat(payload["avgPrice"]),
		VenueOrderID:  firstNonEmpty(asString(payload["orderId"]), req.VenueOrderID),
		ClientOrderID: firstNonEmpty(asString(payload["clientOrderId"]), req.ClientOrderID),
		Terminal:      isTerminalOrderStatus(status),
		Canceled:      status == "CANCELED" || status == "EXPIRED",
		RawResponse:   raw,
	}, nil
}

func (c *CEXTradeClient) GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	if !c.Enabled() {
		return AccountSnapshot{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	var payload map[string]any
	raw, err := c.signedGET(ctx, c.accountPath, params, &payload)
	if err != nil {
		return AccountSnapshot{}, err
	}
	return AccountSnapshot{
		Exchange:         c.name,
		Equity:           firstPositive(parseNullableFloat(payload["totalMarginBalance"]), parseNullableFloat(payload["totalWalletBalance"])),
		AvailableBalance: parseNullableFloat(payload["availableBalance"]),
		MarginUsed:       parseNullableFloat(payload["totalInitialMargin"]),
		RawResponse:      raw,
	}, nil
}

func (c *CEXTradeClient) GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, _ string) (Position, error) {
	if !c.Enabled() {
		return Position{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	var payload []map[string]any
	_, err := c.signedGET(ctx, c.positionPath, params, &payload)
	if err != nil {
		return Position{}, err
	}
	for _, item := range payload {
		if !strings.EqualFold(asString(item["symbol"]), venueSymbol) {
			continue
		}
		return Position{
			Exchange:      c.name,
			Symbol:        canonicalSymbol,
			VenueSymbol:   venueSymbol,
			Quantity:      parseNullableFloat(item["positionAmt"]),
			EntryPrice:    parseNullableFloat(item["entryPrice"]),
			MarkPrice:     parseNullableFloat(item["markPrice"]),
			UnrealizedPnL: parseNullableFloat(item["unRealizedProfit"]),
		}, nil
	}
	return Position{Exchange: c.name, Symbol: canonicalSymbol, VenueSymbol: venueSymbol}, nil
}

func (c *CEXTradeClient) signedGET(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodGet, path, params, out)
}

func (c *CEXTradeClient) signedPOST(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodPost, path, params, out)
}

func (c *CEXTradeClient) signedDo(ctx context.Context, method, path string, params url.Values, out any) (string, error) {
	payload := params.Encode()
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	_, _ = mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path + "?" + payload + "&signature=" + signature
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return string(body), fmt.Errorf("%s signed request failed status=%d body=%s", c.name, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return string(body), err
		}
	}
	return string(body), nil
}

func asString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		return formatFloat(val, 0)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func isTerminalOrderStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "CANCELED", "REJECTED", "EXPIRED", "NO_POSITION":
		return true
	default:
		return false
	}
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatFloat(v float64, decimals int) string {
	format := fmt.Sprintf("%%.%df", decimals)
	text := fmt.Sprintf(format, v)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}
