package exchange

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BinanceSpotTradeClient struct {
	name          string
	cfg           ExchangeConfig
	logger        *slog.Logger
	httpClient    *http.Client
	apiKey        string
	apiSecret     string
	rsaPrivateKey *rsa.PrivateKey
	authMode      string
}

type binanceSpotAccountBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

type binanceSpotAccount struct {
	Balances []binanceSpotAccountBalance `json:"balances"`
}

func NewBinanceSpotTradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://api.binance.com"
	}
	appCfg := loadAppConfig()
	return &BinanceSpotTradeClient{
		name:          name,
		cfg:           c,
		logger:        logger,
		httpClient:    newHTTPClient(c, appCfg, logger, name+"-spot-trade"),
		apiKey:        readEnvByName(c.Auth.APIKeyEnv),
		apiSecret:     readEnvByName(c.Auth.APISecretEnv),
		rsaPrivateKey: loadRSASigner(c.Auth),
		authMode:      tradeAuthMode(name, c),
	}
}

func (c *BinanceSpotTradeClient) Name() string { return c.name }

func (c *BinanceSpotTradeClient) Enabled() bool {
	if !c.cfg.Enabled {
		return false
	}
	switch c.authMode {
	case binanceLikeTradeAuthRSA:
		return strings.TrimSpace(c.apiKey) != "" && c.rsaPrivateKey != nil
	default:
		return strings.TrimSpace(c.apiKey) != "" && strings.TrimSpace(c.apiSecret) != ""
	}
}

func (c *BinanceSpotTradeClient) Capabilities() TradeCapabilities {
	return TradeCapabilities{
		MakerLimitTIF:            "GTC",
		TakerOrderType:           "MARKET",
		TakerUsesAggressiveIOC:   false,
		SupportsOrderEventStream: false,
	}
}

func (c *BinanceSpotTradeClient) PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	if !c.Enabled() {
		return TradeOrderResult{}, fmt.Errorf("%s spot trade client disabled or missing credentials", c.name)
	}

	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	params.Set("side", strings.ToUpper(req.Side))
	params.Set("type", strings.ToUpper(firstNonEmpty(req.OrderType, "MARKET")))
	params.Set("quantity", formatFloat(req.Quantity, 8))
	params.Set("newOrderRespType", "RESULT")
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if req.ClientOrderID != "" {
		params.Set("newClientOrderId", req.ClientOrderID)
	}
	if strings.EqualFold(req.OrderType, "LIMIT") {
		params.Set("price", formatFloat(req.Price, 8))
		params.Set("timeInForce", firstNonEmpty(req.TimeInForce, "GTC"))
	}

	var payload map[string]any
	raw, err := c.signedPOST(ctx, "/api/v3/order", params, &payload)
	if err != nil {
		return TradeOrderResult{}, err
	}
	return TradeOrderResult{
		Exchange:        c.name,
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   firstNonEmpty(asString(payload["clientOrderId"]), req.ClientOrderID),
		VenueOrderID:    asString(payload["orderId"]),
		Status:          asString(payload["status"]),
		ExecutedQty:     parseNullableFloat(payload["executedQty"]),
		AveragePrice:    spotAveragePriceFromOrderPayload(payload),
		RawResponse:     raw,
	}, nil
}

func (c *BinanceSpotTradeClient) ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	pos, err := c.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err != nil {
		return TradeOrderResult{}, err
	}
	qty := mathMin(pos.Quantity, req.Quantity)
	if qty <= 0 {
		return TradeOrderResult{
			Exchange:        c.name,
			CanonicalSymbol: req.CanonicalSymbol,
			VenueSymbol:     req.VenueSymbol,
			Status:          "NO_POSITION",
		}, nil
	}
	return c.PlaceOrder(ctx, TradeOrderRequest{
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		AssetID:         req.AssetID,
		Side:            "SELL",
		OrderType:       firstNonEmpty(req.OrderType, "MARKET"),
		TimeInForce:     req.TimeInForce,
		Quantity:        qty,
		Price:           req.Price,
		ClientOrderID:   req.ClientOrderID,
		Reason:          req.Reason,
	})
}

func (c *BinanceSpotTradeClient) GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, assetID string) (Position, error) {
	account, _, err := c.accountSnapshot(ctx)
	if err != nil {
		return Position{}, err
	}
	asset := strings.ToUpper(strings.TrimSpace(assetID))
	if asset == "" {
		asset = strings.ToUpper(strings.TrimSpace(canonicalSymbol))
	}
	for _, balance := range account.Balances {
		if !strings.EqualFold(balance.Asset, asset) {
			continue
		}
		return Position{
			Exchange:    c.name,
			Symbol:      canonicalSymbol,
			VenueSymbol: venueSymbol,
			Quantity:    mustFloat(balance.Free) + mustFloat(balance.Locked),
		}, nil
	}
	return Position{Exchange: c.name, Symbol: canonicalSymbol, VenueSymbol: venueSymbol}, nil
}

