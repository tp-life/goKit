package entity

// Position 表示机器人当前记录的持仓。
type Position struct {
	Slug        string  `json:"slug"`
	Side        string  `json:"side"`
	EntryPrice  float64 `json:"entry_price"`
	EntryDiff   float64 `json:"entry_diff"`
	Size        float64 `json:"size"`
	Amount      float64 `json:"amount,omitempty"`
	WindowSec   int     `json:"window_sec,omitempty"`
	StrategyKey string  `json:"strategy_key,omitempty"`
	Execution   string  `json:"execution,omitempty"`
}

// PendingOrder 表示当前仍在挂单中的订单。
type PendingOrder struct {
	OrderID     string  `json:"order_id"`
	Time        string  `json:"time"`
	Slug        string  `json:"slug"`
	Side        string  `json:"side"`
	Action      string  `json:"action"`
	Reason      string  `json:"reason,omitempty"`
	Price       float64 `json:"price"`
	Size        float64 `json:"size,omitempty"`
	Amount      float64 `json:"amount,omitempty"`
	WindowSec   int     `json:"window_sec,omitempty"`
	StrategyKey string  `json:"strategy_key,omitempty"`
	Execution   string  `json:"execution,omitempty"`
	PostOnly    bool    `json:"post_only,omitempty"`
}

// LastOrder 记录同一市场同一方向最近一次尝试下单的信息。
type LastOrder struct {
	Key        string  `json:"key"`
	Time       string  `json:"time"`
	RetryCount int     `json:"retry_count"`
	LastPrice  float64 `json:"last_price"`
	Error      string  `json:"error,omitempty"`
}

