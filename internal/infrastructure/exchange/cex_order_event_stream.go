package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// cexUserStreamKeepaliveInterval 选 30 分钟，是对 Binance 官方“listenKey 60 分钟有效期”的保守续期策略。
//
// 这样即使中途出现一次 keepalive 抖动，也仍然有充足时间在下一轮继续续期，
// 不至于把 listenKey 用到接近过期边缘。
const cexUserStreamKeepaliveInterval = 30 * time.Minute

type cexListenKeyResponse struct {
	ListenKey string `json:"listenKey"`
}

type cexUserDataEvent struct {
	EventType       string           `json:"e"`
	EventTime       int64            `json:"E"`
	TransactionTime int64            `json:"T"`
	ListenKey       string           `json:"listenKey"`
	Order           cexUserDataOrder `json:"o"`
}

type cexUserDataOrder struct {
	Symbol        string `json:"s"`
	ClientOrderID string `json:"c"`
	Side          string `json:"S"`
	OrderType     string `json:"o"`
	TimeInForce   string `json:"f"`
	OrderStatus   string `json:"X"`
	ExecutionType string `json:"x"`
	AvgPrice      string `json:"ap"`
	FilledQty     string `json:"z"`
	OrderID       any    `json:"i"`
}

// supportsOrderEventStream 明确声明“当前这份 CEX 交易适配器是否真的具备私有订单流实现”。
//
// 虽然 Binance 和 Aster 在仓库里都复用了 CEXTradeClient，但当前真实落地的私有流实现
// 只针对 Binance 官方 USD-M Futures 文档验证过。
// 因此这里先保守限制在 Binance，避免把“看起来像 Binance-like”但细节不完全一致的交易所
// 误接进来后产生静默错单风险。
func (c *CEXTradeClient) supportsOrderEventStream() bool {
	return c != nil && c.Enabled() && strings.EqualFold(c.name, "binance")
}

// StartOrderEventStream 负责启动 Binance Futures 用户订单事件流。
//
// 当前实现遵循 Binance 官方用户流流程：
// 1. 先通过 REST 创建 listenKey；
// 2. 再连接 `wss://.../ws/<listenKey>`；
// 3. 后台定期对 listenKey 做 keepalive；
// 4. 读取 `ORDER_TRADE_UPDATE` 并发布为统一 `OrderEvent`。
//
// 这里刻意做成“外层循环自动重建 listenKey + 重新连接”的结构，
// 这样无论是 24 小时连接主动断开、listenKey 过期、还是链路短暂抖动，
// 都能尽量通过同一条重连路径恢复，而不把恢复逻辑散落到多个 goroutine 里。
func (c *CEXTradeClient) StartOrderEventStream(ctx context.Context, sink OrderEventSink) error {
	if !c.supportsOrderEventStream() || sink == nil {
		return nil
	}

	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		listenKey, err := c.startUserDataStream(ctx)
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("cex_user_stream_listen_key_create_failed", "exchange", c.name, "err", err)
			}
			if !sleepContext(ctx, backoff) {
				return nil
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		backoff = time.Second
		streamCtx, cancel := context.WithCancel(ctx)
		go c.keepaliveUserDataStreamLoop(streamCtx, listenKey)
		err = c.readUserDataStream(streamCtx, listenKey, sink)
		cancel()
		_ = c.closeUserDataStream(context.Background(), listenKey)

		if ctx.Err() != nil {
			return nil
		}
		if c.logger != nil && err != nil {
			c.logger.Warn("cex_user_stream_read_loop_stopped", "exchange", c.name, "err", err)
		}
		if !sleepContext(ctx, backoff) {
			return nil
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *CEXTradeClient) startUserDataStream(ctx context.Context) (string, error) {
	var payload cexListenKeyResponse
	_, err := c.apiKeyRequest(ctx, http.MethodPost, "/fapi/v1/listenKey", &payload)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.ListenKey) == "" {
		return "", fmt.Errorf("%s user stream returned empty listenKey", c.name)
	}
	return strings.TrimSpace(payload.ListenKey), nil
}

func (c *CEXTradeClient) keepaliveUserDataStreamLoop(ctx context.Context, listenKey string) {
	ticker := time.NewTicker(cexUserStreamKeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := c.apiKeyRequest(ctx, http.MethodPut, "/fapi/v1/listenKey?listenKey="+listenKey, nil); err != nil && c.logger != nil {
				c.logger.Warn("cex_user_stream_keepalive_failed", "exchange", c.name, "listen_key", listenKey, "err", err)
			}
		}
	}
}

