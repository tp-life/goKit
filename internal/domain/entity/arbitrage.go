package entity

import "time"

// ArbitrageOpportunity 套利机会
type ArbitrageOpportunity struct {
	ID             uint64     `gorm:"primaryKey;autoIncrement"`
	Symbol         string     `gorm:"index"`
	ExchangeA      string     `gorm:"index"`
	ExchangeB      string     `gorm:"index"`
	PriceDiff      float64    `gorm:"type:decimal(20,8)"`
	RateDiff       float64    `gorm:"type:decimal(20,8)"`
	ArbitrageLevel string     `gorm:"type:varchar(20)"` // high/medium/none
	Profitability  float64    `gorm:"type:decimal(20,8)"` // 预期收益率
	DetectedAt     time.Time  `gorm:"index"`
	ExpiredAt      *time.Time
	Executed       bool       `gorm:"default:false"`
	CreatedAt      time.Time
}

func (ArbitrageOpportunity) TableName() string {
	return "arbitrage_opportunities"
}

// ArbitrageThreshold 套利阈值配置
type ArbitrageThreshold struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement"`
	Symbol          string    `gorm:"uniqueIndex:idx_symbol_exchange"`
	ExchangeA       string    `gorm:"uniqueIndex:idx_symbol_exchange"`
	ExchangeB       string    `gorm:"uniqueIndex:idx_symbol_exchange"`
	HighThreshold   float64   `gorm:"type:decimal(20,8);default:0.0005"`   // 0.05%
	MediumThreshold float64   `gorm:"type:decimal(20,8);default:0.0002"`   // 0.02%
	MinProfitability float64  `gorm:"type:decimal(20,8);default:0.0001"`   // 0.01%
	Enabled         bool      `gorm:"default:true"`
	UpdatedAt       time.Time
	CreatedAt       time.Time
}

func (ArbitrageThreshold) TableName() string {
	return "arbitrage_thresholds"
}

// ExchangeConfig 交易所配置
type ExchangeConfig struct {
	ID                uint64        `gorm:"primaryKey;autoIncrement"`
	Name              string        `gorm:"uniqueIndex"`
	Type              string        `gorm:"type:varchar(20)"` // spot/perpetual
	WSURL             string
	RESTURL           string
	ReconnectInterval time.Duration
	RateLimit         int           `gorm:"default:100"`
	Enabled           bool          `gorm:"default:true"`
	UpdatedAt         time.Time
	CreatedAt         time.Time
}

func (ExchangeConfig) TableName() string {
	return "exchange_configs"
}
