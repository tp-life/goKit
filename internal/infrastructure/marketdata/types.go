package marketdata

import "encoding/json"

// BinanceTickerPriceResponse 表示 Binance ticker 接口返回的价格载荷。
type BinanceTickerPriceResponse struct {
	Price string `json:"price"`
}

// BinanceTradeMessage 表示 Binance websocket 推送的简化价格消息。
type BinanceTradeMessage struct {
	Price     string `json:"p"`
	LastPrice string `json:"c"`
}

// CryptoPriceResponse 表示 PTB 接口返回的开盘价和收盘价。
type CryptoPriceResponse struct {
	OpenPrice  any `json:"openPrice"`
	ClosePrice any `json:"closePrice"`
}

// RTDSSubscribeRequest 表示订阅 RTDS 行情时发送的请求体。
type RTDSSubscribeRequest struct {
	Action        string             `json:"action"`
	Subscriptions []RTDSSubscription `json:"subscriptions"`
}

// RTDSSubscription 表示单个 RTDS 订阅项。
type RTDSSubscription struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Filters string `json:"filters"`
}

// RTDSMessage 表示 RTDS websocket 返回的通用消息结构。
type RTDSMessage struct {
	Topic   string          `json:"topic"`
	Value   any             `json:"value"`
	Data    json.RawMessage `json:"data"`
	Payload json.RawMessage `json:"payload"`
}

// RTDSPayload 表示 RTDS 中包裹在 payload 内的价格消息体。
type RTDSPayload struct {
	Symbol string          `json:"symbol"`
	Value  any             `json:"value"`
	Data   json.RawMessage `json:"data"`
}

// RTDSDataPoint 表示 RTDS 消息中可能出现的单个价格点位。
type RTDSDataPoint struct {
	Value any `json:"value"`
}
