package entity

type FundingSnapshot struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Exchange string `gorm:"index:idx_funding_symbol_exchange_time,priority:1;size:32" json:"exchange"`
	Symbol   string `gorm:"index:idx_funding_symbol_exchange_time,priority:2;size:64" json:"symbol"`

	VenueSymbol string `gorm:"size:64" json:"venue_symbol"`

	MarkPrice            float64 `json:"mark_price"`
	IndexPrice           float64 `json:"index_price"`
	EstimatedSettlePrice float64 `json:"estimated_settle_price"`
	FundingRate          float64 `json:"funding_rate"`
	FundingTimeMs        int64   `json:"funding_time_ms"`
	FundingIntervalHours int     `json:"funding_interval_hours"`
	EventTimeMs          int64   `gorm:"index:idx_funding_symbol_exchange_time,priority:3" json:"event_time_ms"`
	TimestampModel
}

func (FundingSnapshot) TableName() string { return "funding_snapshots" }