// TradeHistoryItem 表示机器人侧的交易历史项。
type TradeHistoryItem struct {
	ID          string   `json:"id,omitempty"`
	Time        string   `json:"time"`
	Slug        string   `json:"slug"`
	Action      string   `json:"action"`
	Side        string   `json:"side"`
	Price       float64  `json:"price"`
	Amount      float64  `json:"amount,omitempty"`
	Size        float64  `json:"size,omitempty"`
	OrderID     string   `json:"order_id,omitempty"`
	Status      string   `json:"status,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Error       string   `json:"error,omitempty"`
	Diff        *float64 `json:"diff,omitempty"`
	PnL         *float64 `json:"pnl,omitempty"`
	WindowSec   int      `json:"window_sec,omitempty"`
	StrategyKey string   `json:"strategy_key,omitempty"`
	Execution   string   `json:"execution,omitempty"`
	PostOnly    bool     `json:"post_only,omitempty"`
	NetEdgeBps  *float64 `json:"net_edge_bps,omitempty"`
}

// PolymarketState 表示需要持久化到本地文件的交易状态。
type PolymarketState struct {
	Position        *Position          `json:"position,omitempty"`
	PendingOrder    *PendingOrder      `json:"pending_order,omitempty"`
	TakeProfitOrder *PendingOrder      `json:"take_profit_order,omitempty"`
	LastOrder       *LastOrder         `json:"last_order,omitempty"`
	LastRedeemAt    string             `json:"last_redeem_at,omitempty"`
	TradeHistory    []TradeHistoryItem `json:"trade_history,omitempty"`
	PTB             *float64           `json:"ptb,omitempty"`
	PTBTarget       *float64           `json:"ptb_target,omitempty"`
	Chainlink       *float64           `json:"chainlink,omitempty"`
	Binance         *float64           `json:"binance,omitempty"`
	UpPrice         *float64           `json:"up_price,omitempty"`
	DownPrice       *float64           `json:"down_price,omitempty"`
	LastUpdate      string             `json:"last_update,omitempty"`
}

// ActivityLog 表示 dashboard 上展示的一条活动日志。
type ActivityLog struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// ActiveMarket 表示当前活跃市场的核心元数据。
type ActiveMarket struct {
	Slug      string   `json:"slug"`
	Start     string   `json:"start,omitempty"`
	End       string   `json:"end,omitempty"`
	Remaining int      `json:"remaining"`
	UpPrice   *float64 `json:"up_price,omitempty"`
	DownPrice *float64 `json:"down_price,omitempty"`
	UpToken   string   `json:"up_token,omitempty"`
	DownToken string   `json:"down_token,omitempty"`
}

// DashboardMarket 表示 dashboard 上的市场摘要信息。
type DashboardMarket struct {
	Slug          string `json:"slug"`
	Remaining     int    `json:"remaining"`
	RemainingText string `json:"remaining_text,omitempty"`
	Start         string `json:"start,omitempty"`
	End           string `json:"end,omitempty"`
	Status        string `json:"status"`
}

// DashboardPrices 表示 dashboard 上展示的一组实时价格。
type DashboardPrices struct {
	PTB          *float64 `json:"ptb,omitempty"`
	TargetPrice  *float64 `json:"target_price,omitempty"`
	ChainlinkBTC *float64 `json:"chainlink_btc,omitempty"`
	BinanceBTC   *float64 `json:"binance_btc,omitempty"`
	UpPrice      *float64 `json:"up_price,omitempty"`
	DownPrice    *float64 `json:"down_price,omitempty"`
	UpBid        *float64 `json:"up_bid,omitempty"`
	UpAsk        *float64 `json:"up_ask,omitempty"`
	DownBid      *float64 `json:"down_bid,omitempty"`
	DownAsk      *float64 `json:"down_ask,omitempty"`
	Diff         *float64 `json:"diff,omitempty"`
	DiffAbs      *float64 `json:"diff_abs,omitempty"`
	UpdatedTS    int64    `json:"updated_ts,omitempty"`
}

// TrackedMarketView 表示 watchlist 中单个市场的快照摘要。
type TrackedMarketView struct {
	Key          string               `json:"key"`
	Label        string               `json:"label,omitempty"`
	Symbol       string               `json:"symbol,omitempty"`
	IntervalSec  int                  `json:"interval_sec,omitempty"`
	UpdatedAt    string               `json:"updated_at,omitempty"`
	Market       DashboardMarket      `json:"market"`
	Prices       DashboardPrices      `json:"prices"`
	Position     *Position            `json:"position,omitempty"`
	PendingOrder *PendingOrder        `json:"pending_order,omitempty"`
	LastOrder    *LastOrder           `json:"last_order,omitempty"`
	AutoTrade    AutoTradeDiagnostics `json:"auto_trade"`
	AutoConfig   AutoTradeConfigView  `json:"auto_trade_config"`
}

// WalletPosition 表示从 Data API 同步回来的钱包持仓视图。
type WalletPosition struct {
	ProxyWallet string   `json:"proxyWallet,omitempty"`
	Asset       string   `json:"asset,omitempty"`
	ConditionID string   `json:"conditionId,omitempty"`
	Slug        string   `json:"slug,omitempty"`
	EventSlug   string   `json:"eventSlug,omitempty"`
	Title       string   `json:"title,omitempty"`
	Outcome     string   `json:"outcome,omitempty"`
	Side        string   `json:"side,omitempty"`
	Size        float64  `json:"size"`
	AvgPrice    *float64 `json:"avgPrice,omitempty"`
	CurPrice    *float64 `json:"curPrice,omitempty"`
	RealizedPnL *float64 `json:"realizedPnl,omitempty"`
	Redeemable  bool     `json:"redeemable,omitempty"`
	Mergeable   bool     `json:"mergeable,omitempty"`
}

// LiveTradeSummary 表示按市场聚合后的实时交易摘要。
type LiveTradeSummary struct {
	ID              string   `json:"id"`
	PairID          string   `json:"pair_id,omitempty"`
	Direction       string   `json:"direction,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Slug            string   `json:"slug,omitempty"`
	BuyCount        int      `json:"buy_count"`
	SellCount       int      `json:"sell_count"`
	RedeemCount     int      `json:"redeem_count"`
	BuyUSDC         float64  `json:"buy_usdc"`
	SellUSDC        float64  `json:"sell_usdc"`
	RedeemUSDC      float64  `json:"redeem_usdc"`
	Size            float64  `json:"size"`
	EntryPriceQuote *float64 `json:"entry_price_quote,omitempty"`
	ExitPriceQuote  *float64 `json:"exit_price_quote,omitempty"`
	OrderTime       string   `json:"order_time,omitempty"`
	SettleTime      string   `json:"settle_time,omitempty"`
	Profit          float64  `json:"profit"`
	Result          string   `json:"result,omitempty"`
	Status          string   `json:"status,omitempty"`
}

