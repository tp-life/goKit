package entity

import "time"

type Opportunity struct {
	ID uint `gorm:"primaryKey" json:"id"`

	BatchID    string `gorm:"index;size:64" json:"batch_id"`
	AsOfTimeMs int64  `gorm:"index" json:"as_of_time_ms"`

	Symbol           string `gorm:"index;size:64" json:"symbol"`
	LongExchange     string `gorm:"size:32" json:"long_exchange"`
	ShortExchange    string `gorm:"size:32" json:"short_exchange"`
	LongVenueSymbol  string `gorm:"size:64" json:"long_venue_symbol"`
	ShortVenueSymbol string `gorm:"size:64" json:"short_venue_symbol"`

	LongFundingRate    float64 `json:"long_funding_rate"`
	ShortFundingRate   float64 `json:"short_funding_rate"`
	LongFundingTimeMs  int64   `json:"long_funding_time_ms"`
	ShortFundingTimeMs int64   `json:"short_funding_time_ms"`

	LongFundingHourly  float64 `json:"long_funding_hourly"`
	ShortFundingHourly float64 `json:"short_funding_hourly"`
	GrossEdgeHourly    float64 `json:"gross_edge_hourly"`

	LongBidPrice   float64 `json:"long_bid_price"`
	LongAskPrice   float64 `json:"long_ask_price"`
	ShortBidPrice  float64 `json:"short_bid_price"`
	ShortAskPrice  float64 `json:"short_ask_price"`
	LongMarkPrice  float64 `json:"long_mark_price"`
	ShortMarkPrice float64 `json:"short_mark_price"`

	GrossFundingPNL float64 `json:"gross_funding_pnl"`
	EntryFeePNL     float64 `json:"entry_fee_pnl"`
	ExitFeePNL      float64 `json:"exit_fee_pnl"`
	SlippagePNL     float64 `json:"slippage_pnl"`
	SafetyBufferPNL float64 `json:"safety_buffer_pnl"`
	NetExpectedPNL  float64 `json:"net_expected_pnl"`
	NetExpectedBps  float64 `json:"net_expected_bps"`
	BasisBps        float64 `json:"basis_bps"`

	Score float64 `json:"score"`

	EarliestFundingTimeMs        int64   `json:"earliest_funding_time_ms"`
	LatestFundingTimeMs          int64   `json:"latest_funding_time_ms"`
	ProjectedFundingTimeMs       int64   `json:"projected_funding_time_ms"`
	RequiredEntryByFundingTimeMs int64   `json:"required_entry_by_funding_time_ms"`
	LongFundingEventCount        int     `json:"long_funding_event_count"`
	ShortFundingEventCount       int     `json:"short_funding_event_count"`
	FundingWindowHours           float64 `json:"funding_window_hours"`
	FundingComputationMode       string  `gorm:"size:64" json:"funding_computation_mode"`

	Status               string    `gorm:"size:32;index" json:"status"`
	RejectReason         string    `gorm:"size:255" json:"reject_reason"`
	EligibleForExecution bool      `gorm:"index" json:"eligible_for_execution"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (Opportunity) TableName() string { return "opportunities" }
