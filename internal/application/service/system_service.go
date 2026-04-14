package service

import (
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"
)

type SystemService struct {
	store     *MarketStore
	cfg       Config
	exchanges exchange.ConfigSet
	execRepo  repository.ExecutionRepository
}

func NewSystemService(store *MarketStore, cfg Config, exchanges exchange.ConfigSet, execRepo repository.ExecutionRepository) *SystemService {
	return &SystemService{
		store:     store,
		cfg:       cfg.normalize(),
		exchanges: exchanges,
		execRepo:  execRepo,
	}
}

func (s *SystemService) Status() map[string]any {
	feesByExchange := map[string]any{}
	for name, cfg := range s.exchanges.Items() {
		feesByExchange[name] = map[string]any{
			"maker_bps": cfg.Fees.MakerBps,
			"taker_bps": cfg.Fees.TakerBps,
		}
	}

	var activeRecords []entity.ExecutionRecord
	if s.execRepo != nil {
		items, err := s.execRepo.ListActiveLive(nil)
		if err == nil {
			activeRecords = items
		}
	}
	activeLivePlans := 0
	activeAllocatedNotional := 0.0
	activeRollingRecords := 0
	activeRollingGroups := map[string]struct{}{}
	rollingDueReviews := 0
	rollingWaitingReviews := 0
	nowMs := time.Now().UTC().UnixMilli()
	for _, rec := range activeRecords {
		if !countsTowardLivePlanLimit(rec) {
			continue
		}
		activeLivePlans++
		activeAllocatedNotional += rec.AllocatedNotionalUSDT

		// rolling 统计只关心“当前真的会被 rolling monitor 继续观察的 live opened 仓位”。
		// pending_close / close_failed 这类尾部状态虽然仍占用 live slot，
		// 但它们已经不再参与 review / continue / flip 的策略判断。
		if !shouldMonitorRollingRecord(rec) {
			continue
		}
		activeRollingRecords++
		if rec.RollingGroupKey != "" {
			activeRollingGroups[rec.RollingGroupKey] = struct{}{}
		}
		switch {
		case rec.NextReviewTimeMs > 0 && nowMs >= rec.NextReviewTimeMs:
			rollingDueReviews++
		case rec.NextReviewTimeMs > 0:
			rollingWaitingReviews++
		}
	}
	effectiveNotional := s.cfg.EffectiveNotional()
	remainingBudget := effectiveNotional - activeAllocatedNotional
	if remainingBudget < 0 {
		remainingBudget = 0
	}
	remainingLiveSlots := -1
	if s.cfg.Execution.MaxLivePlans > 0 {
		remainingLiveSlots = s.cfg.Execution.MaxLivePlans - activeLivePlans
		if remainingLiveSlots < 0 {
			remainingLiveSlots = 0
		}
	}

	return map[string]any{
		// watchlist 是“全市场基础池”，deep_scan_watchlist 才是当前真的做盘口深扫的那一批 symbol。
		"watchlist":           s.store.Watchlist(),
		"deep_scan_watchlist": s.store.DeepScanWatchlist(),
		"connectors":          s.store.Statuses(),
		"strategy": map[string]any{
			"enabled":                                         s.cfg.Enabled,
			"mode":                                            s.cfg.StrategyMode,
			"arbitrage_mode":                                  s.cfg.ArbitrageMode,
			"hold_hours":                                      s.cfg.HoldHours,
			"hold_selection_mode":                             s.cfg.HoldSelectionMode,
			"effective_notional":                              effectiveNotional,
			"min_net_pnl":                                     s.cfg.MinNetPNL,
			"entry_mode":                                      s.cfg.EntryMode,
			"exit_mode":                                       s.cfg.ExitMode,
			"max_data_age":                                    s.cfg.MaxDataAge.String(),
			"max_spread_bps":                                  s.cfg.MaxSpreadBps,
			"dynamic_max_spread_multiplier":                   s.cfg.DynamicMaxSpreadMultiplier,
			"dynamic_max_spread_reference_hours":              s.cfg.DynamicMaxSpreadReferenceHours,
			"entry_lead_time":                                 s.cfg.EntryLeadTime.String(),
			"entry_cutoff_time":                               s.cfg.EntryCutoffTime.String(),
			"capital_total_usdt":                              s.cfg.TotalCapitalUSDT,
			"capital_utilization":                             s.cfg.CapitalUtilization,
			"leverage":                                        s.cfg.Leverage,
			"fees_by_exchange":                                feesByExchange,
			"funding_history_lookback":                        s.cfg.FundingHistoryLookback.String(),
			"funding_rate_history_lookback":                   s.cfg.FundingRateHistoryLookback.String(),
			"funding_rate_history_sync_interval":              s.cfg.FundingRateHistorySyncInterval.String(),
			"funding_smoothing_current_weight":                s.cfg.FundingSmoothingCurrentWeight,
			"dynamic_candidate_limit":                         s.cfg.DynamicCandidateLimit,
			"rotation_batch_size":                             s.cfg.RotationBatchSize,
			"rotation_interval":                               s.cfg.RotationInterval.String(),
			"deep_scan_hold_duration":                         s.cfg.DeepScanHoldDuration.String(),
			"same_exchange_require_long_hold_eligible":        s.cfg.SameExchangeRequireLongHoldEligible,
			"same_exchange_min_history_sample_count":          s.cfg.SameExchangeMinHistorySampleCount,
			"same_exchange_min_historical_support_ratio":      s.cfg.SameExchangeMinHistoricalSupportRatio,
			"same_exchange_min_annualized_net_rate":           s.cfg.SameExchangeMinAnnualizedNetRate,
			"same_exchange_basis_long_hold_window_hours":      s.cfg.SameExchangeBasisLongHoldWindowHours,
			"same_exchange_max_basis_payback_events":          s.cfg.SameExchangeMaxBasisPaybackEvents,
			"same_exchange_close_on_negative_funding":         s.cfg.SameExchangeCloseOnNegativeFunding,
			"same_exchange_history_negative_exit_threshold":   s.cfg.SameExchangeHistoryNegativeExitThreshold,
			"same_exchange_exit_require_positive_close_pnl":   s.cfg.SameExchangeExitRequirePositiveClosePNL,
			"same_exchange_exit_min_close_pnl":                s.cfg.SameExchangeExitMinClosePNL,
			"same_exchange_max_perp_leverage":                 s.cfg.SameExchangeMaxPerpLeverage,
			"same_exchange_min_liq_distance_ratio":            s.cfg.SameExchangeMinLiqDistanceRatio,
			"same_exchange_warn_liq_distance_ratio":           s.cfg.SameExchangeWarnLiqDistanceRatio,
			"same_exchange_reduce_liq_distance_ratio":         s.cfg.SameExchangeReduceLiqDistanceRatio,
			"same_exchange_emergency_liq_distance_ratio":      s.cfg.SameExchangeEmergencyLiqDistanceRatio,
			"same_exchange_max_1h_price_shock_ratio":          s.cfg.SameExchangeMax1hPriceShockRatio,
			"same_exchange_funding_extreme_percentile":        s.cfg.SameExchangeFundingExtremePercentile,
			"same_exchange_extreme_funding_negative_ratio":    s.cfg.SameExchangeExtremeFundingNegativeRatio,
			"same_exchange_extreme_funding_size_multiplier":   s.cfg.SameExchangeExtremeFundingSizeMultiplier,
			"same_exchange_extreme_basis_payback_events":      s.cfg.SameExchangeExtremeBasisPaybackEvents,
			"same_exchange_extreme_basis_size_multiplier":     s.cfg.SameExchangeExtremeBasisSizeMultiplier,
			"core_symbols":                                    s.cfg.CoreSymbols,
			"rolling_review_settle_grace_period":              s.cfg.RollingReviewSettleGracePeriod.String(),
			"rolling_review_fresh_snapshot_max_wait":          s.cfg.RollingReviewFreshSnapshotMaxWait.String(),
			"rolling_review_close_on_snapshot_timeout":        s.cfg.RollingReviewCloseOnSnapshotTimeout,
			"rolling_review_continue_on_same_direction":       s.cfg.RollingReviewContinueOnSameDirection,
			"rolling_review_close_on_unprofitable":            s.cfg.RollingReviewCloseOnUnprofitable,
			"rolling_review_require_incremental_net_positive": s.cfg.RollingReviewRequireIncrementalNetPositive,
			"rolling_review_min_incremental_net_pnl":          s.cfg.RollingReviewMinIncrementalNetPNL,
			"rolling_flip_enabled":                            s.cfg.RollingFlipEnabled,
			"rolling_flip_require_net_positive":               s.cfg.RollingFlipRequireNetPositive,
			"rolling_flip_min_net_pnl":                        s.cfg.RollingFlipMinNetPNL,
			"rolling_flip_slippage_multiplier":                s.cfg.RollingFlipSlippageMultiplier,
			"rolling_flip_extra_safety_buffer_usdt":           s.cfg.RollingFlipExtraSafetyBufferUSDT,
		},
		"execution": map[string]any{
			"live_trading_enabled": s.cfg.Execution.Enabled,
			"auto_entry":           s.cfg.Execution.AutoEntry,
			"auto_close":           s.cfg.Execution.AutoClose,
			"position_monitor_force_close_on_single_leg":    s.cfg.ExecutionPositionMonitorForceCloseOnSingleLeg,
			"position_monitor_force_close_on_side_mismatch": s.cfg.ExecutionPositionMonitorForceCloseOnSideMismatch,
			"position_monitor_force_close_on_size_mismatch": s.cfg.ExecutionPositionMonitorForceCloseOnSizeMismatch,
			"position_monitor_max_qty_deviation_ratio":      s.cfg.ExecutionPositionMonitorMaxQtyDeviationRatio,
			"active_replacement_enabled":                    s.cfg.ExecutionReplacementEnabled,
			"active_replacement_only_when_constrained":      s.cfg.ExecutionReplacementOnlyWhenConstrained,
			"active_replacement_require_net_improvement":    s.cfg.ExecutionReplacementRequireNetImprovement,
			"active_replacement_min_net_improvement_pnl":    s.cfg.ExecutionReplacementMinNetImprovementPNL,
			"active_replacement_extra_safety_buffer_usdt":   s.cfg.ExecutionReplacementExtraSafetyBufferUSDT,
			"close_grace_period":                            s.cfg.Execution.CloseGracePeriod.String(),
			"loop_interval":                                 s.cfg.Execution.LoopInterval.String(),
			"max_latest_plans":                              s.cfg.Execution.MaxLatestPlans,
			"auto_allocate_capital":                         s.cfg.Execution.AutoAllocateCapital,
			"max_live_plans":                                s.cfg.Execution.MaxLivePlans,
			"max_auto_open_per_loop":                        s.cfg.Execution.MaxAutoOpenPerLoop,
			"active_live_plans":                             activeLivePlans,
			"active_allocated_notional_usdt":                activeAllocatedNotional,
			"remaining_auto_budget_usdt":                    remainingBudget,
			"remaining_live_slots":                          remainingLiveSlots,
			"active_rolling_records":                        activeRollingRecords,
			"active_rolling_groups":                         len(activeRollingGroups),
			"rolling_due_reviews":                           rollingDueReviews,
			"rolling_waiting_reviews":                       rollingWaitingReviews,
		},
	}
}
