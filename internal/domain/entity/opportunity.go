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
	StrategyMode                 string  `json:"strategy_mode,omitempty"`
	IncludedSegmentCount         int     `json:"included_segment_count,omitempty"`
	NextReviewTimeMs             int64   `json:"next_review_time_ms,omitempty"`
	SyncBoundaryTimeMs           int64   `json:"sync_boundary_time_ms,omitempty"`
	PathEndReason                string  `json:"path_end_reason,omitempty"`
}

type OpportunityFundingRule struct {
	Exchange                     string                           `json:"exchange"`
	VenueSymbol                  string                           `json:"venue_symbol"`
	FundingIntervalHours         int                              `json:"funding_interval_hours"`
	NextFundingTimeMs            int64                            `json:"next_funding_time_ms"`
	CurrentFundingRate           float64                          `json:"current_funding_rate"`
	CurrentFundingRank           int                              `json:"current_funding_rank,omitempty"`
	CurrentFundingRankTotal      int                              `json:"current_funding_rank_total,omitempty"`
	CurrentFundingRankPercentile float64                          `json:"current_funding_rank_percentile,omitempty"`
	HistorySampleCount           int                              `json:"history_sample_count,omitempty"`
	HistoryMeanRate              float64                          `json:"history_mean_rate,omitempty"`
	HistoryNegativeRatio         float64                          `json:"history_negative_ratio,omitempty"`
	HistoryPositiveRatio         float64                          `json:"history_positive_ratio,omitempty"`
	HistoricalSupportRatio       float64                          `json:"historical_support_ratio,omitempty"`
	CurrentHistoricalPercentile  float64                          `json:"current_historical_percentile,omitempty"`
	EstimatedEventRate           float64                          `json:"estimated_event_rate,omitempty"`
	EstimatedAnnualizedCarryRate float64                          `json:"estimated_annualized_carry_rate,omitempty"`
	EstimatedAnnualizedNetRate   float64                          `json:"estimated_annualized_net_rate,omitempty"`
	SuggestedFundingEvents       int                              `json:"suggested_funding_events,omitempty"`
	SuggestedHoldHours           float64                          `json:"suggested_hold_hours,omitempty"`
	SuggestedFundingTimeMs       int64                            `json:"suggested_funding_time_ms,omitempty"`
	SuggestedGrossFundingPNL     float64                          `json:"suggested_gross_funding_pnl,omitempty"`
	SuggestedNetPNL              float64                          `json:"suggested_net_pnl,omitempty"`
	LongHoldEligible             bool                             `json:"long_hold_eligible,omitempty"`
	LongHoldReason               string                           `json:"long_hold_reason,omitempty"`
	ClampSource                  string                           `json:"clamp_source"`
	EffectiveFloorRate           float64                          `json:"effective_floor_rate"`
	EffectiveCapRate             float64                          `json:"effective_cap_rate"`
	ForecastRegime               string                           `json:"forecast_regime"`
	ForecastConfidence           string                           `json:"forecast_confidence"`
	MetadataSummary              string                           `json:"metadata_summary"`
	HistorySeries                []OpportunityFundingHistoryPoint `json:"history_series,omitempty"`
}

type OpportunityFundingHistoryPoint struct {
	FundingTimeMs int64   `json:"funding_time_ms"`
	FundingRate   float64 `json:"funding_rate"`
	MarkPrice     float64 `json:"mark_price,omitempty"`
}

