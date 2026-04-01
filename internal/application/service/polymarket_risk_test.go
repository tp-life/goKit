package service

import (
	"strings"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/polymarket"
)

// TestPolymarketManagerBlocksWhenConcurrentMarketsExceeded 确认全局风控会限制同时暴露的市场数量。
func TestPolymarketManagerBlocksWhenConcurrentMarketsExceeded(t *testing.T) {
	manager := &PolymarketManager{
		cfg: polymarket.Config{
			MaxConcurrentMarkets: 1,
			MaxTotalOpenNotional: 10,
			MaxSameSideMarkets:   1,
		},
		workers: map[string]*managedWorker{
			"btc-15m": {
				snapshot: entity.DashboardState{
					Position: &entity.Position{
						Side:   "UP",
						Amount: 5,
					},
				},
			},
			"eth-15m": {
				snapshot: entity.NewDashboardState(),
			},
		},
		order:    []string{"btc-15m", "eth-15m"},
		snapshot: entity.NewDashboardState(),
	}

	err := manager.checkAutoTradeRisk(autoTradeRiskRequest{
		MarketSlug:  "eth-updown-15m",
		Side:        "UP",
		TradeAmount: 5,
	})
	if err == nil {
		t.Fatalf("expected global concurrent market limit to block new trade")
	}
	if !strings.Contains(err.Error(), "达到上限") {
		t.Fatalf("expected clear concurrent limit reason, got %v", err)
	}
}

// TestPolymarketManagerBlocksDuringLossCooldown 确认连续亏损达到阈值后会进入冷却期。
func TestPolymarketManagerBlocksDuringLossCooldown(t *testing.T) {
	now := time.Now()
	manager := &PolymarketManager{
		cfg: polymarket.Config{
			LossStreakLimit:       3,
			LossStreakCooldownMin: 120,
		},
		workers:  map[string]*managedWorker{},
		order:    []string{},
		snapshot: entity.NewDashboardState(),
	}
	manager.snapshot.LiveTrades = []entity.LiveTradeSummary{
		{ID: "t3", Result: "CLOSED", Profit: -1.2, SettleTime: now.Add(-1 * time.Minute).Format(time.RFC3339)},
		{ID: "t2", Result: "CLOSED", Profit: -0.8, SettleTime: now.Add(-2 * time.Minute).Format(time.RFC3339)},
		{ID: "t1", Result: "CLOSED", Profit: -0.5, SettleTime: now.Add(-3 * time.Minute).Format(time.RFC3339)},
	}

	err := manager.checkAutoTradeRisk(autoTradeRiskRequest{
		MarketSlug:  "btc-updown-15m",
		Side:        "UP",
		TradeAmount: 5,
	})
	if err == nil {
		t.Fatalf("expected loss streak cooldown to block new trade")
	}
	if !strings.Contains(err.Error(), "连续亏损停机中") {
		t.Fatalf("expected cooldown reason, got %v", err)
	}
	if manager.lossCooldownUntil.IsZero() {
		t.Fatalf("expected manager to arm cooldown window")
	}
}

// TestBuildStrategyPerformanceFromHistory 确认本地闭环历史可以恢复出市场/窗口/方向收益榜。
func TestBuildStrategyPerformanceFromHistory(t *testing.T) {
	rows := []entity.TradeHistoryItem{
		{
			Time:        "2026-04-01 10:00:00",
			Slug:        "btc-updown-15m-1",
			Action:      "BUY",
			Side:        "UP",
			Price:       0.84,
			Amount:      5,
			Size:        5.9524,
			OrderID:     "buy-1",
			Status:      "filled",
			Reason:      "剩余≤120s 且价差满足阈值(5.00bps)",
			WindowSec:   120,
			StrategyKey: "btc-15m|120s|UP",
			Execution:   "maker_gtd",
		},
		{
			Time:        "2026-04-01 10:05:00",
			Slug:        "btc-updown-15m-1",
			Action:      "SELL",
			Side:        "UP",
			Price:       0.88,
			Amount:      5.2381,
			Size:        5.9524,
			OrderID:     "sell-1",
			Status:      "filled",
			Reason:      "take_profit",
			WindowSec:   120,
			StrategyKey: "btc-15m|120s|UP",
		},
	}

	out := buildStrategyPerformanceFromHistory(rows, 4, 0, false)
	if len(out) != 1 {
		t.Fatalf("expected one strategy row, got %d", len(out))
	}
	if out[0].MarketKey != "btc-15m" || out[0].WindowSec != 120 || out[0].Side != "UP" {
		t.Fatalf("unexpected strategy grouping: %+v", out[0])
	}
	if out[0].Trades != 1 || out[0].Wins != 1 || out[0].Profit <= 0 {
		t.Fatalf("unexpected strategy profit summary: %+v", out[0])
	}
}

