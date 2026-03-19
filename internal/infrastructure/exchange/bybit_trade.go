package exchange

import (
	"bytes"
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

// BybitV5TradeClient 封装 Bybit V5 永续交易接口。
//
// 和 Binance-like family 最大的不同点有三类：
// 1. 签名头不同：Bybit 使用 `X-BAPI-*` 头，签名串由 timestamp + apiKey + recvWindow + payload 组成；
// 2. 业务 envelope 不同：HTTP 200 不代表业务成功，必须继续检查 `retCode / retMsg`；
// 3. 订单/持仓字段不同：例如 `orderLinkId`、`cumExecQty`、`side=Buy/Sell`、`size` 等。
//
// 因此它必须是一套独立 adapter family，而不是继续堆在 Binance-like client 里做分支。
type BybitV5TradeClient struct {
	name         string
	cfg          ExchangeConfig
	logger       *slog.Logger
	httpClient   *http.Client
	wsDialer     *websocket.Dialer
	apiKey       string
	apiSecret    string
	recvWindow   string
	category     string
	accountType  string
	positionIdx  int
	extraHeaders map[string]string
}

type bybitOrderResult struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
}

type bybitRealtimeOrder struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
	OrderStatus string `json:"orderStatus"`
	AvgPrice    string `json:"avgPrice"`
	CumExecQty  string `json:"cumExecQty"`
}

type bybitRealtimeOrderResult struct {
	Category string               `json:"category"`
	List     []bybitRealtimeOrder `json:"list"`
}

type bybitPositionItem struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Size          string `json:"size"`
	AvgPrice      string `json:"avgPrice"`
	MarkPrice     string `json:"markPrice"`
	UnrealisedPnl string `json:"unrealisedPnl"`
}

type bybitPositionResult struct {
	Category string              `json:"category"`
	List     []bybitPositionItem `json:"list"`
}

type bybitWalletBalance struct {
	TotalEquity           string `json:"totalEquity"`
	TotalAvailableBalance string `json:"totalAvailableBalance"`
	TotalInitialMargin    string `json:"totalInitialMargin"`
	TotalMarginBalance    string `json:"totalMarginBalance"`
	TotalWalletBalance    string `json:"totalWalletBalance"`
}

type bybitWalletResult struct {
	List []bybitWalletBalance `json:"list"`
}

func NewBybitV5TradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://api.bybit.com"
	}
	if c.PrivateWSBaseURL == "" {
		c.PrivateWSBaseURL = "wss://stream.bybit.com/v5/private"
	}
	appCfg := loadAppConfig()
	return &BybitV5TradeClient{
		name:         name,
		cfg:          c,
		logger:       logger,
		httpClient:   newHTTPClient(c, appCfg, logger, name+"-trade"),
		wsDialer:     newWebSocketDialer(c, appCfg, logger, name+"-trade"),
		apiKey:       readEnvByName(c.Auth.APIKeyEnv),
		apiSecret:    readEnvByName(c.Auth.APISecretEnv),
		recvWindow:   bybitRecvWindow(c),
		category:     bybitCategory(c),
		accountType:  bybitAccountType(c),
		positionIdx:  bybitPositionIdx(c),
		extraHeaders: resolveBybitExtraHeaders(c.Auth),
	}
}

func (c *BybitV5TradeClient) Name() string { return c.name }
func (c *BybitV5TradeClient) Enabled() bool {
	return c.cfg.Enabled && c.apiKey != "" && c.apiSecret != ""
}

func (c *BybitV5TradeClient) Capabilities() TradeCapabilities {
	return TradeCapabilities{
		MakerLimitTIF:            "PostOnly",
		TakerOrderType:           "Market",
		TakerUsesAggressiveIOC:   false,
		SupportsOrderEventStream: c.supportsOrderEventStream(),
	}
}

