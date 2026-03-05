package entity

import "time"

// StockInfo 股票基础维度表
type StockInfo struct {
	StockCode string     `gorm:"primaryKey;size:16"`
	StockName string     `gorm:"size:64;not null"`
	Industry  string     `gorm:"size:64;index"`
	Exchange  string     `gorm:"size:16"`
	ListDate  *time.Time `gorm:"type:date"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime"`
}

func (StockInfo) TableName() string { return "stock_info" }

// InstitutionInfo 机构维度表
type InstitutionInfo struct {
	InstID    uint64    `gorm:"primaryKey;autoIncrement"`
	InstName  string    `gorm:"size:128;uniqueIndex"`
	InstType  string    `gorm:"size:32;index"` // 如: 国家队, 普通机构
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (InstitutionInfo) TableName() string { return "institution_info" }

// StockHoldingRecord 持仓事实流水记录
type StockHoldingRecord struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"`
	// 【关键修复】：将 StockCode, InstID, ReportDate 联合组成唯一的 idx_holding_unique
	StockCode  string    `gorm:"size:16;uniqueIndex:idx_holding_unique"`
	InstID     uint64    `gorm:"uniqueIndex:idx_holding_unique"`
	ReportDate time.Time `gorm:"type:date;uniqueIndex:idx_holding_unique"`
	HoldCount  int64
	HoldRatio  float64 `gorm:"type:decimal(10,4)"`
	ChangeType string  `gorm:"size:32"`
}

func (StockHoldingRecord) TableName() string { return "stock_holding_record" }

// StockDailyQuote 每日行情流水表 (腾讯数据源)
type StockDailyQuote struct {
	StockCode string    `gorm:"primaryKey;size:16"`
	TradeDate time.Time `gorm:"primaryKey;type:date"`
	Open      float64   `gorm:"type:decimal(10,2)"`
	High      float64   `gorm:"type:decimal(10,2)"`
	Low       float64   `gorm:"type:decimal(10,2)"`
	Close     float64   `gorm:"type:decimal(10,2)"`
	Volume    int64     // 成交量（手）
}

func (StockDailyQuote) TableName() string { return "stock_daily_quote" }
