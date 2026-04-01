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
