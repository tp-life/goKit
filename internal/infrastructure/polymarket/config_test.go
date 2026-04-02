package polymarket

import (
	"path/filepath"
	"testing"
)

// TestParseMarketTargets 确认 watchlist 能稳定解析多个 symbol/interval 组合。
func TestParseMarketTargets(t *testing.T) {
	targets := parseMarketTargets("BTC:15m,ETH:15m,SOL:5m", "BTC", 900)
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}

	if targets[0].Key != "btc-15m" || targets[0].Symbol != "BTC" || targets[0].IntervalSec != 900 {
		t.Fatalf("unexpected first target: %+v", targets[0])
	}
	if targets[2].Key != "sol-5m" || targets[2].IntervalSec != 300 {
		t.Fatalf("unexpected third target: %+v", targets[2])
	}
}

// TestCloneForTargetUsesDedicatedStateFile 确认多市场模式会为每个 worker 分配独立状态文件。
func TestCloneForTargetUsesDedicatedStateFile(t *testing.T) {
	cfg := Config{
		StateFile: filepath.Join("data", "polymarket", "state.json"),
		MarketTargets: []MarketTargetConfig{
			{Key: "btc-15m", Label: "BTC 15m", Symbol: "BTC", IntervalSec: 900},
			{Key: "eth-15m", Label: "ETH 15m", Symbol: "ETH", IntervalSec: 900},
		},
	}

	child := cfg.CloneForTarget(cfg.MarketTargets[1], 1, false)
	if child.MarketSymbol != "ETH" {
		t.Fatalf("expected child symbol ETH, got %s", child.MarketSymbol)
	}
	if child.EnableAccountPolling {
		t.Fatalf("expected shared account tasks to be disabled for non-primary worker")
	}
	if filepath.Base(child.StateFile) != "state_eth_15m.json" {
		t.Fatalf("unexpected child state file: %s", child.StateFile)
	}
}

// TestCloneForTargetDerivesBinanceWSURL 确认多市场模式不会把 BTC 默认 websocket 地址错误复用到其他资产。
func TestCloneForTargetDerivesBinanceWSURL(t *testing.T) {
	cfg := Config{
		MarketSymbol:  "BTC",
		BinanceSymbol: "BTCUSDT",
		BinanceWSURL:  "wss://stream.binance.com:9443/ws/btcusdt@trade",
	}

	child := cfg.CloneForTarget(MarketTargetConfig{
		Key:         "eth-15m",
		Label:       "ETH 15m",
		Symbol:      "ETH",
		IntervalSec: 900,
	}, 1, false)

	if child.ResolvedBinanceSymbol() != "ETHUSDT" {
		t.Fatalf("expected child binance symbol ETHUSDT, got %s", child.ResolvedBinanceSymbol())
	}
	if got := child.ResolvedBinanceWSURL(); got != "wss://stream.binance.com:9443/ws/ethusdt@trade" {
		t.Fatalf("unexpected child binance ws url: %s", got)
	}
}

// TestDefaultCryptoPriceVariantByInterval 确认 5 分钟和 15 分钟市场都显式使用官网对应的 PTB 变体名。
func TestDefaultCryptoPriceVariantByInterval(t *testing.T) {
	if got := defaultCryptoPriceVariant(300); got != "fiveminute" {
		t.Fatalf("expected 300s crypto-price variant fiveminute, got %q", got)
	}
	if got := defaultCryptoPriceVariant(900); got != "fifteen" {
		t.Fatalf("expected 900s crypto-price variant fifteen, got %q", got)
	}
}

// TestCloneForTargetAppliesMarketOverrides 确认单市场 worker 能按 watchlist 键名读取独立策略覆盖。
func TestCloneForTargetAppliesMarketOverrides(t *testing.T) {
	t.Setenv("MARKET_ETH_15M_TRADE_AMOUNT", "3")
	t.Setenv("MARKET_ETH_15M_AUTO_TRADE_CONFIRM_SEC", "2")
	t.Setenv("MARKET_ETH_15M_BINANCE_REQUIRE_ALIGNMENT", "true")
	t.Setenv("MARKET_ETH_15M_BINANCE_CONFIRM_MIN_DIFF_BPS", "1.5")
	t.Setenv("MARKET_ETH_15M_CONDITION_1_DIFF_BPS", "6")
	t.Setenv("MARKET_ETH_15M_CONDITION_3_TIME", "0")

	cfg := Config{
		TradeAmount:         5,
		AutoTradeConfirmSec: 0,
		Conditions: []ConditionConfig{
			{Slot: 1, Time: 120, DiffBps: 3, MinProb: 0.80, MaxProb: 0.92},
			{Slot: 2, Time: 120, DiffBps: 3, MinProb: 0.80, MaxProb: 0.92},
			{Slot: 3, Time: 60, DiffBps: 5, MinProb: 0.80, MaxProb: 0.92},
			{Slot: 4, Time: 60, DiffBps: 5, MinProb: 0.80, MaxProb: 0.92},
		},
	}

	child := cfg.CloneForTarget(MarketTargetConfig{
		Key:         "eth-15m",
		Label:       "ETH 15m",
		Symbol:      "ETH",
		IntervalSec: 900,
	}, 1, false)

	if child.TradeAmount != 3 {
		t.Fatalf("expected overridden trade amount 3, got %.2f", child.TradeAmount)
	}
	if child.AutoTradeConfirmSec != 2 {
		t.Fatalf("expected overridden confirm window 2, got %.2f", child.AutoTradeConfirmSec)
	}
	if !child.BinanceRequireAlign || child.BinanceConfirmMinBps != 1.5 {
		t.Fatalf("expected Binance confirmation override to apply, got %+v", child)
	}
	if child.Conditions[0].DiffBps != 6 {
		t.Fatalf("expected condition 1 diff bps override, got %+v", child.Conditions[0])
	}
	if child.Conditions[2].Time != 0 {
		t.Fatalf("expected condition 3 to be disabled, got %+v", child.Conditions[2])
	}
}

