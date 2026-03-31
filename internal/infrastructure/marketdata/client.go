package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraPolymarket "goKit/internal/infrastructure/polymarket"

	"github.com/gorilla/websocket"
	"log/slog"
)

// Client 是外部行情基础设施客户端的具体实现。
type Client struct {
	cfg    infraPolymarket.Config
	logger *slog.Logger
	http   *http.Client
	dialer *websocket.Dialer
}

// NewClient 创建行情基础设施客户端。
func NewClient(cfg infraPolymarket.Config, logger *slog.Logger) (*Client, error) {
	httpClient, err := infraPolymarket.NewProxyHTTPClient(cfg, 12*time.Second)
	if err != nil {
		return nil, err
	}
	dialer, err := infraPolymarket.NewProxyWebsocketDialer(cfg, 10*time.Second)
	if err != nil {
		return nil, err
	}

	return &Client{
		cfg:    cfg,
		logger: logger,
		http:   httpClient,
		dialer: dialer,
	}, nil
}

// GetBinanceBTCPrice 从 Binance 拉取当前配置资产的参考现价。
func (c *Client) GetBinanceBTCPrice(ctx context.Context) (float64, error) {
	endpoint, err := url.Parse(c.cfg.BinancePriceURL)
	if err != nil {
		return 0, err
	}
	q := endpoint.Query()
	q.Set("symbol", c.cfg.ResolvedBinanceSymbol())
	endpoint.RawQuery = q.Encode()

	// Binance ticker 接口会把价格作为字符串返回，这里统一转成 float64。
	var resp BinanceTickerPriceResponse
	if err := c.doJSON(ctx, endpoint.String(), nil, &resp); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(resp.Price, 64)
}

