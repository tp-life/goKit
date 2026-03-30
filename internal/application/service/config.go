package service

import (
	"strings"
	"time"
)

const (
	// StrategyModeLegacyProjection 保留当前仓库里的“累计候选窗口 + 选主窗口”模式。
	StrategyModeLegacyProjection = "legacy_projection"
	// StrategyModeRollingCycleAligned 表示后续将切到“按结算段滚动 review / 续持 / 翻仓”的模式。
	StrategyModeRollingCycleAligned = "rolling_cycle_aligned"

	// HoldSelectionModeBestNet 表示：
	// 在 hold_hours 允许的候选结算窗口内，直接选择“净收益最高”的那一个窗口。
	//
	// 这是一个更通用的模式：
	// - 它允许主计划在 1h / 2h / 4h / 8h 等多个候选窗口之间自由跳转；
	// - 更适合“收益最大化优先”的场景；
	// - 但它不保证最终选中的窗口一定尽量贴近 hold_hours。
	HoldSelectionModeBestNet = "best_net"
	// HoldSelectionModeLatestProfitable 表示：
	// 在 hold_hours 内优先选择“最晚且仍然满足最小净收益门槛”的窗口。
	//
	// 这更贴近 funding 套利的直觉：
	// - 先给仓位足够长的时间去覆盖开平仓成本；
	// - 只要更长窗口依然赚钱，就尽量多吃几轮 funding；
	// - 只有当更长窗口已经不再划算时，才退回到更早的可盈利窗口。
	HoldSelectionModeLatestProfitable = "latest_profitable"
	// HoldSelectionModeStrictTarget 表示：
	// 直接把“hold_hours 内最后一个真实 funding 兑现窗口”视为目标窗口。
	//
	// 这是一种最严格的套利对齐模式：
	// - 收益展示按目标窗口算；
	// - execution plan 也按这个窗口生成；
	// - 自动平仓同样按这个窗口的 target_close_time 走。
	//
	// 如果该目标窗口本身不赚钱，系统不会偷偷退回到更早窗口，
	// 而是把这条机会明确展示为“不盈利/不可执行”。
	HoldSelectionModeStrictTarget = "strict_target"
)

type StrategyUniverseConfig struct {
	AllowedSymbols []string `mapstructure:"allowed_symbols"`
	CoreSymbols    []string `mapstructure:"core_symbols"`
	QuoteAsset     string   `mapstructure:"quote_asset"`
}

type StrategyCapitalConfig struct {
	TotalCapitalUSDT   float64 `mapstructure:"total_capital_usdt"`
	CapitalUtilization float64 `mapstructure:"capital_utilization"`
	Leverage           float64 `mapstructure:"leverage"`
	AssumedNotional    float64 `mapstructure:"assumed_notional"`
}

type StrategyOpportunityConfig struct {
	HoldHours                 float64 `mapstructure:"hold_hours"`
	LegacyHoldSelectionMode   string  `mapstructure:"legacy_hold_selection_mode"`
	MinNetPNL                 float64 `mapstructure:"min_net_pnl"`
	MaxDisplayedOpportunities int     `mapstructure:"max_displayed_opportunities"`
}

type StrategyExecutionCostConfig struct {
	SlippageBps      float64 `mapstructure:"slippage_bps"`
	SafetyBufferUSDT float64 `mapstructure:"safety_buffer_usdt"`
	EntryMode        string  `mapstructure:"entry_mode"`
	ExitMode         string  `mapstructure:"exit_mode"`
}

type StrategyMarketDataConfig struct {
	MaxDataAge                     time.Duration `mapstructure:"max_data_age"`
	FundingSnapshotPersistInterval time.Duration `mapstructure:"funding_snapshot_persist_interval"`
	BookSnapshotPersistInterval    time.Duration `mapstructure:"book_snapshot_persist_interval"`
	SnapshotRetention              time.Duration `mapstructure:"snapshot_retention"`
	OpportunityCalcInterval        time.Duration `mapstructure:"opportunity_calc_interval"`
	BookSnapshotMinPriceChangeBps  float64       `mapstructure:"book_snapshot_min_price_change_bps"`
	BookSnapshotMinQtyChangeRatio  float64       `mapstructure:"book_snapshot_min_qty_change_ratio"`
}