// TestCloneForTargetResolvesTailSweepAllowance 确认尾盘扫尾巴策略可按市场白名单启用到单市场 worker。
func TestCloneForTargetResolvesTailSweepAllowance(t *testing.T) {
	cfg := Config{
		TailSweepEnabled: true,
		TailSweepTargets: []MarketTargetConfig{
			{Key: "btc-15m", Label: "BTC 15m", Symbol: "BTC", IntervalSec: 900},
		},
	}

	btcChild := cfg.CloneForTarget(MarketTargetConfig{
		Key:         "btc-15m",
		Label:       "BTC 15m",
		Symbol:      "BTC",
		IntervalSec: 900,
	}, 0, true)
	if !btcChild.TailSweepAllowed {
		t.Fatalf("expected BTC worker to allow tail sweep")
	}

	ethChild := cfg.CloneForTarget(MarketTargetConfig{
		Key:         "eth-15m",
		Label:       "ETH 15m",
		Symbol:      "ETH",
		IntervalSec: 900,
	}, 1, false)
	if ethChild.TailSweepAllowed {
		t.Fatalf("expected ETH worker to reject tail sweep")
	}
}

// TestResolvedMarketTargetsMergesTailSweepTargets 确认尾盘白名单市场会自动并入实际 watchlist，便于展示价格与切换焦点。
func TestResolvedMarketTargetsMergesTailSweepTargets(t *testing.T) {
	cfg := Config{
		MarketSymbol:      "BTC",
		MarketIntervalSec: 900,
		MarketTargets: []MarketTargetConfig{
			{Key: "btc-15m", Label: "BTC 15m", Symbol: "BTC", IntervalSec: 900},
		},
		TailSweepTargets: []MarketTargetConfig{
			{Key: "eth-15m", Label: "ETH 15m", Symbol: "ETH", IntervalSec: 900},
			{Key: "btc-15m", Label: "BTC 15m", Symbol: "BTC", IntervalSec: 900},
		},
	}

	targets := cfg.ResolvedMarketTargets()
	if len(targets) != 2 {
		t.Fatalf("expected merged watchlist to contain 2 unique targets, got %d: %+v", len(targets), targets)
	}
	if targets[0].Key != "btc-15m" || targets[1].Key != "eth-15m" {
		t.Fatalf("unexpected merged watchlist order: %+v", targets)
	}
}

// TestResolvedMarketTargetsSupportsTailSweepOnlyWatchlist 确认只配置尾盘市场时，也会启动对应 worker 并展示价格。
func TestResolvedMarketTargetsSupportsTailSweepOnlyWatchlist(t *testing.T) {
	cfg := Config{
		MarketSymbol:      "BTC",
		MarketIntervalSec: 900,
		TailSweepTargets: []MarketTargetConfig{
			{Key: "sol-5m", Label: "SOL 5m", Symbol: "SOL", IntervalSec: 300},
		},
	}

	targets := cfg.ResolvedMarketTargets()
	if len(targets) != 1 {
		t.Fatalf("expected tail sweep only config to produce one watchlist target, got %d: %+v", len(targets), targets)
	}
	if targets[0].Key != "sol-5m" || targets[0].Symbol != "SOL" || targets[0].IntervalSec != 300 {
		t.Fatalf("unexpected tail sweep only target: %+v", targets[0])
	}
}

// TestCloneForTargetCanDisableMainStrategy 确认某个市场可以只保留尾盘/价格观察，而不参与主策略。
func TestCloneForTargetCanDisableMainStrategy(t *testing.T) {
	t.Setenv("MARKET_SOL_5M_MAIN_STRATEGY_ENABLED", "false")

	cfg := Config{
		MainStrategyEnabled: true,
	}

	child := cfg.CloneForTarget(MarketTargetConfig{
		Key:         "sol-5m",
		Label:       "SOL 5m",
		Symbol:      "SOL",
		IntervalSec: 300,
	}, 1, false)

	if child.MainStrategyEnabled {
		t.Fatalf("expected SOL 5m child to disable main strategy")
	}
}

// TestCloneForTargetDisablesMainStrategyForTailOnlyMarket 确认仅存在于尾盘白名单的市场默认不跑主策略。
func TestCloneForTargetDisablesMainStrategyForTailOnlyMarket(t *testing.T) {
	cfg := Config{
		MainStrategyEnabled: true,
		MarketTargets: []MarketTargetConfig{
			{Key: "btc-15m", Label: "BTC 15m", Symbol: "BTC", IntervalSec: 900},
		},
		TailSweepTargets: []MarketTargetConfig{
			{Key: "eth-5m", Label: "ETH 5m", Symbol: "ETH", IntervalSec: 300},
		},
	}

	child := cfg.CloneForTarget(MarketTargetConfig{
		Key:         "eth-5m",
		Label:       "ETH 5m",
		Symbol:      "ETH",
		IntervalSec: 300,
	}, 1, false)

	if child.MainStrategyEnabled {
		t.Fatalf("expected tail-only market to disable main strategy by default")
	}
}
