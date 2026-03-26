package service

import (
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
	for _, rec := range activeRecords {
		if !countsTowardLivePlanLimit(rec) {
			continue
		}
		activeLivePlans++
		activeAllocatedNotional += rec.AllocatedNotionalUSDT
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
			"enabled":                            s.cfg.Enabled,
			"hold_hours":                         s.cfg.HoldHours,
			"effective_notional":                 effectiveNotional,
			"min_net_pnl":                        s.cfg.MinNetPNL,
			"entry_mode":                         s.cfg.EntryMode,
			"exit_mode":                          s.cfg.ExitMode,
			"max_data_age":                       s.cfg.MaxDataAge.String(),
			"max_spread_bps":                     s.cfg.MaxSpreadBps,
			"dynamic_max_spread_multiplier":      s.cfg.DynamicMaxSpreadMultiplier,
			"dynamic_max_spread_reference_hours": s.cfg.DynamicMaxSpreadReferenceHours,
			"entry_lead_time":                    s.cfg.EntryLeadTime.String(),
			"entry_cutoff_time":                  s.cfg.EntryCutoffTime.String(),
			"capital_total_usdt":                 s.cfg.TotalCapitalUSDT,
			"capital_utilization":                s.cfg.CapitalUtilization,
			"leverage":                           s.cfg.Leverage,
			"fees_by_exchange":                   feesByExchange,
			"funding_history_lookback":           s.cfg.FundingHistoryLookback.String(),
			"funding_smoothing_current_weight":   s.cfg.FundingSmoothingCurrentWeight,
			"dynamic_candidate_limit":            s.cfg.DynamicCandidateLimit,
			"rotation_batch_size":                s.cfg.RotationBatchSize,
			"rotation_interval":                  s.cfg.RotationInterval.String(),
			"deep_scan_hold_duration":            s.cfg.DeepScanHoldDuration.String(),
			"core_symbols":                       s.cfg.CoreSymbols,
		},
		"execution": map[string]any{
			"live_trading_enabled":           s.cfg.Execution.Enabled,
			"auto_entry":                     s.cfg.Execution.AutoEntry,
			"auto_close":                     s.cfg.Execution.AutoClose,
			"close_grace_period":             s.cfg.Execution.CloseGracePeriod.String(),
			"loop_interval":                  s.cfg.Execution.LoopInterval.String(),
			"max_latest_plans":               s.cfg.Execution.MaxLatestPlans,
			"auto_allocate_capital":          s.cfg.Execution.AutoAllocateCapital,
			"max_live_plans":                 s.cfg.Execution.MaxLivePlans,
			"max_auto_open_per_loop":         s.cfg.Execution.MaxAutoOpenPerLoop,
			"active_live_plans":              activeLivePlans,
			"active_allocated_notional_usdt": activeAllocatedNotional,
			"remaining_auto_budget_usdt":     remainingBudget,
			"remaining_live_slots":           remainingLiveSlots,
		},
	}
}