type StrategySpreadGuardConfig struct {
	MaxSpreadBps                   float64       `mapstructure:"max_spread_bps"`
	DynamicMaxSpreadMultiplier     float64       `mapstructure:"dynamic_max_spread_multiplier"`
	DynamicMaxSpreadReferenceHours float64       `mapstructure:"dynamic_max_spread_reference_hours"`
	EntryLeadTime                  time.Duration `mapstructure:"entry_lead_time"`
	EntryCutoffTime                time.Duration `mapstructure:"entry_cutoff_time"`
}

type StrategyPredictionConfig struct {
	FundingHistoryLookback                  time.Duration `mapstructure:"funding_history_lookback"`
	FundingSmoothingCurrentWeight           float64       `mapstructure:"funding_smoothing_current_weight"`
	FundingRateContinuationDecay            float64       `mapstructure:"funding_rate_continuation_decay"`
	AllowIntermediateForecastBeforeBoundary *bool         `mapstructure:"allow_intermediate_forecast_before_boundary"`
	ForbidBoundaryForecast                  *bool         `mapstructure:"forbid_boundary_forecast"`
}

type StrategyScanConfig struct {
	DynamicCandidateLimit int           `mapstructure:"dynamic_candidate_limit"`
	RotationBatchSize     int           `mapstructure:"rotation_batch_size"`
	RotationInterval      time.Duration `mapstructure:"rotation_interval"`
	DeepScanHoldDuration  time.Duration `mapstructure:"deep_scan_hold_duration"`
}

type StrategyRollingReviewConfig struct {
	SettleGracePeriod             time.Duration `mapstructure:"settle_grace_period"`
	FreshSnapshotMaxWait          time.Duration `mapstructure:"fresh_snapshot_max_wait"`
	CloseOnSnapshotTimeout        *bool         `mapstructure:"close_on_snapshot_timeout"`
	ContinueOnSameDirection       *bool         `mapstructure:"continue_on_same_direction"`
	CloseOnUnprofitable           *bool         `mapstructure:"close_on_unprofitable"`
	RequireIncrementalNetPositive *bool         `mapstructure:"require_incremental_net_positive"`
	MinIncrementalNetPNL          float64       `mapstructure:"min_incremental_net_pnl"`
}

type StrategyRollingFlipConfig struct {
	Enabled               *bool   `mapstructure:"enabled"`
	RequireNetPositive    *bool   `mapstructure:"require_net_positive"`
	MinNetPNL             float64 `mapstructure:"min_net_pnl"`
	SlippageMultiplier    float64 `mapstructure:"slippage_multiplier"`
	ExtraSafetyBufferUSDT float64 `mapstructure:"extra_safety_buffer_usdt"`
}

type StrategyRollingConfig struct {
	EntryPathRequireConsistentDirection     *bool                       `mapstructure:"entry_path_require_consistent_direction"`
	MaxSingleExchangeForecastSegments       int                         `mapstructure:"max_single_exchange_forecast_segments"`
	AllowIntermediateForecastBeforeBoundary *bool                       `mapstructure:"allow_intermediate_forecast_before_boundary"`
	ForbidBoundaryForecast                  *bool                       `mapstructure:"forbid_boundary_forecast"`
	Review                                  StrategyRollingReviewConfig `mapstructure:"review"`
	Flip                                    StrategyRollingFlipConfig   `mapstructure:"flip"`
}