// AutoRedeemStatus 表示自动兑奖模块当前状态。
type AutoRedeemStatus struct {
	Enabled        bool           `json:"enabled"`
	PendingCount   int            `json:"pending_count"`
	ClaimableCount int            `json:"claimable_count"`
	LastResult     map[string]any `json:"last_result,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	LastRunAt      string         `json:"last_run_at,omitempty"`
	NextRunAt      string         `json:"next_run_at,omitempty"`
}

// AutoTradeDiagnostics 表示自动交易触发评估的诊断统计。
type AutoTradeDiagnostics struct {
	MarketSlug            string `json:"market_slug,omitempty"`
	SampleCount           int    `json:"sample_count"`
	TriggerCount          int    `json:"trigger_count"`
	NoMarketCount         int    `json:"no_market_count"`
	MarketClosedCount     int    `json:"market_closed_count"`
	TimeWindowMissCount   int    `json:"time_window_miss_count"`
	ReferenceMissingCount int    `json:"reference_missing_count"`
	OutcomePriceMissing   int    `json:"outcome_price_missing_count"`
	DiffMissCount         int    `json:"diff_miss_count"`
	ProbabilityMissCount  int    `json:"probability_miss_count"`
	DataLagCount          int    `json:"data_lag_count"`
	BlockedByStateCount   int    `json:"blocked_by_state_count"`
	RetryLimitCount       int    `json:"retry_limit_count"`
	SignalConfirmCount    int    `json:"signal_confirm_count"`
	BinanceVetoCount      int    `json:"binance_veto_count"`
	NetEdgeMissCount      int    `json:"net_edge_miss_count"`
	LastReason            string `json:"last_reason,omitempty"`
	LastReasonAt          string `json:"last_reason_at,omitempty"`
	LastTriggerAt         string `json:"last_trigger_at,omitempty"`
}

// AutoTradeConditionView 表示一条自动交易触发条件的只读视图。
type AutoTradeConditionView struct {
	Index   int     `json:"index"`
	Enabled bool    `json:"enabled"`
	Time    int     `json:"time,omitempty"`
	DiffBps float64 `json:"diff_bps,omitempty"`
	MinProb float64 `json:"min_prob,omitempty"`
	MaxProb float64 `json:"max_prob,omitempty"`
}

// AutoTradeConfigView 表示当前市场实际生效的自动交易配置摘要。
type AutoTradeConfigView struct {
	MainStrategyEnabled        bool                     `json:"main_strategy_enabled,omitempty"`
	TradeAmount                float64                  `json:"trade_amount,omitempty"`
	ConfirmSec                 float64                  `json:"confirm_sec,omitempty"`
	MarketDataMaxLagSec        float64                  `json:"market_data_max_lag_sec,omitempty"`
	MinNetEdgeBps              float64                  `json:"min_net_edge_bps,omitempty"`
	PreferPostOnly             bool                     `json:"prefer_post_only,omitempty"`
	StopLossProbPct            float64                  `json:"stop_loss_prob_pct,omitempty"`
	StopLossHoldFinalSec       int                      `json:"stop_loss_hold_final_sec,omitempty"`
	StopLossHoldMinDiffBps     float64                  `json:"stop_loss_hold_min_diff_bps,omitempty"`
	StopLossHoldRequireBinance bool                     `json:"stop_loss_hold_require_binance,omitempty"`
	StopLossHoldMaxLagSec      float64                  `json:"stop_loss_hold_max_lag_sec,omitempty"`
	TakeProfitRR               float64                  `json:"take_profit_rr,omitempty"`
	BinanceRequireAlign        bool                     `json:"binance_require_alignment,omitempty"`
	BinanceConfirmMinBps       float64                  `json:"binance_confirm_min_bps,omitempty"`
	BinanceVetoMaxDevBps       float64                  `json:"binance_veto_max_dev_bps,omitempty"`
	TailSweepEnabled           bool                     `json:"tail_sweep_enabled,omitempty"`
	TailSweepAllowed           bool                     `json:"tail_sweep_allowed,omitempty"`
	TailSweepFinalSec          int                      `json:"tail_sweep_final_sec,omitempty"`
	TailSweepMinDiffBps        float64                  `json:"tail_sweep_min_diff_bps,omitempty"`
	TailSweepMinProb           float64                  `json:"tail_sweep_min_prob,omitempty"`
	TailSweepMaxProb           float64                  `json:"tail_sweep_max_prob,omitempty"`
	TailSweepMaxPrice          float64                  `json:"tail_sweep_max_price,omitempty"`
	TailSweepRequireBinance    bool                     `json:"tail_sweep_require_binance,omitempty"`
	TailSweepMaxLagSec         float64                  `json:"tail_sweep_max_lag_sec,omitempty"`
	TailSweepMaxSpread         float64                  `json:"tail_sweep_max_spread,omitempty"`
	TailSweepSizeRatio         float64                  `json:"tail_sweep_size_ratio,omitempty"`
	TailSweepMaxTradesPerHour  int                      `json:"tail_sweep_max_trades_per_hour,omitempty"`
	TailSweepLossStreakLimit   int                      `json:"tail_sweep_loss_streak_limit,omitempty"`
	TailSweepHoldToSettlement  bool                     `json:"tail_sweep_hold_to_settlement,omitempty"`
	Conditions                 []AutoTradeConditionView `json:"conditions,omitempty"`
}

// StrategyPerformance 表示某个市场/窗口/方向在本地闭环历史上的收益表现。
type StrategyPerformance struct {
	StrategyKey   string  `json:"strategy_key"`
	MarketKey     string  `json:"market_key"`
	MarketSlug    string  `json:"market_slug,omitempty"`
	Mode          string  `json:"mode,omitempty"`
	Side          string  `json:"side,omitempty"`
	WindowSec     int     `json:"window_sec,omitempty"`
	Trades        int     `json:"trades"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	WinRate       float64 `json:"win_rate"`
	Profit        float64 `json:"profit"`
	AvgProfit     float64 `json:"avg_profit"`
	LastClosedAt  string  `json:"last_closed_at,omitempty"`
	Disabled      bool    `json:"disabled,omitempty"`
	DisableReason string  `json:"disable_reason,omitempty"`
}

