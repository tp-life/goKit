package polymarket

import "goKit/internal/domain/entity"

// ActiveMarketView 是供服务层直接消费的市场投影视图。
type ActiveMarketView = entity.ActiveMarket

// OrderStatusResponse 表示 CLOB 返回的原始订单状态载荷。
type OrderStatusResponse struct {
	Status       string `json:"status"`
	OriginalSize string `json:"original_size"`
	SizeMatched  string `json:"size_matched"`
}

// OrderCreateResponse 表示提交订单后 CLOB 返回的最小响应结构。
type OrderCreateResponse struct {
	OrderID string `json:"orderID"`
}

// TickSizeResponse 表示 tick-size 元数据接口的原始响应。
type TickSizeResponse struct {
	MinimumTickSize FlexibleText `json:"minimum_tick_size"`
}

// NegRiskResponse 表示 neg-risk 元数据接口的原始响应。
type NegRiskResponse struct {
	NegRisk bool `json:"neg_risk"`
}

// FeeRateResponse 表示手续费元数据接口的原始响应。
type FeeRateResponse struct {
	BaseFee FlexibleInt `json:"base_fee"`
}

// RPCBalanceResponse 表示 Polygon JSON-RPC 余额查询响应。
type RPCBalanceResponse struct {
	Result string `json:"result"`
}

// GammaMarketPayload 表示机器人当前需要的 Gamma market 字段子集。
type GammaMarketPayload struct {
	Outcomes      any `json:"outcomes"`
	OutcomePrices any `json:"outcomePrices"`
	ClobTokenIDs  any `json:"clobTokenIds"`
}

// GammaEventPayload 表示活跃市场发现流程需要的 Gamma event 字段子集。
type GammaEventPayload struct {
	Closed    bool                 `json:"closed"`
	EndDate   string               `json:"endDate"`
	StartTime string               `json:"startTime"`
	Markets   []GammaMarketPayload `json:"markets"`
}

// RedeemResult 表示兑奖交易的链上执行结果。
type RedeemResult struct {
	TxHash        string
	TransactionID string
	BlockNumber   uint64
	GasUsed       uint64
	Status        uint64
	State         string
	ProxyWallet   string
	ErrorMessage  string
}