type ExecutionConfig struct {
	Enabled   bool `mapstructure:"enabled"`
	AutoEntry bool `mapstructure:"auto_entry"`
	AutoClose bool `mapstructure:"auto_close"`
	// CloseGracePeriod 是“计划性 funding 平仓”的结算后缓冲。
	//
	// 它的作用不是“额外再持有很久”，而是：
	// 1. 给 funding 入账/账务落地一个很短的缓冲；
	// 2. 避免刚踩到结算点就立刻发平仓，降低边界抖动。
	//
	// 对 funding 套利来说，这个值通常应该比较短；
	// 若配得太长，会把原本不属于收益模型的额外市场暴露带进来。
	CloseGracePeriod                  time.Duration `mapstructure:"close_grace_period"`
	LoopInterval                      time.Duration `mapstructure:"loop_interval"`
	MaxLatestPlans                    int           `mapstructure:"max_latest_plans"`
	AutoAllocateCapital               bool          `mapstructure:"auto_allocate_capital"`
	MaxLivePlans                      int           `mapstructure:"max_live_plans"`
	MaxAutoOpenPerLoop                int           `mapstructure:"max_auto_open_per_loop"`
	MaxSingleSymbolExposureUSDT       float64       `mapstructure:"max_single_symbol_exposure_usdt"`
	MaxSingleExchangeExposureUSDT     float64       `mapstructure:"max_single_exchange_exposure_usdt"`
	MinAccountEquityUSDT              float64       `mapstructure:"min_account_equity_usdt"`
	MinAvailableBalanceRatio          float64       `mapstructure:"min_available_balance_ratio"`
	MaxUnrealizedLossUSDT             float64       `mapstructure:"max_unrealized_loss_usdt"`
	MaxUnwindBasisBps                 float64       `mapstructure:"max_unwind_basis_bps"`
	EmergencyMinAvailableBalanceRatio float64       `mapstructure:"emergency_min_available_balance_ratio"`
	PrimaryLegTimeout                 time.Duration `mapstructure:"primary_leg_timeout"`
	APIFailureThreshold               int           `mapstructure:"api_failure_threshold"`
	APIFailureCooldown                time.Duration `mapstructure:"api_failure_cooldown"`
	OrderStatusPollAttempts           int           `mapstructure:"order_status_poll_attempts"`
	OrderStatusPollInterval           time.Duration `mapstructure:"order_status_poll_interval"`
}

