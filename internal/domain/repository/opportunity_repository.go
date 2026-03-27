package repository

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
)

type OpportunitySummary struct {
	ID uint `json:"id"`

	BatchID    string `json:"batch_id"`
	AsOfTimeMs int64  `json:"as_of_time_ms"`

	Symbol           string `json:"symbol"`
	LongExchange     string `json:"long_exchange"`
	ShortExchange    string `json:"short_exchange"`
	LongVenueSymbol  string `json:"long_venue_symbol"`
	ShortVenueSymbol string `json:"short_venue_symbol"`

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
	FundingEstimateMode       string  `json:"funding_estimate_mode"`
	FundingEstimateConfidence string  `json:"funding_estimate_confidence"`

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
	ExecutionPenaltyModel  string  `json:"execution_penalty_model"`
	ExecutionPenaltyBucket string  `json:"execution_penalty_bucket"`
	NetExpectedPNL         float64 `json:"net_expected_pnl"`
	NetExpectedBps         float64 `json:"net_expected_bps"`
	BasisBps               float64 `json:"basis_bps"`
	MaxAllowedBasisBps     float64 `json:"max_allowed_basis_bps"`

	Score float64 `json:"score"`

	EarliestFundingTimeMs        int64   `json:"earliest_funding_time_ms"`
	LatestFundingTimeMs          int64   `json:"latest_funding_time_ms"`
	ProjectedFundingTimeMs       int64   `json:"projected_funding_time_ms"`
	RequiredEntryByFundingTimeMs int64   `json:"required_entry_by_funding_time_ms"`
	LongFundingEventCount        int     `json:"long_funding_event_count"`
	ShortFundingEventCount       int     `json:"short_funding_event_count"`
	FundingWindowHours           float64 `json:"funding_window_hours"`
	StrategyMode                 string  `json:"strategy_mode"`
	FundingComputationMode       string  `json:"funding_computation_mode"`
	NextReviewTimeMs             int64   `json:"next_review_time_ms"`
	SyncBoundaryTimeMs           int64   `json:"sync_boundary_time_ms"`
	EntryPathSegmentCount        int     `json:"entry_path_segment_count"`
	EntryPathStopReason          string  `json:"entry_path_stop_reason"`

	Status               string    `json:"status"`
	RejectReason         string    `json:"reject_reason"`
	EligibleForExecution bool      `json:"eligible_for_execution"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type OpportunityRepository interface {
	SaveBatch(ctx context.Context, batchID string, items []entity.Opportunity) error
	ListLatest(ctx context.Context, limit int) ([]entity.Opportunity, error)
	ListLatestSummary(ctx context.Context, limit int) ([]OpportunitySummary, error)
	FindByID(ctx context.Context, id uint) (*entity.Opportunity, error)
	DeleteOlderThan(ctx context.Context, cutoffMs int64, limit int) (int64, error)
}
