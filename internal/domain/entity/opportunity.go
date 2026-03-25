package entity

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type OpportunityProjection struct {
	ProjectionRank               int     `json:"projection_rank"`
	IsBestProjection             bool    `json:"is_best_projection"`
	ProjectedFundingTimeMs       int64   `json:"projected_funding_time_ms"`
	RequiredEntryByFundingTimeMs int64   `json:"required_entry_by_funding_time_ms"`
	LongFundingEventCount        int     `json:"long_funding_event_count"`
	ShortFundingEventCount       int     `json:"short_funding_event_count"`
	FundingWindowHours           float64 `json:"funding_window_hours"`
	CarryRate                    float64 `json:"carry_rate"`
	CarryRateHourlyEquivalent    float64 `json:"carry_rate_hourly_equivalent"`
	GrossFundingPNL              float64 `json:"gross_funding_pnl"`
	NetExpectedPNL               float64 `json:"net_expected_pnl"`
	NetExpectedBps               float64 `json:"net_expected_bps"`
	ComputationMode              string  `json:"computation_mode"`
}

type OpportunityFundingRule struct {
	Exchange             string  `json:"exchange"`
	VenueSymbol          string  `json:"venue_symbol"`
	FundingIntervalHours int     `json:"funding_interval_hours"`
	NextFundingTimeMs    int64   `json:"next_funding_time_ms"`
	CurrentFundingRate   float64 `json:"current_funding_rate"`
	ClampSource          string  `json:"clamp_source"`
	EffectiveFloorRate   float64 `json:"effective_floor_rate"`
	EffectiveCapRate     float64 `json:"effective_cap_rate"`
	ForecastRegime       string  `json:"forecast_regime"`
	ForecastConfidence   string  `json:"forecast_confidence"`
	MetadataSummary      string  `json:"metadata_summary"`
}

type Opportunity struct {
	ID uint `gorm:"primaryKey" json:"id"`

	BatchID    string `gorm:"index;index:idx_opp_batch_score_pnl_time,priority:1;size:64" json:"batch_id"`
	AsOfTimeMs int64  `gorm:"index;index:idx_opp_batch_score_pnl_time,priority:4,sort:desc" json:"as_of_time_ms"`

	Symbol           string `gorm:"index;size:64" json:"symbol"`
	LongExchange     string `gorm:"size:32" json:"long_exchange"`
	ShortExchange    string `gorm:"size:32" json:"short_exchange"`
	LongVenueSymbol  string `gorm:"size:64" json:"long_venue_symbol"`
	ShortVenueSymbol string `gorm:"size:64" json:"short_venue_symbol"`

	LongFundingRate           float64 `json:"long_funding_rate"`
	ShortFundingRate          float64 `json:"short_funding_rate"`
	LongFundingTimeMs         int64   `json:"long_funding_time_ms"`
	ShortFundingTimeMs        int64   `json:"short_funding_time_ms"`
	LongFundingIntervalHours  int     `json:"long_funding_interval_hours"`
	ShortFundingIntervalHours int     `json:"short_funding_interval_hours"`

	LongFundingHourly         float64 `json:"long_funding_hourly"`
	ShortFundingHourly        float64 `json:"short_funding_hourly"`
	GrossEdgeHourly           float64 `json:"gross_edge_hourly"`
	LongFutureFundingRate     float64 `json:"long_future_funding_rate"`
	ShortFutureFundingRate    float64 `json:"short_future_funding_rate"`
	FundingEstimateMode       string  `gorm:"size:64" json:"funding_estimate_mode"`
	FundingEstimateConfidence string  `gorm:"size:32" json:"funding_estimate_confidence"`

	LongBidPrice   float64 `json:"long_bid_price"`
	LongAskPrice   float64 `json:"long_ask_price"`
	ShortBidPrice  float64 `json:"short_bid_price"`
	ShortAskPrice  float64 `json:"short_ask_price"`
	LongMarkPrice  float64 `json:"long_mark_price"`
	ShortMarkPrice float64 `json:"short_mark_price"`

	GrossFundingPNL        float64 `json:"gross_funding_pnl"`
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
	NetExpectedPNL         float64 `gorm:"index:idx_opp_batch_score_pnl_time,priority:3,sort:desc" json:"net_expected_pnl"`
	NetExpectedBps         float64 `json:"net_expected_bps"`
	BasisBps               float64 `json:"basis_bps"`
	MaxAllowedBasisBps     float64 `json:"max_allowed_basis_bps"`

	Score float64 `gorm:"index:idx_opp_batch_score_pnl_time,priority:2,sort:desc" json:"score"`

	EarliestFundingTimeMs        int64                   `json:"earliest_funding_time_ms"`
	LatestFundingTimeMs          int64                   `json:"latest_funding_time_ms"`
	ProjectedFundingTimeMs       int64                   `json:"projected_funding_time_ms"`
	RequiredEntryByFundingTimeMs int64                   `json:"required_entry_by_funding_time_ms"`
	LongFundingEventCount        int                     `json:"long_funding_event_count"`
	ShortFundingEventCount       int                     `json:"short_funding_event_count"`
	FundingWindowHours           float64                 `json:"funding_window_hours"`
	FundingComputationMode       string                  `gorm:"size:64" json:"funding_computation_mode"`
	ProjectionDetailsJSON        string                  `gorm:"type:text" json:"-"`
	LongFundingRuleJSON          string                  `gorm:"type:text" json:"-"`
	ShortFundingRuleJSON         string                  `gorm:"type:text" json:"-"`
	ProjectionDetails            []OpportunityProjection `gorm:"-" json:"projection_details,omitempty"`
	LongFundingRule              OpportunityFundingRule  `gorm:"-" json:"long_funding_rule"`
	ShortFundingRule             OpportunityFundingRule  `gorm:"-" json:"short_funding_rule"`

	Status               string    `gorm:"size:32;index" json:"status"`
	RejectReason         string    `gorm:"size:255" json:"reject_reason"`
	EligibleForExecution bool      `gorm:"index" json:"eligible_for_execution"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (Opportunity) TableName() string { return "opportunities" }

func (o *Opportunity) BeforeSave(_ *gorm.DB) error {
	if raw, err := json.Marshal(o.ProjectionDetails); err == nil {
		o.ProjectionDetailsJSON = string(raw)
	} else {
		return err
	}
	if raw, err := json.Marshal(o.LongFundingRule); err == nil {
		o.LongFundingRuleJSON = string(raw)
	} else {
		return err
	}
	if raw, err := json.Marshal(o.ShortFundingRule); err == nil {
		o.ShortFundingRuleJSON = string(raw)
	} else {
		return err
	}
	return nil
}

func (o *Opportunity) AfterFind(_ *gorm.DB) error {
	if o.ProjectionDetailsJSON != "" {
		if err := json.Unmarshal([]byte(o.ProjectionDetailsJSON), &o.ProjectionDetails); err != nil {
			return err
		}
	}
	if o.LongFundingRuleJSON != "" {
		if err := json.Unmarshal([]byte(o.LongFundingRuleJSON), &o.LongFundingRule); err != nil {
			return err
		}
	}
	if o.ShortFundingRuleJSON != "" {
		if err := json.Unmarshal([]byte(o.ShortFundingRuleJSON), &o.ShortFundingRule); err != nil {
			return err
		}
	}
	return nil
}