func (c *BybitV5TradeClient) PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	if !c.Enabled() {
		return TradeOrderResult{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}

	orderType := bybitOrderType(req.OrderType)
	payload := map[string]any{
		"category":    c.category,
		"symbol":      req.VenueSymbol,
		"side":        bybitSide(req.Side),
		"orderType":   orderType,
		"qty":         formatFloat(req.Quantity, 8),
		"positionIdx": c.positionIdx,
	}
	if orderType == "Limit" {
		payload["price"] = formatFloat(req.Price, 8)
		payload["timeInForce"] = bybitTimeInForce(req.TimeInForce, "GTC")
	}
	if req.ReduceOnly {
		payload["reduceOnly"] = true
	}
	if req.ClientOrderID != "" {
		payload["orderLinkId"] = req.ClientOrderID
	}

	var result bybitOrderResult
	raw, _, err := c.signedPOST(ctx, "/v5/order/create", payload, &result)
	if err != nil {
		return TradeOrderResult{}, err
	}

	return TradeOrderResult{
		Exchange:        c.name,
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   firstNonEmpty(result.OrderLinkID, req.ClientOrderID),
		VenueOrderID:    result.OrderID,
		Status:          "SUBMITTED",
		ExecutedQty:     0,
		AveragePrice:    req.Price,
		RawResponse:     raw,
	}, nil
}

