package dto

// ManualOrderReq 表示 dashboard 发起的手动下单请求。
type ManualOrderReq struct {
	Action      string  `json:"action"`
	Outcome     string  `json:"outcome"`
	Amount      float64 `json:"amount"`
	Probability float64 `json:"probability"`
	Intent      string  `json:"intent"`
}

// ManualOrderResp 表示手动下单接口返回的结果。
type ManualOrderResp struct {
	OK      bool    `json:"ok"`
	Action  string  `json:"action"`
	Intent  string  `json:"intent"`
	Outcome string  `json:"outcome"`
	OrderID string  `json:"order_id"`
	Price   float64 `json:"price,omitempty"`
	Size    float64 `json:"size,omitempty"`
	Error   string  `json:"error,omitempty"`
}