// TestPolymarketManagerBlocksNegativeStrategy 确认最近持续亏损的策略桶会被自动停用。
func TestPolymarketManagerBlocksNegativeStrategy(t *testing.T) {
	manager := &PolymarketManager{
		cfg: polymarket.Config{
			AutoDisableNegative:  true,
			AutoDisableLookback:  2,
			AutoDisableMinProfit: -0.1,
		},
		workers: map[string]*managedWorker{
			"btc-15m": {
				snapshot: entity.DashboardState{
					TradeHistory: []entity.TradeHistoryItem{
						{Time: "2026-04-01 09:00:00", Slug: "btc-updown-15m-1", Action: "BUY", Side: "UP", Price: 0.84, Amount: 5, Size: 5.95, OrderID: "b1", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
						{Time: "2026-04-01 09:05:00", Slug: "btc-updown-15m-1", Action: "SELL", Side: "UP", Price: 0.80, Amount: 4.76, Size: 5.95, OrderID: "s1", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
						{Time: "2026-04-01 10:00:00", Slug: "btc-updown-15m-2", Action: "BUY", Side: "UP", Price: 0.83, Amount: 5, Size: 6.02, OrderID: "b2", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
						{Time: "2026-04-01 10:05:00", Slug: "btc-updown-15m-2", Action: "SELL", Side: "UP", Price: 0.79, Amount: 4.75, Size: 6.02, OrderID: "s2", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
					},
				},
			},
		},
		order:    []string{"btc-15m"},
		snapshot: entity.NewDashboardState(),
	}

	err := manager.checkAutoTradeRisk(autoTradeRiskRequest{
		MarketSlug:  "btc-updown-15m-next",
		MarketKey:   "btc-15m",
		Side:        "UP",
		TradeAmount: 5,
		WindowSec:   120,
		StrategyKey: "btc-15m|120s|UP",
	})
	if err == nil {
		t.Fatalf("expected negative strategy auto-disable to block new trade")
	}
	if !strings.Contains(err.Error(), "策略已自动停用") {
		t.Fatalf("expected strategy auto-disable reason, got %v", err)
	}
}

// TestPolymarketManagerRecommendAutoTradeAmountScalesWithPerformance 确认收益稳定的策略会得到更大的自动下单金额。
func TestPolymarketManagerRecommendAutoTradeAmountScalesWithPerformance(t *testing.T) {
	manager := &PolymarketManager{
		cfg: polymarket.Config{
			AutoSizeByPerformance: true,
			AutoSizeLookback:      4,
			AutoSizeMinTrades:     2,
			AutoSizeMinMultiplier: 0.5,
			AutoSizeMaxMultiplier: 1.5,
		},
		workers: map[string]*managedWorker{
			"btc-15m": {
				snapshot: entity.DashboardState{
					TradeHistory: []entity.TradeHistoryItem{
						{Time: "2026-04-01 09:00:00", Slug: "btc-updown-15m-1", Action: "BUY", Side: "UP", Price: 0.84, Amount: 5, Size: 5.95, OrderID: "b1", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
						{Time: "2026-04-01 09:05:00", Slug: "btc-updown-15m-1", Action: "SELL", Side: "UP", Price: 0.89, Amount: 5.30, Size: 5.95, OrderID: "s1", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
						{Time: "2026-04-01 10:00:00", Slug: "btc-updown-15m-2", Action: "BUY", Side: "UP", Price: 0.83, Amount: 5, Size: 6.02, OrderID: "b2", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
						{Time: "2026-04-01 10:05:00", Slug: "btc-updown-15m-2", Action: "SELL", Side: "UP", Price: 0.88, Amount: 5.30, Size: 6.02, OrderID: "s2", Status: "filled", WindowSec: 120, StrategyKey: "btc-15m|120s|UP"},
					},
				},
			},
		},
		order:    []string{"btc-15m"},
		snapshot: entity.NewDashboardState(),
	}

	amount := manager.recommendAutoTradeAmount(autoTradeSizeRequest{
		MarketSlug:      "btc-updown-15m-next",
		MarketKey:       "btc-15m",
		Side:            "UP",
		BaseTradeAmount: 4,
		WindowSec:       120,
		StrategyKey:     "btc-15m|120s|UP",
	})
	if amount <= 4 {
		t.Fatalf("expected profitable strategy to get larger size, got %.4f", amount)
	}
}