func (c *CEXTradeClient) closeUserDataStream(ctx context.Context, listenKey string) error {
	if strings.TrimSpace(listenKey) == "" {
		return nil
	}
	_, err := c.apiKeyRequest(ctx, http.MethodDelete, "/fapi/v1/listenKey?listenKey="+listenKey, nil)
	return err
}

func (c *CEXTradeClient) readUserDataStream(ctx context.Context, listenKey string, sink OrderEventSink) error {
	base := strings.TrimRight(resolvePrivateWSBaseURL(c.cfg, ""), "/")
	endpoint := base + "/ws/" + listenKey

	conn, _, err := c.wsDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	configureWebSocketReadDeadline(conn, 30*time.Second)

	// websocket.DialContext 只约束建连阶段，不会在建连成功后自动把 ctx.Done() 传递到 ReadMessage。
	// 这里单独起一个很薄的 goroutine，在 ctx 结束时主动关闭连接，
	// 让 read loop 能尽快退出，而不是最多等到下一次 read deadline 超时。
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	go keepaliveWebSocketControlPingLoop(ctx, conn, 15*time.Second, done, func(err error) {
		if c.logger != nil {
			c.logger.Warn("cex_user_stream_ping_failed", "exchange", c.name, "listen_key", listenKey, "err", err)
		}
		_ = conn.Close()
	})

	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		event, ok, err := c.parseUserDataOrderEvent(msg)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "listenkey expired") {
				return err
			}
			if c.logger != nil {
				c.logger.Warn("cex_user_stream_parse_failed", "exchange", c.name, "err", err, "payload", string(msg))
			}
			continue
		}
		if !ok {
			continue
		}
		sink.PublishOrderEvent(event)
	}
}

func (c *CEXTradeClient) parseUserDataOrderEvent(payload []byte) (OrderEvent, bool, error) {
	var event cexUserDataEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return OrderEvent{}, false, err
	}

	switch strings.TrimSpace(event.EventType) {
	case "listenKeyExpired":
		return OrderEvent{}, false, fmt.Errorf("%s user stream listenKey expired", c.name)
	case "ORDER_TRADE_UPDATE":
		orderStatus := strings.ToUpper(strings.TrimSpace(event.Order.OrderStatus))
		return OrderEvent{
			Source:        "binance_user_stream",
			Exchange:      c.name,
			ClientOrderID: strings.TrimSpace(event.Order.ClientOrderID),
			VenueOrderID:  normalizeVenueOrderID(event.Order.OrderID),
			Status:        orderStatus,
			ExecutedQty:   mustFloat(event.Order.FilledQty),
			AveragePrice:  mustFloat(event.Order.AvgPrice),
			Terminal:      isTerminalOrderStatus(orderStatus),
			Canceled:      orderStatus == "CANCELED" || orderStatus == "EXPIRED" || orderStatus == "EXPIRED_IN_MATCH",
			RawPayload:    string(payload),
			OccurredAtMs:  pickPositiveInt64(event.TransactionTime, event.EventTime),
		}, true, nil
	default:
		return OrderEvent{}, false, nil
	}
}

func normalizeVenueOrderID(v any) string {
	return strings.TrimSpace(asString(v))
}

func pickPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// sleepContext 是订单事件流重连 backoff 使用的 ctx-aware sleep。
//
// 这里不用普通 `time.Sleep`，是因为用户流 goroutine 在服务停机时应该尽快退出；
// 如果仍然直接 sleep，OnStop 之后最多还要额外等一个 backoff 周期，退出会显得拖沓。
func sleepContext(ctx context.Context, wait time.Duration) bool {
	if wait <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// apiKeyRequest 用于 Binance 用户流那类“只需要 API Key、不需要签名”的 REST 调用。
//
// 例如：
// - 创建 listenKey
// - keepalive listenKey
// - 关闭 listenKey
//
// 它和 signedDo 的边界不同，故意单独拆开，避免把“签名交易请求”和“用户流管理请求”
// 混成一套 helper，降低后续维护时的心智负担。
func (c *CEXTradeClient) apiKeyRequest(ctx context.Context, method, path string, out any) (string, error) {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path
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
		return string(body), fmt.Errorf("%s api-key request failed status=%d body=%s", c.name, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return string(body), err
		}
	}
	return string(body), nil
}
