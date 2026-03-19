package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// bybitPrivatePingInterval 使用 20s，是一个比较保守的应用层 ping 周期。
	//
	// Bybit 私有流除了 websocket 控制帧以外，还支持 JSON 级别的 `{"op":"ping"}`。
	// 这里主动维持一条轻量心跳，目的不是“和交易所拼最低延迟”，而是：
	// 1. 让 NAT / 代理 / 中间层更不容易把空闲连接静默掐断；
	// 2. 让 read loop 在真实断链前更快暴露问题；
	// 3. 为后续接更多交易所时提供统一、可读的私有流 keepalive 模式。
	bybitPrivatePingInterval = 20 * time.Second

	// Bybit V5 私有流认证要求签名串使用 `GET/realtime{expires}` 这个固定前缀。
	//
	// 这里单独提成常量，是为了把“HTTP REST 的签名串”和“private websocket 的签名串”
	// 明确区分开，避免以后维护时误把两者混用。
	bybitPrivateAuthPayloadPrefix = "GET/realtime"
)

type bybitPrivateControlMessage struct {
	Op      string   `json:"op"`
	Success bool     `json:"success"`
	RetMsg  string   `json:"ret_msg"`
	ConnID  string   `json:"connId"`
	ReqID   string   `json:"req_id"`
	Args    []string `json:"args"`
}

type bybitPrivateOrderEnvelope struct {
	ID           string                  `json:"id"`
	Topic        string                  `json:"topic"`
	CreationTime int64                   `json:"creationTime"`
	Data         []bybitPrivateOrderData `json:"data"`
}

type bybitPrivateOrderData struct {
	Category     string `json:"category"`
	OrderID      string `json:"orderId"`
	OrderLinkID  string `json:"orderLinkId"`
	OrderStatus  string `json:"orderStatus"`
	AvgPrice     string `json:"avgPrice"`
	CumExecQty   string `json:"cumExecQty"`
	CancelType   string `json:"cancelType"`
	RejectReason string `json:"rejectReason"`
	UpdatedTime  string `json:"updatedTime"`
	CreatedTime  string `json:"createdTime"`
}

type bybitPrivateAuthRequest struct {
	Op   string `json:"op"`
	Args []any  `json:"args"`
}

type bybitPrivateSubscribeRequest struct {
	Op   string   `json:"op"`
	Args []string `json:"args"`
}

func (c *BybitV5TradeClient) supportsOrderEventStream() bool {
	return c != nil && c.Enabled()
}

// StartOrderEventStream 启动 Bybit V5 private order stream。
//
// 这条链路的目标不是把 Bybit 所有私有 topic 一次补齐，而是先把“订单事件 -> Execution 状态机”
// 这条最关键的恢复链路打通：
// 1. 主腿超时后，后续迟到成交可以通过 private stream 继续修正本地 OrderRecord；
// 2. Recovery close / open_hedging 期间，新的订单事实也能继续进入统一事件入口；
// 3. 同一条 `ApplyExternalOrderEvent(...)` 入口同时兼容 websocket 与 HTTP 手工回放。
func (c *BybitV5TradeClient) StartOrderEventStream(ctx context.Context, sink OrderEventSink) error {
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

		err := c.runPrivateOrderStream(ctx, sink)
		if ctx.Err() != nil {
			return nil
		}
		if c.logger != nil && err != nil {
			c.logger.Warn("bybit_private_order_stream_stopped", "exchange", c.name, "err", err)
		}
		if !sleepContext(ctx, backoff) {
			return nil
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *BybitV5TradeClient) runPrivateOrderStream(ctx context.Context, sink OrderEventSink) error {
	endpoint := resolvePrivateWSBaseURL(c.cfg, "wss://stream.bybit.com/v5/private")
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("%s private websocket base url is empty", c.name)
	}

	conn, _, err := c.wsDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	if err := c.authenticatePrivateOrderStream(conn); err != nil {
		return err
	}
	if err := c.subscribePrivateOrderStream(conn); err != nil {
		return err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.keepalivePrivateOrderStreamLoop(streamCtx, conn)

	return c.readPrivateOrderStream(streamCtx, conn, sink)
}

// authenticatePrivateOrderStream 完成 Bybit V5 private websocket 的鉴权。
//
// Bybit 这一步和 REST 签名不同：
// - REST 用的是 `timestamp + apiKey + recvWindow + payload`
// - Private WS 用的是 `GET/realtime{expires}`
//
// 之所以明确拆成单独方法，是因为 websocket auth 经常会在后续接入更多 topic 时复用；
// 把这部分和具体 topic 订阅解耦，后面扩展 `execution` / `position` topic 会更自然。
func (c *BybitV5TradeClient) authenticatePrivateOrderStream(conn *websocket.Conn) error {
	req := c.buildPrivateAuthRequest(time.Now())
	if err := conn.WriteJSON(req); err != nil {
		return err
	}
	return c.awaitPrivateControlAck(conn, "auth")
}

func (c *BybitV5TradeClient) subscribePrivateOrderStream(conn *websocket.Conn) error {
	req := bybitPrivateSubscribeRequest{
		Op:   "subscribe",
		Args: []string{c.privateOrderTopic()},
	}
	if err := conn.WriteJSON(req); err != nil {
		return err
	}
	return c.awaitPrivateControlAck(conn, "subscribe")
}

func (c *BybitV5TradeClient) buildPrivateAuthRequest(now time.Time) bybitPrivateAuthRequest {
	expires := now.Add(10 * time.Second).UnixMilli()
	signature := c.sign(bybitPrivateAuthPayloadPrefix + strconv.FormatInt(expires, 10))
	return bybitPrivateAuthRequest{
		Op:   "auth",
		Args: []any{c.apiKey, expires, signature},
	}
}

func (c *BybitV5TradeClient) privateOrderTopic() string {
	switch strings.ToLower(strings.TrimSpace(c.category)) {
	case "spot", "linear", "inverse", "option":
		return "order." + strings.ToLower(strings.TrimSpace(c.category))
	default:
		return "order"
	}
}

func (c *BybitV5TradeClient) awaitPrivateControlAck(conn *websocket.Conn, op string) error {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if isTimeoutErr(err) {
				continue
			}
			return err
		}

		var msg bybitPrivateControlMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(msg.Op), op) {
			// 认证/订阅阶段只关心对应 ack，其他控制消息直接忽略即可。
			continue
		}
		if !msg.Success {
			return fmt.Errorf("%s private websocket %s failed: %s", c.name, op, strings.TrimSpace(msg.RetMsg))
		}
		return nil
	}
}

