package entity

import "time"

// ComparisonResult 对比结果（内存实体，不持久化）
type ComparisonResult struct {
	Symbol          string
	Exchanges       []string
	Prices          map[string]*MarketData
	Rates           map[string]*FundingRate
	Comparisons     []ExchangePairComparison
	BestArbitrage   *ArbitragePath
	LastUpdated     time.Time
}

// ExchangePairComparison 交易所对对比
type ExchangePairComparison struct {
	ExchangeA      string
	ExchangeB      string
	PriceDiff      float64
	RateDiff       float64
	ArbitrageLevel string
	Profitability  float64
}

// ArbitragePath 套利路径
type ArbitragePath struct {
	Path          []string
	Symbol        string
	EntryPrice    float64
	ExitPrice     float64
	RateDiff      float64
	Fees          float64
	NetProfit     float64
	RiskScore     float64
	ExecutionTime time.Duration
}
