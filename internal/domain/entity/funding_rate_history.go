package entity

type FundingRateHistory struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Exchange string `gorm:"uniqueIndex:idx_funding_rate_history_exchange_symbol_time,priority:1;index:idx_funding_rate_history_query,priority:1;size:32" json:"exchange"`
	Symbol   string `gorm:"uniqueIndex:idx_funding_rate_history_exchange_symbol_time,priority:2;index:idx_funding_rate_history_query,priority:2;size:64" json:"symbol"`

	VenueSymbol string `gorm:"size:64" json:"venue_symbol"`

	FundingRate   float64 `json:"funding_rate"`
	MarkPrice     float64 `json:"mark_price"`
	FundingTimeMs int64   `gorm:"uniqueIndex:idx_funding_rate_history_exchange_symbol_time,priority:3;index:idx_funding_rate_history_query,priority:3,sort:desc;index:idx_funding_rate_history_time" json:"funding_time_ms"`
	Source        string  `gorm:"size:32" json:"source"`
	TimestampModel
}

func (FundingRateHistory) TableName() string { return "funding_rate_history" }
