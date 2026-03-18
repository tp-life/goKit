package entity

type ExecutionPlan struct {
	ID                 uint   `gorm:"primaryKey" json:"id"`
	BatchID            string `gorm:"index:idx_plan_batch;size:64" json:"batch_id"`
	OpportunityBatchID string `gorm:"index:idx_plan_batch;size:64" json:"opportunity_batch_id"`
	PlanKey            string `gorm:"uniqueIndex;size:160" json:"plan_key"`

	Symbol           string `gorm:"index:idx_plan_symbol;size:64" json:"symbol"`
	LongExchange     string `gorm:"size:32" json:"long_exchange"`
	ShortExchange    string `gorm:"size:32" json:"short_exchange"`
	LongVenueSymbol  string `gorm:"size:64" json:"long_venue_symbol"`
	ShortVenueSymbol string `gorm:"size:64" json:"short_venue_symbol"`

	Status    string `gorm:"size:32" json:"status"`
	LongSide  string `gorm:"size:16" json:"long_side"`
	ShortSide string `gorm:"size:16" json:"short_side"`
	EntryMode string `gorm:"size:16" json:"entry_mode"`
	ExitMode  string `gorm:"size:16" json:"exit_mode"`

	TargetLeverage       float64 `json:"target_leverage"`
	CapitalAllocatedUSDT float64 `json:"capital_allocated_usdt"`
	TargetNotionalUSDT   float64 `json:"target_notional_usdt"`
	RoundedNotionalUSDT  float64 `json:"rounded_notional_usdt"`

	LongEntryPrice       float64 `json:"long_entry_price"`
	ShortEntryPrice      float64 `json:"short_entry_price"`
	LongQty              float64 `json:"long_qty"`
	ShortQty             float64 `json:"short_qty"`
	LongMinQty           float64 `json:"long_min_qty"`
	ShortMinQty          float64 `json:"short_min_qty"`
	LongMinNotionalUSDT  float64 `json:"long_min_notional_usdt"`
	ShortMinNotionalUSDT float64 `json:"short_min_notional_usdt"`

	CrossVenueBasisBps     float64 `json:"cross_venue_basis_bps"`
	FundingCarryPNL        float64 `json:"funding_carry_pnl"`
	EntryFeePNL            float64 `json:"entry_fee_pnl"`
	ExitFeePNL             float64 `json:"exit_fee_pnl"`
	SlippagePNL            float64 `json:"slippage_pnl"`
	SafetyBufferPNL        float64 `json:"safety_buffer_pnl"`
	EntryPenaltyBps        float64 `json:"entry_penalty_bps"`
	ExitPenaltyBps         float64 `json:"exit_penalty_bps"`
	HedgePenaltyBps        float64 `json:"hedge_penalty_bps"`
	ExecutionPenaltyBps    float64 `json:"execution_penalty_bps"`
	ExecutionPenaltyModel  string  `gorm:"size:64" json:"execution_penalty_model"`
	ExecutionPenaltyBucket string  `gorm:"size:64" json:"execution_penalty_bucket"`
	NetExpectedPNL         float64 `json:"net_expected_pnl"`
	NetExpectedPNLBps      float64 `json:"net_expected_pnl_bps"`
	Score                  float64 `gorm:"index:idx_plan_score" json:"score"`

	EarliestFundingTimeMs        int64   `json:"earliest_funding_time_ms"`
	LatestFundingTimeMs          int64   `json:"latest_funding_time_ms"`
	ProjectedFundingTimeMs       int64   `json:"projected_funding_time_ms"`
	RequiredEntryByFundingTimeMs int64   `json:"required_entry_by_funding_time_ms"`
	LongFundingEventCount        int     `json:"long_funding_event_count"`
	ShortFundingEventCount       int     `json:"short_funding_event_count"`
	FundingWindowHours           float64 `json:"funding_window_hours"`
	FundingComputationMode       string  `gorm:"size:64" json:"funding_computation_mode"`
	EntryWindowOpenMs            int64   `json:"entry_window_open_ms"`
	EntryWindowCloseMs           int64   `json:"entry_window_close_ms"`
	TargetCloseTimeMs            int64   `json:"target_close_time_ms"`
	AsOfTimeMs                   int64   `gorm:"index:idx_plan_batch" json:"as_of_time_ms"`
	ReadyNow                     bool    `json:"ready_now"`
	RejectReason                 string  `gorm:"size:255" json:"reject_reason,omitempty"`
	TimestampModel
}

func (ExecutionPlan) TableName() string { return "execution_plans" }