// SubscribeBinanceBTC 维持当前配置资产的 Binance 现价 websocket 订阅，并在断线后自动重连。
func (c *Client) SubscribeBinanceBTC(ctx context.Context, onPrice func(float64)) error {
	endpoint := strings.TrimSpace(c.cfg.ResolvedBinanceWSURL())
	if endpoint == "" {
		return fmt.Errorf("binance websocket url is empty")
	}

	for {
		if err := c.runBinanceWSLoop(ctx, endpoint, onPrice); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if c.logger != nil {
				c.logger.Warn("marketdata binance reconnect", "error", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		return nil
	}
}

// GetCryptoPrice 拉取 Polymarket 页面依赖的 PTB 开盘价和收盘价。
func (c *Client) GetCryptoPrice(ctx context.Context, startTime, endTime string) (openPrice *float64, closePrice *float64, err error) {
	endpoint, err := url.Parse(c.cfg.CryptoPriceAPI)
	if err != nil {
		return nil, nil, err
	}
	q := endpoint.Query()
	q.Set("symbol", c.cfg.ResolvedCryptoPriceSymbol())
	q.Set("eventStartTime", startTime)
	if variant := strings.TrimSpace(c.cfg.ResolvedCryptoPriceVariant()); variant != "" {
		q.Set("variant", variant)
	}
	q.Set("endDate", endTime)
	endpoint.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("User-Agent", "goKit-polymarket")
	headers.Set("Accept", "application/json")
	headers.Set("Referer", "https://polymarket.com/")

	var resp CryptoPriceResponse
	if err := c.doJSON(ctx, endpoint.String(), headers, &resp); err != nil {
		return nil, nil, err
	}

	// 这里继续保留宽松解析，兼容接口偶尔返回字符串或数字两种形式。
	if v := maybeFloat(resp.OpenPrice); v != nil {
		openPrice = v
	}
	if v := maybeFloat(resp.ClosePrice); v != nil {
		closePrice = v
	}
	return openPrice, closePrice, nil
}

// SubscribeRTDS 维持 RTDS 行情订阅，并在断线后自动重连。
func (c *Client) SubscribeRTDS(ctx context.Context, onPrice func(float64)) error {
	for {
		if err := c.runRTDSWSLoop(ctx, c.cfg.RTDSWSURL, onPrice); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if c.logger != nil {
				c.logger.Warn("marketdata rtds reconnect", "error", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		return nil
	}
}

// runBinanceWSLoop 建立单次 Binance websocket 会话，并持续读取最新成交价。
func (c *Client) runBinanceWSLoop(ctx context.Context, endpoint string, onPrice func(float64)) error {
	conn, _, err := c.dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		price, ok := extractBinancePrice(msg)
		if ok {
			onPrice(price)
		}
	}
}

// runRTDSWSLoop 建立单次 RTDS websocket 会话，并持续读取当前配置资产的 Chainlink 价格消息。
func (c *Client) runRTDSWSLoop(ctx context.Context, endpoint string, onPrice func(float64)) error {
	conn, _, err := c.dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}

	// RTDS 订阅符号默认随市场资产自动切换，例如 BTC -> btc/usd，ETH -> eth/usd。
	rtdsSymbol := c.cfg.ResolvedRTDSSymbol()
	sub := RTDSSubscribeRequest{
		Action: "subscribe",
		Subscriptions: []RTDSSubscription{
			{
				Topic:   "crypto_prices_chainlink",
				Type:    "*",
				Filters: fmt.Sprintf("{\"symbol\":\"%s\"}", rtdsSymbol),
			},
		},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		price, ok := extractRTDSPrice(msg, rtdsSymbol)
		if ok {
			onPrice(price)
		}
	}
}

// doJSON 发起只读 HTTP 请求，并把 JSON 响应解码到目标对象中。
func (c *Client) doJSON(ctx context.Context, rawURL string, headers http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "goKit-polymarket")
	for k, values := range headers {
		for _, value := range values {
			req.Header.Add(k, value)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("marketdata status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// extractRTDSPrice 从 RTDS 消息中提取价格，兼容 Python 版使用过的 payload/data 结构。
func extractRTDSPrice(message []byte, expectedSymbol string) (float64, bool) {
	var payload RTDSMessage
	if err := json.Unmarshal(message, &payload); err != nil {
		return 0, false
	}
	if v := maybeFloat(payload.Value); v != nil {
		return *v, true
	}
	if price, ok := extractRTDSDataPoints(payload.Data); ok {
		return price, true
	}

	// RTDS 常见消息会把真正内容包在 payload 里；这里按 Python 版字段结构继续兼容。
	if len(payload.Payload) > 0 {
		var inner RTDSPayload
		if err := json.Unmarshal(payload.Payload, &inner); err == nil {
			if symbol := strings.ToLower(strings.TrimSpace(inner.Symbol)); symbol != "" && expectedSymbol != "" && symbol != strings.ToLower(expectedSymbol) {
				return 0, false
			}
			if v := maybeFloat(inner.Value); v != nil {
				return *v, true
			}
			if price, ok := extractRTDSDataPoints(inner.Data); ok {
				return price, true
			}
		}
	}
	return 0, false
}

// extractRTDSDataPoints 从 data 字段中提取单个点位或一批点位里的价格。
func extractRTDSDataPoints(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}

	// 有些消息会把价格包在单个对象里。
	var single RTDSDataPoint
	if json.Unmarshal(raw, &single) == nil {
		if v := maybeFloat(single.Value); v != nil {
			return *v, true
		}
	}

	// 也有消息会把多个点位放进数组，取第一个可解析价格即可。
	var batch []RTDSDataPoint
	if json.Unmarshal(raw, &batch) == nil {
		for idx := len(batch) - 1; idx >= 0; idx-- {
			item := batch[idx]
			if v := maybeFloat(item.Value); v != nil {
				return *v, true
			}
		}
	}
	return 0, false
}

// extractBinancePrice 从 Binance websocket 消息中提取成交价或最新价。
func extractBinancePrice(message []byte) (float64, bool) {
	var payload BinanceTradeMessage
	if err := json.Unmarshal(message, &payload); err != nil {
		return 0, false
	}
	if price := strings.TrimSpace(payload.Price); price != "" {
		if parsed, err := strconv.ParseFloat(price, 64); err == nil {
			return parsed, true
		}
	}
	if price := strings.TrimSpace(payload.LastPrice); price != "" {
		if parsed, err := strconv.ParseFloat(price, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// maybeFloat 尽量把常见 JSON 标量转换成 float64 指针。
func maybeFloat(v any) *float64 {
	switch value := v.(type) {
	case float64:
		return &value
	case float32:
		parsed := float64(value)
		return &parsed
	case json.Number:
		parsed, err := value.Float64()
		if err == nil {
			return &parsed
		}
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return &parsed
		}
	}
	return nil
}
