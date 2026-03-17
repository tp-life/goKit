package service

import (
	"strings"
	"time"
)

type ExecutionConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	AutoEntry        bool          `mapstructure:"auto_entry"`
	AutoClose        bool          `mapstructure:"auto_close"`
	CloseGracePeriod time.Duration `mapstructure:"close_grace_period"`
	LoopInterval     time.Duration `mapstructure:"loop_interval"`
	MaxLatestPlans   int           `mapstructure:"max_latest_plans"`
}

type Config struct {
	Enabled bool `mapstructure:"enabled"`
	// AllowedSymbols 是“硬白名单”。为空时表示先不过滤，允许所有 canonical symbol 进入基础池。
	AllowedSymbols []string `mapstructure:"allowed_symbols"`
	// CoreSymbols 是常驻深扫的 symbol。它们会长期保留在盘口订阅列表里，
	// 适合作为 BTC / ETH / SOL 这类“不该错过”的核心交易对。
	CoreSymbols                    []string      `mapstructure:"core_symbols"`
	QuoteAsset                     string        `mapstructure:"quote_asset"`
	HoldHours                      float64       `mapstructure:"hold_hours"`
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
	EntryLeadTime                  time.Duration `mapstructure:"entry_lead_time"`
	EntryCutoffTime                time.Duration `mapstructure:"entry_cutoff_time"`
	SnapshotPersistInterval        time.Duration `mapstructure:"snapshot_persist_interval"`
	FundingSnapshotPersistInterval time.Duration `mapstructure:"funding_snapshot_persist_interval"`
	BookSnapshotPersistInterval    time.Duration `mapstructure:"book_snapshot_persist_interval"`
	SnapshotRetention              time.Duration `mapstructure:"snapshot_retention"`
	BookSnapshotMinPriceChangeBps  float64       `mapstructure:"book_snapshot_min_price_change_bps"`
	BookSnapshotMinQtyChangeRatio  float64       `mapstructure:"book_snapshot_min_qty_change_ratio"`
	OpportunityCalcInterval        time.Duration `mapstructure:"opportunity_calc_interval"`
	// FundingRateContinuationDecay 控制“在同一持仓窗口内，对同一腿未来第2次及以后 funding 事件”的费率衰减。
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
}

func (c Config) normalize() Config {
	if c.QuoteAsset == "" {
		c.QuoteAsset = "USDT"
	}
	if c.HoldHours <= 0 {
		c.HoldHours = 24
	}
	if c.TotalCapitalUSDT <= 0 {
		c.TotalCapitalUSDT = 1000
	}
	if c.CapitalUtilization <= 0 || c.CapitalUtilization > 1 {
		c.CapitalUtilization = 0.8
	}
	if c.Leverage <= 0 {
		c.Leverage = 2
	}
	if c.AssumedNotional <= 0 {
		c.AssumedNotional = c.EffectiveNotional()
	}
	if c.MinNetPNL <= 0 {
		c.MinNetPNL = 1.5
	}
	if c.MaxDisplayedOpportunities <= 0 {
		c.MaxDisplayedOpportunities = 20
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
	if c.EntryLeadTime <= 0 {
		c.EntryLeadTime = 3 * time.Minute
	}
	if c.EntryCutoffTime <= 0 {
		c.EntryCutoffTime = 45 * time.Second
	}
	c.EntryMode = normalizeMode(c.EntryMode, "maker")
	c.ExitMode = normalizeMode(c.ExitMode, "mixed")
	if c.Execution.CloseGracePeriod <= 0 {
		c.Execution.CloseGracePeriod = 2 * time.Minute
	}
	if c.Execution.LoopInterval <= 0 {
		c.Execution.LoopInterval = 3 * time.Second
	}
	if c.Execution.MaxLatestPlans <= 0 {
		c.Execution.MaxLatestPlans = 50
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
