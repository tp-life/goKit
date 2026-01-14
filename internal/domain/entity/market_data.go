package entity

import "time"

// MarketData 市场数据实体
type MarketData struct {
	Symbol         string     `gorm:"primaryKey;index:idx_symbol_time"`
	Exchange       string     `gorm:"primaryKey;index:idx_symbol_time"`
	SpotPrice      *float64   `gorm:"type:decimal(20,8)"`
	MarkPrice      *float64   `gorm:"type:decimal(20,8)"`
	IndexPrice     *float64   `gorm:"type:decimal(20,8)"`
	LastTradePrice *float64   `gorm:"type:decimal(20,8)"`
	UpdatedAt      time.Time  `gorm:"index:idx_symbol_time"`
	CreatedAt      time.Time
}

// MarketDataSnapshot 市场数据快照（用于历史记录）
type MarketDataSnapshot struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement"`
	Symbol        string    `gorm:"index:idx_symbol_time"`
	Exchange      string    `gorm:"index:idx_symbol_time"`
	SpotPrice     *float64  `gorm:"type:decimal(20,8)"`
	MarkPrice     *float64  `gorm:"type:decimal(20,8)"`
	IndexPrice    *float64  `gorm:"type:decimal(20,8)"`
	LastTradePrice *float64 `gorm:"type:decimal(20,8)"`
	Timestamp     time.Time `gorm:"index:idx_symbol_time"`
	CreatedAt     time.Time
}

func (MarketDataSnapshot) TableName() string {
	return "market_data_snapshots"
}
