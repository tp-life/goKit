package service

type SystemService struct {
	store *MarketStore
	cfg   Config
}

func NewSystemService(store *MarketStore, cfg Config) *SystemService {
	return &SystemService{store: store, cfg: cfg.normalize()}
}

func (s *SystemService) Status() map[string]any {
	return map[string]any{
		// watchlist 是“全市场基础池”，deep_scan_watchlist 才是当前真的做盘口深扫的那一批 symbol。
		"watchlist":           s.store.Watchlist(),
		"deep_scan_watchlist": s.store.DeepScanWatchlist(),
		"connectors":          s.store.Statuses(),
		"strategy": map[string]any{
			"enabled":                 s.cfg.Enabled,
			"hold_hours":              s.cfg.HoldHours,
			"effective_notional":      s.cfg.EffectiveNotional(),
			"min_net_pnl":             s.cfg.MinNetPNL,
			"entry_mode":              s.cfg.EntryMode,
			"exit_mode":               s.cfg.ExitMode,
			"max_data_age":            s.cfg.MaxDataAge.String(),
			"max_spread_bps":          s.cfg.MaxSpreadBps,
			"entry_lead_time":         s.cfg.EntryLeadTime.String(),
			"entry_cutoff_time":       s.cfg.EntryCutoffTime.String(),
			"capital_total_usdt":      s.cfg.TotalCapitalUSDT,
			"capital_utilization":     s.cfg.CapitalUtilization,
			"leverage":                s.cfg.Leverage,
			"dynamic_candidate_limit": s.cfg.DynamicCandidateLimit,
			"rotation_batch_size":     s.cfg.RotationBatchSize,
			"rotation_interval":       s.cfg.RotationInterval.String(),
			"deep_scan_hold_duration": s.cfg.DeepScanHoldDuration.String(),
			"core_symbols":            s.cfg.CoreSymbols,
		},
		"execution": map[string]any{
			"live_trading_enabled": s.cfg.Execution.Enabled,
			"auto_entry":           s.cfg.Execution.AutoEntry,
			"auto_close":           s.cfg.Execution.AutoClose,
			"close_grace_period":   s.cfg.Execution.CloseGracePeriod.String(),
			"loop_interval":        s.cfg.Execution.LoopInterval.String(),
			"max_latest_plans":     s.cfg.Execution.MaxLatestPlans,
		},
	}
}
