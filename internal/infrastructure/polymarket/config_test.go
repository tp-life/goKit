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
