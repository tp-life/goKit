package entity

import "time"

// StockInfo 股票维度表
type StockInfo struct {
	StockCode string    `gorm:"primaryKey;size:16"`
	StockName string    `gorm:"size:64;not null"`
	Industry  string    `gorm:"size:64;index"`
	Exchange  string    `gorm:"size:16"`
	ListDate  time.Time `gorm:"type:date"`
	Status    int8      `gorm:"default:1"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// InstitutionInfo 机构维度表
type InstitutionInfo struct {
	InstID    uint64    `gorm:"primaryKey;autoIncrement"`
	InstName  string    `gorm:"size:128;uniqueIndex"`
	InstType  string    `gorm:"size:32;index"` // 如: 国家队, 社保
	Tags      string    `gorm:"type:json"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// StockHoldingRecord 持仓明细事实表
type StockHoldingRecord struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	StockCode   string    `gorm:"size:16;not null;uniqueIndex:uk_holding"`
	InstID      uint64    `gorm:"not null;uniqueIndex:uk_holding"`
	ReportDate  time.Time `gorm:"type:date;not null;uniqueIndex:uk_holding"`
	HoldCount   int64     `gorm:"not null"`
	HoldRatio   float64   `gorm:"type:decimal(10,4);not null"`
	ChangeType  string    `gorm:"size:16;not null"`
	ChangeCount int64     `gorm:"default:0"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`

	// Gorm 预加载关联
	Stock       StockInfo       `gorm:"foreignKey:StockCode;references:StockCode"`
	Institution InstitutionInfo `gorm:"foreignKey:InstID;references:InstID"`
}

// StockDailyQuote 日线行情表
type StockDailyQuote struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement"`
	StockCode    string    `gorm:"size:16;not null;uniqueIndex:uk_daily_quote"`
	TradeDate    time.Time `gorm:"type:date;not null;uniqueIndex:uk_daily_quote"`
	Open         float64   `gorm:"type:decimal(10,3)"`
	Close        float64   `gorm:"type:decimal(10,3)"`
	High         float64   `gorm:"type:decimal(10,3)"`
	Low          float64   `gorm:"type:decimal(10,3)"`
	Volume       int64     `gorm:"not null"`
	TurnoverRate float64   `gorm:"type:decimal(10,4)"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}
