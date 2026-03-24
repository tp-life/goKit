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

	"github.com/gorilla/websocket"
)

// CEXTradeClient 是保留了历史命名的实现类型。
//
// 它的真实职责不是“适配任意中心化交易所”，而是：
// 适配 Binance / Aster 这类 Binance-like 永续合约私有交易协议。
//
// 这层纠偏非常重要，因为后续如果继续接入 OKX / Bybit / Bitget，
// 我们应该先判断它们是否真的属于同一协议族，而不是被 `CEX` 这个宽泛名字误导。
type CEXTradeClient struct {
	name         string
	cfg          ExchangeConfig
	logger       *slog.Logger
	httpClient   *http.Client
	wsDialer     *websocket.Dialer
	apiKey       string
	apiSecret    string
	orderPath    string
	positionPath string
	accountPath  string
}

func NewBinanceTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	return NewCEXTradeAdapter("binance", cfg.Binance, logger)
}

func NewAsterTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	return NewCEXTradeAdapter("aster", cfg.Aster, logger)
}

// NewBinanceLikeTradeAdapter 是 registry 层应该优先使用的交易适配器构造入口。
//
// 它的命名刻意强调：这份实现只覆盖 Binance Futures 风格的私有交易协议，
// 例如 API Key + HMAC 签名、`/fapi/...` 路径、Binance 风格字段名等。
//
// 这能帮助后续接入新交易所时更快看清边界：
// - 如果新交易所真的是 Binance-like，可以复用这份实现；
// - 如果不是，就应该新增新的协议族适配器，而不是继续往这里堆特殊分支。
func NewBinanceLikeTradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	return NewCEXTradeAdapter(name, cfg, logger)
}

// NewCEXTradeAdapter 是保留给现有代码的兼容构造入口。
//
// 名字虽然还是 `CEX`，但不要把它理解成“所有 CEX 通用适配器”；
// 它现在只代表 `binance_like` 协议族。
func NewCEXTradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		switch name {
		case "aster":
			c.RestBaseURL = "https://fapi.asterdex.com"
		default:
			c.RestBaseURL = "https://fapi.binance.com"
		}
	}
	if c.PublicWSBaseURL == "" {
		switch name {
		case "aster":
			c.PublicWSBaseURL = "wss://fstream.asterdex.com"
		default:
			c.PublicWSBaseURL = "wss://fstream.binance.com"
		}
	}
	if c.PrivateWSBaseURL == "" {
		c.PrivateWSBaseURL = c.PublicWSBaseURL
	}
	appCfg := loadAppConfig()
	orderPath := "/fapi/v1/order"
	positionPath := "/fapi/v2/positionRisk"
	accountPath := "/fapi/v2/account"
	if name == "aster" {
		orderPath = "/fapi/v3/order"
		positionPath = "/fapi/v3/positionRisk"
		accountPath = "/fapi/v3/account"
	}
	client := &CEXTradeClient{
		name:         name,
		cfg:          c,
		logger:       logger,
		httpClient:   newHTTPClient(c, appCfg, logger, name+"-trade"),
		wsDialer:     newWebSocketDialer(c, appCfg, logger, name+"-trade"),
		apiKey:       readEnvByName(c.Auth.APIKeyEnv),
		apiSecret:    readEnvByName(c.Auth.APISecretEnv),
		orderPath:    orderPath,
		positionPath: positionPath,
		accountPath:  accountPath,
	}
	return client
}

func (c *CEXTradeClient) Name() string  { return c.name }
func (c *CEXTradeClient) Enabled() bool { return c.cfg.Enabled && c.apiKey != "" && c.apiSecret != "" }
func (c *CEXTradeClient) Capabilities() TradeCapabilities {
	return TradeCapabilities{
		MakerLimitTIF:            "GTX",
		TakerOrderType:           "MARKET",
		TakerUsesAggressiveIOC:   false,
		SupportsOrderEventStream: c.supportsOrderEventStream(),
	}
}

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
	side, qty, ok := closeSideAndQuantity(pos.Quantity, req.Quantity)
	if !ok {
		return TradeOrderResult{Exchange: c.name, CanonicalSymbol: req.CanonicalSymbol, VenueSymbol: req.VenueSymbol, Status: "NO_POSITION"}, nil
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
	resp, body, err := c.performSignedRequest(ctx, method, path, params)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		if c.logger != nil {
			c.logger.Warn("cex_signed_request_failed",
				"exchange", c.name,
				"path", path,
				"method", method,
				"status_code", resp.StatusCode,
				"response_body", string(body),
			)
		}
		return string(body), fmt.Errorf("%s signed request failed status=%d body=%s", c.name, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return string(body), err
		}
	}
	return string(body), nil
}

func (c *CEXTradeClient) performSignedRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, []byte, error) {
	payload := params.Encode()
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	_, _ = mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path + "?" + payload + "&signature=" + signature
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
	case "FILLED", "CANCELED", "REJECTED", "EXPIRED", "EXPIRED_IN_MATCH", "NO_POSITION":
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