type Config struct {
	Enabled bool `mapstructure:"enabled"`
	// StrategyMode 决定策略主链使用哪种收益 / 持仓模型。
	//
	// 当前代码仍主要运行 legacy_projection；
	// rolling_cycle_aligned 先作为配置与设计层的正式入口，后续策略实现会接管它。
	StrategyMode  string                      `mapstructure:"mode"`
	Universe      StrategyUniverseConfig      `mapstructure:"universe"`
	Capital       StrategyCapitalConfig       `mapstructure:"capital"`
	Opportunity   StrategyOpportunityConfig   `mapstructure:"opportunity"`
	ExecutionCost StrategyExecutionCostConfig `mapstructure:"execution_cost"`
	MarketData    StrategyMarketDataConfig    `mapstructure:"market_data"`
	SpreadGuard   StrategySpreadGuardConfig   `mapstructure:"spread_guard"`
	Prediction    StrategyPredictionConfig    `mapstructure:"prediction"`
	Scan          StrategyScanConfig          `mapstructure:"scan"`
	Rolling       StrategyRollingConfig       `mapstructure:"rolling"`
	// AllowedSymbols 是“硬白名单”。为空时表示先不过滤，允许所有 canonical symbol 进入基础池。
	AllowedSymbols []string `mapstructure:"allowed_symbols"`
	// CoreSymbols 是常驻深扫的 symbol。它们会长期保留在盘口订阅列表里，
	// 适合作为 BTC / ETH / SOL 这类“不该错过”的核心交易对。
	CoreSymbols []string `mapstructure:"core_symbols"`
	QuoteAsset  string   `mapstructure:"quote_asset"`
	// HoldHours 不是“必须死等这么久才允许平仓”，而是“策略允许往后看多远”。
	//
	// 最终真正选中的持有周期，还要结合 HoldSelectionMode 来决定：
	// - best_net: 在这段时间里选净收益最高的窗口；
	// - latest_profitable: 在这段时间里尽量选更晚、但仍然过收益门槛的窗口。
	// - strict_target: 直接锚定 hold_hours 内最后一个真实结算窗口，不再回退。
	HoldHours float64 `mapstructure:"hold_hours"`
	// HoldSelectionMode 控制“在 hold_hours 对应的候选 funding 窗口里，主计划到底选哪一个”。
	//
	// 这个字段直接影响：
	// 1. 页面上展示的主收益与主持有时长；
	// 2. execution plan 里的 ProjectedFundingTimeMs；
	// 3. 计划性自动平仓的 TargetCloseTimeMs。
	//
	// 也就是说，收益怎么估，就应该按同一个窗口去持有。
	HoldSelectionMode              string        `mapstructure:"hold_selection_mode"`
	AssumedNotional                float64       `mapstructure:"assumed_notional"`
	TotalCapitalUSDT               float64       `mapstructure:"total_capital_usdt"`
	CapitalUtilization             float64       `mapstructure:"capital_utilization"`
	Leverage                       float64       `mapstructure:"leverage"`
	MinNetPNL                      float64       `mapstructure:"min_net_pnl"`
	MaxDisplayedOpportunities      int           `mapstructure:"max_displayed_opportunities"`
	SlippageBps                    float64       `mapstructure:"slippage_bps"`
	SafetyBufferUSDT               float64       `mapstructure:"safety_buffer_usdt"`
	EntryMode                      string        `mapstructure:"entry_mode"`
	ExitMode                       string        `mapstructure:"exit_mode"`
	MaxDataAge                     time.Duration `mapstructure:"max_data_age"`
	MaxSpreadBps                   float64       `mapstructure:"max_spread_bps"`
	DynamicMaxSpreadMultiplier     float64       `mapstructure:"dynamic_max_spread_multiplier"`
	DynamicMaxSpreadReferenceHours float64       `mapstructure:"dynamic_max_spread_reference_hours"`
	EntryLeadTime                  time.Duration `mapstructure:"entry_lead_time"`
	EntryCutoffTime                time.Duration `mapstructure:"entry_cutoff_time"`
	SnapshotPersistInterval        time.Duration `mapstructure:"snapshot_persist_interval"`
	FundingSnapshotPersistInterval time.Duration `mapstructure:"funding_snapshot_persist_interval"`
	BookSnapshotPersistInterval    time.Duration `mapstructure:"book_snapshot_persist_interval"`
	SnapshotRetention              time.Duration `mapstructure:"snapshot_retention"`
	BookSnapshotMinPriceChangeBps  float64       `mapstructure:"book_snapshot_min_price_change_bps"`
	BookSnapshotMinQtyChangeRatio  float64       `mapstructure:"book_snapshot_min_qty_change_ratio"`
	OpportunityCalcInterval        time.Duration `mapstructure:"opportunity_calc_interval"`
	FundingHistoryLookback         time.Duration `mapstructure:"funding_history_lookback"`
	FundingSmoothingCurrentWeight  float64       `mapstructure:"funding_smoothing_current_weight"`
	// FundingRateContinuationDecay 控制“在同一持仓窗口内，对同一腿未来第2次及以后 funding 事件”的费率衰减。
	//
	// 注意：
	// - legacy_projection 仍会消费这个字段；
	// - rolling_cycle_aligned 现在已经切成 real-only 机会识别，不再把未来预测段纳入 headline carry。
	//
	// 取值建议：
	//   = 1.0 : 不衰减（线性外推，激进）
	//   (0,1): 几何衰减（更保守，降低对当前极值费率的过拟合）
	//   <=0 或 >1: 使用默认值
	FundingRateContinuationDecay float64 `mapstructure:"funding_rate_continuation_decay"`
	// DynamicCandidateLimit 控制“动态候选深扫池”的大小。
	//
	// 取值约定：
	//   > 0 : 只保留 Top N funding 候选进入深扫池。
	//   = 0 : 不设上限，所有 funding 候选都可以进入深扫池。
	//   < 0 : 使用系统默认值。
	//
	// 之所以把 0 定义成“全量”，是为了支持这样一种部署方式：
	// 如果交易所可以提供全市场实时 bookTicker / bbo，或者机器资源足够，
	// 就可以在不写白名单的情况下直接放大扫描范围，尽量减少漏机会。
	DynamicCandidateLimit int `mapstructure:"dynamic_candidate_limit"`
	// RotationBatchSize 控制“冷门币轮转补扫”的大小。
	// 这部分 symbol 不会永久深扫，而是按批次轮流进入盘口订阅，降低漏机会概率。
	RotationBatchSize int `mapstructure:"rotation_batch_size"`
	// RotationInterval 控制候选池与轮转池多久重算一次。
	RotationInterval time.Duration `mapstructure:"rotation_interval"`
	// DeepScanHoldDuration 用于降低候选池抖动。
	// 某个 symbol 一旦进入深扫池，至少保留这么久，避免 websocket 频繁订阅/退订。
	DeepScanHoldDuration time.Duration   `mapstructure:"deep_scan_hold_duration"`
	Execution            ExecutionConfig `mapstructure:"execution"`

	// 下面这些字段是 rolling 设计的运行时归一化结果。
	//
	// 其中与 forecast 相关的开关目前主要保留兼容语义：
	// rolling 结构仍保留 review / continue / flip，但机会识别已经切成 real-only，
	// 不再把 boundary 之前的预测段直接算进当前开仓收益。
	RollingEntryPathRequireConsistentDirection     bool          `mapstructure:"-"`
	RollingMaxSingleExchangeForecastSegments       int           `mapstructure:"-"`
	RollingAllowIntermediateForecastBeforeBoundary bool          `mapstructure:"-"`
	RollingForbidBoundaryForecast                  bool          `mapstructure:"-"`
	RollingReviewSettleGracePeriod                 time.Duration `mapstructure:"-"`
	RollingReviewFreshSnapshotMaxWait              time.Duration `mapstructure:"-"`
	RollingReviewCloseOnSnapshotTimeout            bool          `mapstructure:"-"`
	RollingReviewContinueOnSameDirection           bool          `mapstructure:"-"`
	RollingReviewCloseOnUnprofitable               bool          `mapstructure:"-"`
	RollingReviewRequireIncrementalNetPositive     bool          `mapstructure:"-"`
	RollingReviewMinIncrementalNetPNL              float64       `mapstructure:"-"`
	RollingFlipEnabled                             bool          `mapstructure:"-"`
	RollingFlipRequireNetPositive                  bool          `mapstructure:"-"`
	RollingFlipMinNetPNL                           float64       `mapstructure:"-"`
	RollingFlipSlippageMultiplier                  float64       `mapstructure:"-"`
	RollingFlipExtraSafetyBufferUSDT               float64       `mapstructure:"-"`
}

