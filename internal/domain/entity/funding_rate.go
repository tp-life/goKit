package entity

import "time"

// FundingRate 资金费率实体
type FundingRate struct {
	Symbol      string    `gorm:"primaryKey;index:idx_symbol_time"`
	Exchange    string    `gorm:"primaryKey;index:idx_symbol_time"`
	Rate        float64   `gorm:"type:decimal(20,8)"`  // 原始费率
	Rate8H      float64   `gorm:"column:rate8_h;type:decimal(20,8)"`  // 8小时周期费率（统一后）
	NextFunding time.Time `gorm:"index"`
	UpdatedAt   time.Time `gorm:"index:idx_symbol_time"`
	CreatedAt   time.Time
}

func (FundingRate) TableName() string {
	return "funding_rate"
}

// FundingRateHistory 资金费率历史
type FundingRateHistory struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	Symbol      string    `gorm:"index:idx_symbol_time"`
	Exchange    string    `gorm:"index:idx_symbol_time"`
	Rate        float64   `gorm:"type:decimal(20,8)"`
	Rate8H      float64   `gorm:"column:rate8_h;type:decimal(20,8)"`
	NextFunding time.Time
	Timestamp   time.Time `gorm:"index:idx_symbol_time"`
	CreatedAt   time.Time
}

func (FundingRateHistory) TableName() string {
	return "funding_rate_history"
}