func (c *BybitV5TradeClient) ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	pos, err := c.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err != nil {
		return TradeOrderResult{}, err
	}
	side, qty, ok := closeSideAndQuantity(pos.Quantity, req.Quantity)
	if !ok {
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

func (c *BybitV5TradeClient) GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error) {
	if !c.Enabled() {
		return OrderStatus{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}

	params := url.Values{}
	params.Set("category", c.category)
	if req.VenueSymbol != "" {
		params.Set("symbol", req.VenueSymbol)
	}
	if req.VenueOrderID != "" {
		params.Set("orderId", req.VenueOrderID)
	} else if req.ClientOrderID != "" {
		params.Set("orderLinkId", req.ClientOrderID)
	} else {
		return OrderStatus{}, fmt.Errorf("missing order lookup id")
	}

	var result bybitRealtimeOrderResult
	raw, _, err := c.signedGET(ctx, "/v5/order/realtime", params, &result)
	if err != nil {
		return OrderStatus{}, err
	}
	if len(result.List) == 0 {
		return OrderStatus{}, fmt.Errorf("%s order not found for symbol=%s clientOrderID=%s venueOrderID=%s", c.name, req.VenueSymbol, req.ClientOrderID, req.VenueOrderID)
	}

	item := result.List[0]
	status := bybitNormalizeOrderStatus(item.OrderStatus)
	return OrderStatus{
		Exchange:      c.name,
		Status:        status,
		ExecutedQty:   parseNullableFloat(item.CumExecQty),
		AveragePrice:  parseNullableFloat(item.AvgPrice),
		VenueOrderID:  firstNonEmpty(item.OrderID, req.VenueOrderID),
		ClientOrderID: firstNonEmpty(item.OrderLinkID, req.ClientOrderID),
		Terminal:      bybitIsTerminalOrderStatus(status),
		Canceled:      status == "CANCELED" || status == "PARTIALLY_FILLED_CANCELED" || status == "DEACTIVATED",
		RawResponse:   raw,
	}, nil
}

func (c *BybitV5TradeClient) GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	if !c.Enabled() {
		return AccountSnapshot{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}

	params := url.Values{}
	params.Set("accountType", c.accountType)

	var result bybitWalletResult
	raw, _, err := c.signedGET(ctx, "/v5/account/wallet-balance", params, &result)
	if err != nil {
		return AccountSnapshot{}, err
	}
	if len(result.List) == 0 {
		return AccountSnapshot{Exchange: c.name, RawResponse: raw}, nil
	}

	item := result.List[0]
	return AccountSnapshot{
		Exchange:         c.name,
		Equity:           firstPositive(parseNullableFloat(item.TotalEquity), parseNullableFloat(item.TotalMarginBalance), parseNullableFloat(item.TotalWalletBalance)),
		AvailableBalance: parseNullableFloat(item.TotalAvailableBalance),
		MarginUsed:       parseNullableFloat(item.TotalInitialMargin),
		RawResponse:      raw,
	}, nil
}

func (c *BybitV5TradeClient) GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, _ string) (Position, error) {
	if !c.Enabled() {
		return Position{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}

	params := url.Values{}
	params.Set("category", c.category)
	if venueSymbol != "" {
		params.Set("symbol", venueSymbol)
	}

	var result bybitPositionResult
	_, _, err := c.signedGET(ctx, "/v5/position/list", params, &result)
	if err != nil {
		return Position{}, err
	}

	for _, item := range result.List {
		if venueSymbol != "" && !strings.EqualFold(item.Symbol, venueSymbol) {
			continue
		}
		qty := parseNullableFloat(item.Size)
		if strings.EqualFold(item.Side, "Sell") {
			qty = -qty
		}
		return Position{
			Exchange:      c.name,
			Symbol:        canonicalSymbol,
			VenueSymbol:   firstNonEmpty(venueSymbol, item.Symbol),
			Quantity:      qty,
			EntryPrice:    parseNullableFloat(item.AvgPrice),
			MarkPrice:     parseNullableFloat(item.MarkPrice),
			UnrealizedPnL: parseNullableFloat(item.UnrealisedPnl),
		}, nil
	}

	return Position{Exchange: c.name, Symbol: canonicalSymbol, VenueSymbol: venueSymbol}, nil
}

func (c *BybitV5TradeClient) signedGET(ctx context.Context, path string, params url.Values, out any) (string, int64, error) {
	query := params.Encode()
	return c.signedDo(ctx, http.MethodGet, path, query, nil, out)
}

func (c *BybitV5TradeClient) signedPOST(ctx context.Context, path string, payload any, out any) (string, int64, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	return c.signedDo(ctx, http.MethodPost, path, "", body, out)
}

func (c *BybitV5TradeClient) signedDo(ctx context.Context, method, path, query string, body []byte, out any) (string, int64, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	payload := query
	if len(body) > 0 {
		payload = string(body)
	}
	signature := c.sign(timestamp + c.apiKey + c.recvWindow + payload)

	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path
	if query != "" {
		endpoint += "?" + query
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("X-BAPI-SIGN", signature)
	req.Header.Set("X-BAPI-API-KEY", c.apiKey)
	req.Header.Set("X-BAPI-TIMESTAMP", timestamp)
	req.Header.Set("X-BAPI-RECV-WINDOW", c.recvWindow)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range c.extraHeaders {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}
	if resp.StatusCode >= 300 {
		return string(rawBody), 0, fmt.Errorf("%s bybit signed request %s failed status=%d body=%s", c.name, path, resp.StatusCode, string(rawBody))
	}

	envelope, err := decodeBybitEnvelope(rawBody, out)
	if err != nil {
		return string(rawBody), 0, fmt.Errorf("%s bybit signed request %s failed: %w", c.name, path, err)
	}
	return string(rawBody), envelope.Time, nil
}

func (c *BybitV5TradeClient) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// resolveBybitExtraHeaders 把 auth.extra_env 中和 Bybit 请求头相关的扩展位映射出来。
//
// 当前最常见的场景是 broker 用户的 `X-Referer`。
// 这里没有盲目把所有 extra_env 都塞成 header，而是只接受明确的 header-like key，
// 这样能避免把无关敏感信息意外发到网络请求里。
func resolveBybitExtraHeaders(auth AuthConfig) map[string]string {
	values := auth.ResolveExtraEnv()
	if len(values) == 0 {
		return nil
	}

	headers := make(map[string]string)
	for key, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch normalized := strings.ToLower(strings.TrimSpace(key)); normalized {
		case "referer", "x-referer", "x_referer":
			headers["X-Referer"] = value
		default:
			if strings.HasPrefix(key, "X-") {
				headers[key] = value
			}
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
