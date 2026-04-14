package service

import (
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

func TestSystemServiceStatus_IncludesExecutionBudgetMetrics(t *testing.T) {
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:               "plan-opened",
				Status:                executionStateOpened,
				LiveTrading:           true,
				AutoClose:             true,
				StrategyMode:          StrategyModeRollingCycleAligned,
				RollingGroupKey:       "BTC|aster|binance",
				NextReviewTimeMs:      time.Now().Add(time.Minute).UnixMilli(),
				AllocatedNotionalUSDT: 500,
			},
			{
				PlanKey:               "plan-pending-close",
				Status:                executionStatePendingClose,
				LiveTrading:           true,
				AllocatedNotionalUSDT: 300,
			},
			{
				PlanKey:               "plan-dry-run",
				Status:                "dry_run_opened",
				LiveTrading:           false,
				AllocatedNotionalUSDT: 200,
			},
		},
	}

	svc := NewSystemService(
		NewMarketStore(),
		Config{
			TotalCapitalUSDT:   1000,
			CapitalUtilization: 0.8,
			Leverage:           2,
			StrategyMode:       StrategyModeRollingCycleAligned,
			ArbitrageMode:      ArbitrageModeCrossExchange,
			HoldSelectionMode:  HoldSelectionModeLatestProfitable,
			SameExchange: StrategySameExchangeConfig{
				Entry: StrategySameExchangeEntryConfig{
					BasisLongHoldWindowHours: 14,
					MaxBasisPaybackEvents:    4.5,
				},
				Risk: StrategySameExchangeRiskConfig{
					MaxPerpLeverage:              1.4,
					ReduceLiqDistanceRatio:       0.1,
					EmergencyLiqDistanceRatio:    0.08,
					Max1hPriceShockRatio:         0.09,
					FundingExtremePercentile:     0.95,
					ExtremeFundingNegativeRatio:  0.5,
					ExtremeFundingSizeMultiplier: 0.5,
					ExtremeBasisPaybackEvents:    2.6,
					ExtremeBasisSizeMultiplier:   0.7,
				},
			},
			Rolling: StrategyRollingConfig{
				Review: StrategyRollingReviewConfig{
					FreshSnapshotMaxWait: 12 * time.Second,
				},
			},
			Execution: ExecutionConfig{
				MaxLivePlans:        3,
				MaxAutoOpenPerLoop:  1,
				AutoAllocateCapital: true,
				PositionMonitor: ExecutionPositionMonitorConfig{
					MaxQtyDeviationRatio: 0.2,
				},
				Replacement: ExecutionReplacementConfig{
					Enabled:              boolPtr(true),
					OnlyWhenConstrained:  boolPtr(true),
					MinNetImprovementPNL: 1.2,
				},
			},
		},
		exchange.ConfigSet{},
		execRepo,
	)

	status := svc.Status()
	execution, ok := status["execution"].(map[string]any)
	if !ok {
		t.Fatal("expected execution status payload")
	}

	if got := execution["active_live_plans"]; got != 2 {
		t.Fatalf("expected active_live_plans 2, got %#v", got)
	}
	if got := execution["active_allocated_notional_usdt"]; got != 800.0 {
		t.Fatalf("expected active_allocated_notional_usdt 800, got %#v", got)
	}
	if got := execution["remaining_auto_budget_usdt"]; got != 800.0 {
		t.Fatalf("expected remaining_auto_budget_usdt 800, got %#v", got)
	}
	if got := execution["remaining_live_slots"]; got != 1 {
		t.Fatalf("expected remaining_live_slots 1, got %#v", got)
	}
	if got := execution["active_rolling_records"]; got != 1 {
		t.Fatalf("expected active_rolling_records 1, got %#v", got)
	}
	if got := execution["active_replacement_enabled"]; got != true {
		t.Fatalf("expected active_replacement_enabled true, got %#v", got)
	}
	if got := execution["position_monitor_max_qty_deviation_ratio"]; got != 0.2 {
		t.Fatalf("expected position_monitor_max_qty_deviation_ratio 0.2, got %#v", got)
	}

	strategy, ok := status["strategy"].(map[string]any)
	if !ok {
		t.Fatal("expected strategy status payload")
	}
	if got := strategy["hold_selection_mode"]; got != HoldSelectionModeLatestProfitable {
		t.Fatalf("expected hold_selection_mode %s, got %#v", HoldSelectionModeLatestProfitable, got)
	}
	if got := strategy["mode"]; got != StrategyModeRollingCycleAligned {
		t.Fatalf("expected strategy mode %s, got %#v", StrategyModeRollingCycleAligned, got)
	}
	if got := strategy["arbitrage_mode"]; got != ArbitrageModeCrossExchange {
		t.Fatalf("expected arbitrage_mode %s, got %#v", ArbitrageModeCrossExchange, got)
	}
	if got := strategy["rolling_review_close_on_snapshot_timeout"]; got != true {
		t.Fatalf("expected rolling_review_close_on_snapshot_timeout true, got %#v", got)
	}
	if got := strategy["same_exchange_max_perp_leverage"]; got != 1.4 {
		t.Fatalf("expected same_exchange_max_perp_leverage 1.4, got %#v", got)
	}
	if got := strategy["same_exchange_max_basis_payback_events"]; got != 4.5 {
		t.Fatalf("expected same_exchange_max_basis_payback_events 4.5, got %#v", got)
	}
	if got := strategy["same_exchange_extreme_basis_size_multiplier"]; got != 0.7 {
		t.Fatalf("expected same_exchange_extreme_basis_size_multiplier 0.7, got %#v", got)
	}
	if got := strategy["same_exchange_max_1h_price_shock_ratio"]; got != 0.09 {
		t.Fatalf("expected same_exchange_max_1h_price_shock_ratio 0.09, got %#v", got)
	}
}

func boolPtr(value bool) *bool { return &value }