func (c *BinanceSpotTradeClient) GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error) {
	if !c.Enabled() {
		return OrderStatus{}, fmt.Errorf("%s spot trade client disabled or missing credentials", c.name)
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
	raw, err := c.signedGET(ctx, "/api/v3/order", params, &payload)
	if err != nil {
		return OrderStatus{}, err
	}
	status := strings.ToUpper(asString(payload["status"]))
	return OrderStatus{
		Exchange:      c.name,
		Status:        status,
		ExecutedQty:   parseNullableFloat(payload["executedQty"]),
		AveragePrice:  spotAveragePriceFromOrderPayload(payload),
		VenueOrderID:  firstNonEmpty(asString(payload["orderId"]), req.VenueOrderID),
		ClientOrderID: firstNonEmpty(asString(payload["clientOrderId"]), req.ClientOrderID),
		Terminal:      isTerminalOrderStatus(status),
		Canceled:      status == "CANCELED" || status == "EXPIRED",
		RawResponse:   raw,
	}, nil
}

func (c *BinanceSpotTradeClient) CancelOrder(ctx context.Context, req OrderLookupRequest) error {
	if !c.Enabled() {
		return fmt.Errorf("%s spot trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if strings.TrimSpace(req.VenueOrderID) != "" {
		params.Set("orderId", req.VenueOrderID)
	} else if strings.TrimSpace(req.ClientOrderID) != "" {
		params.Set("origClientOrderId", req.ClientOrderID)
	} else {
		return fmt.Errorf("missing order cancel id")
	}
	_, err := c.signedDELETE(ctx, "/api/v3/order", params, nil)
	return err
}

func (c *BinanceSpotTradeClient) GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	account, raw, err := c.accountSnapshot(ctx)
	if err != nil {
		return AccountSnapshot{}, err
	}
	quoteAsset := strings.ToUpper(strings.TrimSpace(c.cfg.SettleAsset))
	if quoteAsset == "" {
		quoteAsset = "USDT"
	}
	freeQuote := 0.0
	totalQuote := 0.0
	for _, balance := range account.Balances {
		if !strings.EqualFold(balance.Asset, quoteAsset) {
			continue
		}
		freeQuote = mustFloat(balance.Free)
		totalQuote = freeQuote + mustFloat(balance.Locked)
		break
	}
	return AccountSnapshot{
		Exchange:         c.name,
		Equity:           totalQuote,
		AvailableBalance: freeQuote,
		MarginUsed:       0,
		RawResponse:      raw,
	}, nil
}

func (c *BinanceSpotTradeClient) accountSnapshot(ctx context.Context) (binanceSpotAccount, string, error) {
	if !c.Enabled() {
		return binanceSpotAccount{}, "", fmt.Errorf("%s spot trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	var payload binanceSpotAccount
	raw, err := c.signedGET(ctx, "/api/v3/account", params, &payload)
	return payload, raw, err
}

func (c *BinanceSpotTradeClient) signedGET(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodGet, path, params, out)
}

func (c *BinanceSpotTradeClient) signedPOST(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodPost, path, params, out)
}

func (c *BinanceSpotTradeClient) signedDELETE(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodDelete, path, params, out)
}

func (c *BinanceSpotTradeClient) signedDo(ctx context.Context, method, path string, params url.Values, out any) (string, error) {
	resp, body, err := c.performSignedRequest(ctx, method, path, params)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return string(body), fmt.Errorf("%s spot signed request failed status=%d body=%s", c.name, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return string(body), err
		}
	}
	return string(body), nil
}

func (c *BinanceSpotTradeClient) performSignedRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, []byte, error) {
	payload := params.Encode()
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path + "?" + payload
	switch c.authMode {
	case binanceLikeTradeAuthRSA:
		privateKey := c.rsaPrivateKey
		if privateKey == nil {
			return nil, nil, fmt.Errorf("%s spot rsa private key missing", c.name)
		}
		signature, err := signRSAPayload(privateKey, payload)
		if err != nil {
			return nil, nil, err
		}
		endpoint += "&signature=" + url.QueryEscape(signature)
	default:
		endpoint += "&signature=" + signLegacyHMACPayload(c.apiSecret, payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, nil, readErr
	}
	return resp, body, nil
}

func spotAveragePriceFromOrderPayload(payload map[string]any) float64 {
	executedQty := parseNullableFloat(payload["executedQty"])
	if executedQty <= 0 {
		return 0
	}
	if avgPrice := parseNullableFloat(payload["avgPrice"]); avgPrice > 0 {
		return avgPrice
	}
	cumQuote := parseNullableFloat(payload["cummulativeQuoteQty"])
	if cumQuote <= 0 {
		return 0
	}
	return cumQuote / executedQty
}

func mathMin(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