func (c *BybitV5TradeClient) keepalivePrivateOrderStreamLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(bybitPrivatePingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.WriteJSON(map[string]any{"op": "ping"}); err != nil {
				if c.logger != nil {
					c.logger.Warn("bybit_private_order_stream_ping_failed", "exchange", c.name, "err", err)
				}
				return
			}
		}
	}
}

func (c *BybitV5TradeClient) readPrivateOrderStream(ctx context.Context, conn *websocket.Conn, sink OrderEventSink) error {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if isTimeoutErr(err) {
				continue
			}
			return err
		}

		events, ok, err := c.parsePrivateOrderEvents(payload)
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("bybit_private_order_stream_parse_failed", "exchange", c.name, "err", err, "payload", string(payload))
			}
			continue
		}
		if !ok {
			continue
		}
		for _, event := range events {
			select {
			case <-ctx.Done():
				return nil
			default:
				sink.PublishOrderEvent(event)
			}
		}
	}
}

// parsePrivateOrderEvents 把 Bybit private `order` topic 翻译成统一的 `OrderEvent`。
//
// 这里刻意保留“一条 websocket 消息 -> 多条 OrderEvent”的能力，
// 因为 Bybit 的 `data` 本身就是数组结构；后续若同一帧里收到多条更新，
// 我们希望上层状态机仍然逐条吸收，而不是只保留第一条。
func (c *BybitV5TradeClient) parsePrivateOrderEvents(payload []byte) ([]OrderEvent, bool, error) {
	var meta struct {
		Op    string `json:"op"`
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return nil, false, err
	}

	if strings.TrimSpace(meta.Op) != "" {
		// 这里忽略 `pong` 等控制消息，让 read loop 只把真正的订单事实往上游发布。
		return nil, false, nil
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(meta.Topic)), "order") {
		return nil, false, nil
	}

	var envelope bybitPrivateOrderEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, false, err
	}

	events := make([]OrderEvent, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		status := bybitNormalizeOrderStatus(item.OrderStatus)
		events = append(events, OrderEvent{
			Source:        "bybit_private_stream",
			Exchange:      c.name,
			ClientOrderID: strings.TrimSpace(item.OrderLinkID),
			VenueOrderID:  strings.TrimSpace(item.OrderID),
			Status:        status,
			ExecutedQty:   mustFloat(item.CumExecQty),
			AveragePrice:  mustFloat(item.AvgPrice),
			Terminal:      bybitIsTerminalOrderStatus(status),
			Canceled:      status == "CANCELED" || status == "PARTIALLY_FILLED_CANCELED" || status == "DEACTIVATED",
			ErrorMessage:  summarizeBybitOrderEventError(status, item.RejectReason, item.CancelType),
			RawPayload:    string(payload),
			OccurredAtMs:  pickPositiveInt64(parseBybitEventTime(item.UpdatedTime), parseBybitEventTime(item.CreatedTime), envelope.CreationTime),
		})
	}
	return events, len(events) > 0, nil
}

func parseBybitEventTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	out, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return out
}

func summarizeBybitOrderEventError(status, rejectReason, cancelType string) string {
	status = bybitNormalizeOrderStatus(status)
	rejectReason = strings.TrimSpace(rejectReason)
	cancelType = strings.TrimSpace(cancelType)

	switch status {
	case "REJECTED":
		return firstNonEmpty(rejectReason, "rejected")
	case "CANCELED", "PARTIALLY_FILLED_CANCELED", "DEACTIVATED":
		return firstNonEmpty(cancelType, rejectReason)
	default:
		return ""
	}
}