// OpportunityFundingSegment 用于把 rolling_cycle_aligned 模式拆出来的每个 settlement 段直接持久化。
//
// 这组字段不是为了给旧策略做数学计算，而是为了让 review 时能一眼看出：
// 1. 当前 entry path 包含了哪些 settlement；
// 2. 哪些段使用了 forecast；
// 3. 当前方向是否在下一段发生了反转。
type OpportunityFundingSegment struct {
	SegmentRank            int     `json:"segment_rank"`
	SettlementTimeMs       int64   `json:"settlement_time_ms"`
	SegmentType            string  `json:"segment_type"`
	UsesForecast           bool    `json:"uses_forecast"`
	SharedSettlement       bool    `json:"shared_settlement"`
	HeldLongExchange       string  `json:"held_long_exchange"`
	HeldShortExchange      string  `json:"held_short_exchange"`
	OptimalLongExchange    string  `json:"optimal_long_exchange"`
	OptimalShortExchange   string  `json:"optimal_short_exchange"`
	DirectionMatchesHeld   bool    `json:"direction_matches_held"`
	LongLegSettles         bool    `json:"long_leg_settles"`
	ShortLegSettles        bool    `json:"short_leg_settles"`
	LongLegRate            float64 `json:"long_leg_rate"`
	ShortLegRate           float64 `json:"short_leg_rate"`
	LongLegEventIndex      int     `json:"long_leg_event_index"`
	ShortLegEventIndex     int     `json:"short_leg_event_index"`
	CarryRate              float64 `json:"carry_rate"`
	HeldDirectionCarryRate float64 `json:"held_direction_carry_rate"`
	ComputationMode        string  `json:"computation_mode"`
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

	GrossFundingPNL                              float64 `json:"gross_funding_pnl"`
	EntryFeePNL                                  float64 `json:"entry_fee_pnl"`
	ExitFeePNL                                   float64 `json:"exit_fee_pnl"`
	SlippagePNL                                  float64 `json:"slippage_pnl"`
	SafetyBufferPNL                              float64 `json:"safety_buffer_pnl"`
	EntryPenaltyBps                              float64 `json:"entry_penalty_bps"`
	ExitPenaltyBps                               float64 `json:"exit_penalty_bps"`
	HedgePenaltyBps                              float64 `json:"hedge_penalty_bps"`
	ExecutionPenaltyBps                          float64 `json:"execution_penalty_bps"`
	ExecutionPenaltyModel                        string  `gorm:"size:64" json:"execution_penalty_model"`
	ExecutionPenaltyBucket                       string  `gorm:"size:64" json:"execution_penalty_bucket"`
	NetExpectedPNL                               float64 `gorm:"index:idx_opp_batch_score_pnl_time,priority:3,sort:desc" json:"net_expected_pnl"`
	NetExpectedBps                               float64 `json:"net_expected_bps"`
	BasisBps                                     float64 `json:"basis_bps"`
	MaxAllowedBasisBps                           float64 `json:"max_allowed_basis_bps"`
	SameExchangePriceRiskAllowed                 bool    `json:"same_exchange_price_risk_allowed"`
	SameExchangePriceRiskReason                  string  `gorm:"size:64" json:"same_exchange_price_risk_reason,omitempty"`
	SameExchangePriceShockCurrentMarkPrice       float64 `json:"same_exchange_price_shock_current_mark_price"`
	SameExchangePriceShockBaselineMarkPrice      float64 `json:"same_exchange_price_shock_baseline_mark_price"`
	SameExchangePriceShockRatio                  float64 `json:"same_exchange_price_shock_ratio"`
	SameExchangeBasisUsesPaybackModel            bool    `json:"same_exchange_basis_uses_payback_model"`
	SameExchangeBasisCostBps                     float64 `json:"same_exchange_basis_cost_bps"`
	SameExchangeBasisCarryPerEventBps            float64 `json:"same_exchange_basis_carry_per_event_bps"`
	SameExchangeBasisPaybackFundingEvents        float64 `json:"same_exchange_basis_payback_funding_events"`
	SameExchangeBasisAllowed                     bool    `json:"same_exchange_basis_allowed"`
	SameExchangeBasisReason                      string  `gorm:"size:64" json:"same_exchange_basis_reason"`
	SameExchangeBasisRiskSizeMultiplier          float64 `json:"same_exchange_basis_risk_size_multiplier"`
	SameExchangeLongHoldUsingHistoryEstimate     bool    `json:"same_exchange_long_hold_using_history_estimate"`
	SameExchangeLongHoldSuggestedFundingEvents   int     `json:"same_exchange_long_hold_suggested_funding_events,omitempty"`
	SameExchangeLongHoldSuggestedHoldHours       float64 `json:"same_exchange_long_hold_suggested_hold_hours,omitempty"`
	SameExchangeLongHoldSuggestedFundingTimeMs   int64   `json:"same_exchange_long_hold_suggested_funding_time_ms,omitempty"`
	SameExchangeLongHoldSuggestedGrossFundingPNL float64 `json:"same_exchange_long_hold_suggested_gross_funding_pnl,omitempty"`
	SameExchangeLongHoldSuggestedNetPNL          float64 `json:"same_exchange_long_hold_suggested_net_pnl,omitempty"`

	Score float64 `gorm:"index:idx_opp_batch_score_pnl_time,priority:2,sort:desc" json:"score"`

	StrategyMode                 string                      `gorm:"size:64" json:"strategy_mode"`
	EarliestFundingTimeMs        int64                       `json:"earliest_funding_time_ms"`
	LatestFundingTimeMs          int64                       `json:"latest_funding_time_ms"`
	ProjectedFundingTimeMs       int64                       `json:"projected_funding_time_ms"`
	RequiredEntryByFundingTimeMs int64                       `json:"required_entry_by_funding_time_ms"`
	LongFundingEventCount        int                         `json:"long_funding_event_count"`
	ShortFundingEventCount       int                         `json:"short_funding_event_count"`
	FundingWindowHours           float64                     `json:"funding_window_hours"`
	FundingComputationMode       string                      `gorm:"size:64" json:"funding_computation_mode"`
	NextReviewTimeMs             int64                       `json:"next_review_time_ms"`
	SyncBoundaryTimeMs           int64                       `json:"sync_boundary_time_ms"`
	EntryPathSegmentCount        int                         `json:"entry_path_segment_count"`
	EntryPathStopReason          string                      `gorm:"size:64" json:"entry_path_stop_reason"`
	ProjectionDetailsJSON        string                      `gorm:"type:text" json:"-"`
	LongFundingRuleJSON          string                      `gorm:"type:text" json:"-"`
	ShortFundingRuleJSON         string                      `gorm:"type:text" json:"-"`
	FundingSegmentsJSON          string                      `gorm:"type:text" json:"-"`
	EntryPathSegmentsJSON        string                      `gorm:"type:text" json:"-"`
	ProjectionDetails            []OpportunityProjection     `gorm:"-" json:"projection_details,omitempty"`
	LongFundingRule              OpportunityFundingRule      `gorm:"-" json:"long_funding_rule"`
	ShortFundingRule             OpportunityFundingRule      `gorm:"-" json:"short_funding_rule"`
	FundingSegments              []OpportunityFundingSegment `gorm:"-" json:"funding_segments,omitempty"`
	EntryPathSegments            []OpportunityFundingSegment `gorm:"-" json:"entry_path_segments,omitempty"`

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
	if raw, err := json.Marshal(o.FundingSegments); err == nil {
		o.FundingSegmentsJSON = string(raw)
	} else {
		return err
	}
	if raw, err := json.Marshal(o.EntryPathSegments); err == nil {
		o.EntryPathSegmentsJSON = string(raw)
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
	if o.FundingSegmentsJSON != "" {
		if err := json.Unmarshal([]byte(o.FundingSegmentsJSON), &o.FundingSegments); err != nil {
			return err
		}
	}
	if o.EntryPathSegmentsJSON != "" {
		if err := json.Unmarshal([]byte(o.EntryPathSegmentsJSON), &o.EntryPathSegments); err != nil {
			return err
		}
	}
	return nil
}