func (c Config) normalize() Config {
	c.StrategyMode = normalizeStrategyMode(c.StrategyMode, StrategyModeLegacyProjection)
	if len(c.AllowedSymbols) == 0 && len(c.Universe.AllowedSymbols) > 0 {
		c.AllowedSymbols = append([]string(nil), c.Universe.AllowedSymbols...)
	}
	if len(c.CoreSymbols) == 0 && len(c.Universe.CoreSymbols) > 0 {
		c.CoreSymbols = append([]string(nil), c.Universe.CoreSymbols...)
	}
	if strings.TrimSpace(c.QuoteAsset) == "" {
		c.QuoteAsset = c.Universe.QuoteAsset
	}
	if c.QuoteAsset == "" {
		c.QuoteAsset = "USDT"
	}
	if c.HoldHours <= 0 {
		c.HoldHours = c.Opportunity.HoldHours
	}
	if c.HoldHours <= 0 {
		c.HoldHours = 24
	}
	if strings.TrimSpace(c.HoldSelectionMode) == "" {
		c.HoldSelectionMode = c.Opportunity.LegacyHoldSelectionMode
	}
	if strings.TrimSpace(c.HoldSelectionMode) == "" && c.StrategyMode == StrategyModeRollingCycleAligned {
		// rolling 模式在当前兼容层先回落到 latest_profitable，
		// 这样现有策略主链还能得到一条相对接近“尽量持有到更晚 review 点”的旧行为。
		c.HoldSelectionMode = HoldSelectionModeLatestProfitable
	}
	c.HoldSelectionMode = normalizeHoldSelectionMode(c.HoldSelectionMode, HoldSelectionModeBestNet)
	if c.TotalCapitalUSDT <= 0 {
		c.TotalCapitalUSDT = c.Capital.TotalCapitalUSDT
	}
	if c.TotalCapitalUSDT <= 0 {
		c.TotalCapitalUSDT = 1000
	}
	if c.CapitalUtilization <= 0 || c.CapitalUtilization > 1 {
		c.CapitalUtilization = c.Capital.CapitalUtilization
	}
	if c.CapitalUtilization <= 0 || c.CapitalUtilization > 1 {
		c.CapitalUtilization = 0.8
	}
	if c.Leverage <= 0 {
		c.Leverage = c.Capital.Leverage
	}
	if c.Leverage <= 0 {
		c.Leverage = 2
	}
	if c.AssumedNotional <= 0 {
		c.AssumedNotional = c.Capital.AssumedNotional
	}
	if c.AssumedNotional <= 0 {
		c.AssumedNotional = c.EffectiveNotional()
	}
	if c.MinNetPNL <= 0 {
		c.MinNetPNL = c.Opportunity.MinNetPNL
	}
	if c.MinNetPNL <= 0 {
		c.MinNetPNL = 1.5
	}
	if c.MaxDisplayedOpportunities <= 0 {
		c.MaxDisplayedOpportunities = c.Opportunity.MaxDisplayedOpportunities
	}
	if c.MaxDisplayedOpportunities <= 0 {
		c.MaxDisplayedOpportunities = 20
	}
	if c.SlippageBps == 0 && c.ExecutionCost.SlippageBps != 0 {
		c.SlippageBps = c.ExecutionCost.SlippageBps
	}
	if c.SafetyBufferUSDT <= 0 {
		c.SafetyBufferUSDT = c.ExecutionCost.SafetyBufferUSDT
	}
	if strings.TrimSpace(c.EntryMode) == "" {
		c.EntryMode = c.ExecutionCost.EntryMode
	}
	if strings.TrimSpace(c.ExitMode) == "" {
		c.ExitMode = c.ExecutionCost.ExitMode
	}
	if c.MaxDataAge <= 0 {
		c.MaxDataAge = c.MarketData.MaxDataAge
	}
	if c.FundingSnapshotPersistInterval <= 0 {
		c.FundingSnapshotPersistInterval = c.MarketData.FundingSnapshotPersistInterval
	}
	if c.BookSnapshotPersistInterval <= 0 {
		c.BookSnapshotPersistInterval = c.MarketData.BookSnapshotPersistInterval
	}
	if c.SnapshotRetention <= 0 {
		c.SnapshotRetention = c.MarketData.SnapshotRetention
	}
	if c.OpportunityCalcInterval <= 0 {
		c.OpportunityCalcInterval = c.MarketData.OpportunityCalcInterval
	}
	if c.BookSnapshotMinPriceChangeBps <= 0 {
		c.BookSnapshotMinPriceChangeBps = c.MarketData.BookSnapshotMinPriceChangeBps
	}
	if c.BookSnapshotMinQtyChangeRatio <= 0 {
		c.BookSnapshotMinQtyChangeRatio = c.MarketData.BookSnapshotMinQtyChangeRatio
	}
	if c.MaxSpreadBps <= 0 {
		c.MaxSpreadBps = c.SpreadGuard.MaxSpreadBps
	}
	if c.DynamicMaxSpreadMultiplier < 1 {
		c.DynamicMaxSpreadMultiplier = c.SpreadGuard.DynamicMaxSpreadMultiplier
	}
	if c.DynamicMaxSpreadReferenceHours <= 0 {
		c.DynamicMaxSpreadReferenceHours = c.SpreadGuard.DynamicMaxSpreadReferenceHours
	}
	if c.EntryLeadTime <= 0 {
		c.EntryLeadTime = c.SpreadGuard.EntryLeadTime
	}
	if c.EntryCutoffTime <= 0 {
		c.EntryCutoffTime = c.SpreadGuard.EntryCutoffTime
	}
	if c.FundingHistoryLookback <= 0 {
		c.FundingHistoryLookback = c.Prediction.FundingHistoryLookback
	}
	if c.FundingSmoothingCurrentWeight < 0 || c.FundingSmoothingCurrentWeight > 1 {
		c.FundingSmoothingCurrentWeight = c.Prediction.FundingSmoothingCurrentWeight
	}
	if c.FundingRateContinuationDecay <= 0 || c.FundingRateContinuationDecay > 1 {
		c.FundingRateContinuationDecay = c.Prediction.FundingRateContinuationDecay
	}
	if c.DynamicCandidateLimit == 0 && c.Scan.DynamicCandidateLimit != 0 {
		c.DynamicCandidateLimit = c.Scan.DynamicCandidateLimit
	}
	if c.RotationBatchSize == 0 && c.Scan.RotationBatchSize != 0 {
		c.RotationBatchSize = c.Scan.RotationBatchSize
	}
	if c.RotationInterval <= 0 {
		c.RotationInterval = c.Scan.RotationInterval
	}
	if c.DeepScanHoldDuration <= 0 {
		c.DeepScanHoldDuration = c.Scan.DeepScanHoldDuration
	}
	if c.FundingSnapshotPersistInterval <= 0 {
		if c.SnapshotPersistInterval > 0 {
			c.FundingSnapshotPersistInterval = c.SnapshotPersistInterval
		} else {
			c.FundingSnapshotPersistInterval = 60 * time.Second
		}
	}
	if c.BookSnapshotPersistInterval <= 0 {
		if c.SnapshotPersistInterval > 0 {
			c.BookSnapshotPersistInterval = c.SnapshotPersistInterval
		} else {
			c.BookSnapshotPersistInterval = 20 * time.Second
		}
	}
	if c.SnapshotRetention <= 0 {
		c.SnapshotRetention = 7 * 24 * time.Hour
	}
	if c.BookSnapshotMinPriceChangeBps <= 0 {
		c.BookSnapshotMinPriceChangeBps = 0.5
	}
	if c.BookSnapshotMinQtyChangeRatio <= 0 {
		c.BookSnapshotMinQtyChangeRatio = 0.05
	}
	if c.OpportunityCalcInterval <= 0 {
		c.OpportunityCalcInterval = 5 * time.Second
	}
	if c.FundingHistoryLookback <= 0 {
		c.FundingHistoryLookback = 6 * time.Hour
	}
	if c.FundingSmoothingCurrentWeight < 0 || c.FundingSmoothingCurrentWeight > 1 {
		c.FundingSmoothingCurrentWeight = 0.7
	}
	if c.FundingRateContinuationDecay <= 0 || c.FundingRateContinuationDecay > 1 {
		c.FundingRateContinuationDecay = 0.6
	}
	// DynamicCandidateLimit = 0 表示“全量候选都允许进入深扫池”，
	// 因此这里不能再把 0 自动改写成 30。
	if c.DynamicCandidateLimit < 0 {
		c.DynamicCandidateLimit = 30
	}
	if c.RotationBatchSize < 0 {
		c.RotationBatchSize = 0
	}
	if c.RotationInterval <= 0 {
		c.RotationInterval = 30 * time.Second
	}
	if c.DeepScanHoldDuration <= 0 {
		c.DeepScanHoldDuration = 3 * time.Minute
	}
	if c.SlippageBps < 0 {
		c.SlippageBps = 0
	}
	if c.MaxDataAge <= 0 {
		c.MaxDataAge = 15 * time.Second
	}
	if c.MaxSpreadBps <= 0 {
		c.MaxSpreadBps = 12
	}
	if c.DynamicMaxSpreadMultiplier < 1 {
		c.DynamicMaxSpreadMultiplier = 1.5
	}
	if c.DynamicMaxSpreadReferenceHours <= 0 {
		c.DynamicMaxSpreadReferenceHours = c.HoldHours
	}
	if c.EntryLeadTime <= 0 {
		c.EntryLeadTime = 3 * time.Minute
	}
	if c.EntryCutoffTime <= 0 {
		c.EntryCutoffTime = 45 * time.Second
	}
	c.EntryMode = normalizeMode(c.EntryMode, "taker")
	c.ExitMode = normalizeMode(c.ExitMode, "taker")
	if c.Execution.CloseGracePeriod <= 0 {
		c.Execution.CloseGracePeriod = c.Rolling.Review.SettleGracePeriod
	}
	if c.Execution.CloseGracePeriod <= 0 {
		c.Execution.CloseGracePeriod = 15 * time.Second
	}
	if c.Execution.LoopInterval <= 0 {
		c.Execution.LoopInterval = 3 * time.Second
	}
	if c.Execution.MaxLatestPlans <= 0 {
		c.Execution.MaxLatestPlans = 50
	}
	if c.Execution.MaxLivePlans < 0 {
		c.Execution.MaxLivePlans = 0
	}
	if c.Execution.MaxAutoOpenPerLoop < 0 {
		c.Execution.MaxAutoOpenPerLoop = 0
	}
	if c.Execution.MinAvailableBalanceRatio <= 0 || c.Execution.MinAvailableBalanceRatio > 1 {
		c.Execution.MinAvailableBalanceRatio = 0.1
	}
	if c.Execution.MaxUnrealizedLossUSDT < 0 {
		c.Execution.MaxUnrealizedLossUSDT = 0
	}
	if c.Execution.MaxUnwindBasisBps < 0 {
		c.Execution.MaxUnwindBasisBps = 0
	}
	if c.Execution.EmergencyMinAvailableBalanceRatio < 0 || c.Execution.EmergencyMinAvailableBalanceRatio > 1 {
		c.Execution.EmergencyMinAvailableBalanceRatio = 0
	}
	if c.Execution.PrimaryLegTimeout < 0 {
		c.Execution.PrimaryLegTimeout = 0
	}
	if c.Execution.APIFailureThreshold <= 0 {
		c.Execution.APIFailureThreshold = 3
	}
	if c.Execution.APIFailureCooldown <= 0 {
		c.Execution.APIFailureCooldown = 2 * time.Minute
	}
	if c.Execution.OrderStatusPollAttempts <= 0 {
		c.Execution.OrderStatusPollAttempts = 3
	}
	if c.Execution.OrderStatusPollInterval <= 0 {
		c.Execution.OrderStatusPollInterval = 1500 * time.Millisecond
	}

	c.RollingEntryPathRequireConsistentDirection = boolOrDefault(c.Rolling.EntryPathRequireConsistentDirection, true)
	c.RollingMaxSingleExchangeForecastSegments = c.Rolling.MaxSingleExchangeForecastSegments
	if c.RollingMaxSingleExchangeForecastSegments < 0 {
		c.RollingMaxSingleExchangeForecastSegments = 0
	}
	c.RollingAllowIntermediateForecastBeforeBoundary = boolOrDefault(
		firstBoolPtr(c.Rolling.AllowIntermediateForecastBeforeBoundary, c.Prediction.AllowIntermediateForecastBeforeBoundary),
		true,
	)
	c.RollingForbidBoundaryForecast = boolOrDefault(
		firstBoolPtr(c.Rolling.ForbidBoundaryForecast, c.Prediction.ForbidBoundaryForecast),
		true,
	)
	c.RollingReviewSettleGracePeriod = firstPositiveDuration(c.Rolling.Review.SettleGracePeriod, c.Execution.CloseGracePeriod)
	if c.RollingReviewSettleGracePeriod <= 0 {
		c.RollingReviewSettleGracePeriod = 15 * time.Second
	}
	c.RollingReviewFreshSnapshotMaxWait = c.Rolling.Review.FreshSnapshotMaxWait
	if c.RollingReviewFreshSnapshotMaxWait <= 0 {
		c.RollingReviewFreshSnapshotMaxWait = 20 * time.Second
	}
	c.RollingReviewCloseOnSnapshotTimeout = boolOrDefault(c.Rolling.Review.CloseOnSnapshotTimeout, true)
	c.RollingReviewContinueOnSameDirection = boolOrDefault(c.Rolling.Review.ContinueOnSameDirection, true)
	c.RollingReviewCloseOnUnprofitable = boolOrDefault(c.Rolling.Review.CloseOnUnprofitable, true)
	c.RollingReviewRequireIncrementalNetPositive = boolOrDefault(c.Rolling.Review.RequireIncrementalNetPositive, true)
	c.RollingReviewMinIncrementalNetPNL = c.Rolling.Review.MinIncrementalNetPNL
	if c.RollingReviewMinIncrementalNetPNL < 0 {
		c.RollingReviewMinIncrementalNetPNL = 0
	}
	c.RollingFlipEnabled = boolOrDefault(c.Rolling.Flip.Enabled, true)
	c.RollingFlipRequireNetPositive = boolOrDefault(c.Rolling.Flip.RequireNetPositive, true)
	c.RollingFlipMinNetPNL = firstPositiveFloat(c.Rolling.Flip.MinNetPNL, c.MinNetPNL)
	if c.RollingFlipMinNetPNL <= 0 {
		c.RollingFlipMinNetPNL = c.MinNetPNL
	}
	c.RollingFlipSlippageMultiplier = c.Rolling.Flip.SlippageMultiplier
	if c.RollingFlipSlippageMultiplier <= 0 {
		c.RollingFlipSlippageMultiplier = 1.5
	}
	c.RollingFlipExtraSafetyBufferUSDT = c.Rolling.Flip.ExtraSafetyBufferUSDT
	if c.RollingFlipExtraSafetyBufferUSDT <= 0 {
		c.RollingFlipExtraSafetyBufferUSDT = c.SafetyBufferUSDT
	}
	return c
}

func (c Config) EffectiveNotional() float64 {
	notional := c.TotalCapitalUSDT * c.CapitalUtilization * c.Leverage
	if notional > 0 {
		return notional
	}
	return c.AssumedNotional
}

func normalizeMode(mode, fallback string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "maker", "taker", "mixed":
		return mode
	default:
		return fallback
	}
}

func normalizeStrategyMode(mode, fallback string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case StrategyModeLegacyProjection, StrategyModeRollingCycleAligned:
		return mode
	default:
		return fallback
	}
}

func normalizeHoldSelectionMode(mode, fallback string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case HoldSelectionModeBestNet, HoldSelectionModeLatestProfitable, HoldSelectionModeStrictTarget:
		return mode
	default:
		return fallback
	}
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func firstBoolPtr(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstPositiveDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