// GlobalRiskStatus 表示多市场账户层全局风控的当前状态。
type GlobalRiskStatus struct {
	Enabled             bool    `json:"enabled"`
	OpenMarkets         int     `json:"open_markets"`
	OpenNotional        float64 `json:"open_notional"`
	SameSideOpenMarkets int     `json:"same_side_open_markets"`
	LossStreak          int     `json:"loss_streak"`
	CooldownUntil       string  `json:"cooldown_until,omitempty"`
	LastBlockReason     string  `json:"last_block_reason,omitempty"`
}

// RoundResult 表示一个市场轮次的结算结果摘要。
type RoundResult struct {
	Kind         string   `json:"kind"`
	Slug         string   `json:"slug"`
	Start        string   `json:"start,omitempty"`
	End          string   `json:"end,omitempty"`
	Time         string   `json:"time,omitempty"`
	Status       string   `json:"status,omitempty"`
	EntrySide    string   `json:"entry_side,omitempty"`
	FinalOutcome string   `json:"final_outcome,omitempty"`
	FinalDiff    *float64 `json:"final_diff,omitempty"`
	Profit       *float64 `json:"profit,omitempty"`
	SkipReason   string   `json:"skip_reason,omitempty"`
}

// DashboardState 表示前端 dashboard 所需的完整状态快照。
type DashboardState struct {
	UpdatedAt           string                `json:"updated_at,omitempty"`
	SelectedMarketKey   string                `json:"selected_market_key,omitempty"`
	Market              DashboardMarket       `json:"market"`
	WalletBalance       *float64              `json:"wallet_balance,omitempty"`
	Prices              DashboardPrices       `json:"prices"`
	Position            *Position             `json:"position,omitempty"`
	PendingOrder        *PendingOrder         `json:"pending_order,omitempty"`
	LastOrder           *LastOrder            `json:"last_order,omitempty"`
	TradeHistory        []TradeHistoryItem    `json:"trade_history,omitempty"`
	WalletPositions     []WalletPosition      `json:"wallet_positions,omitempty"`
	WalletHistory       []TradeHistoryItem    `json:"wallet_history,omitempty"`
	LiveTrades          []LiveTradeSummary    `json:"live_trades,omitempty"`
	LivePositionsCount  int                   `json:"live_positions_count"`
	LiveRealizedPnL     float64               `json:"live_realized_pnl"`
	LiveUnrealizedPnL   float64               `json:"live_unrealized_pnl"`
	LiveTotalPnL        float64               `json:"live_total_pnl"`
	AutoTrade           AutoTradeDiagnostics  `json:"auto_trade"`
	AutoTradeConfig     AutoTradeConfigView   `json:"auto_trade_config"`
	AutoRedeem          AutoRedeemStatus      `json:"auto_redeem"`
	GlobalRisk          GlobalRiskStatus      `json:"global_risk"`
	Markets             []TrackedMarketView   `json:"markets,omitempty"`
	StrategyPerformance []StrategyPerformance `json:"strategy_performance,omitempty"`
	Activity            []ActivityLog         `json:"activity,omitempty"`
	RoundResults        []RoundResult         `json:"round_results,omitempty"`
}

// NewDashboardState 返回带有默认值的 dashboard 初始状态。
func NewDashboardState() DashboardState {
	return DashboardState{
		Market: DashboardMarket{
			Status: "waiting",
		},
		AutoTrade: AutoTradeDiagnostics{},
		AutoRedeem: AutoRedeemStatus{
			Enabled:    false,
			LastResult: map[string]any{},
		},
		GlobalRisk:          GlobalRiskStatus{},
		Markets:             []TrackedMarketView{},
		StrategyPerformance: []StrategyPerformance{},
		TradeHistory:        []TradeHistoryItem{},
		WalletPositions:     []WalletPosition{},
		WalletHistory:       []TradeHistoryItem{},
		LiveTrades:          []LiveTradeSummary{},
		Activity:            []ActivityLog{},
		RoundResults:        []RoundResult{},
	}
}
