package dto

type GoldenPitResp struct {
	StockCode   string  `json:"stock_code"`
	StockName   string  `json:"stock_name"`
	Institution string  `json:"institution"`
	ChangeType  string  `json:"change_type"`
	CostPrice   float64 `json:"cost_price"`
	LatestPrice float64 `json:"latest_price"`
	DropRatio   float64 `json:"drop_ratio"` // 跌幅百分比
}
