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
			HoldSelectionMode:  HoldSelectionModeLatestProfitable,
			Rolling: StrategyRollingConfig{
				Review: StrategyRollingReviewConfig{
					FreshSnapshotMaxWait: 12 * time.Second,
				},
			},
			Execution: ExecutionConfig{
				MaxLivePlans:        3,
				MaxAutoOpenPerLoop:  1,
				AutoAllocateCapital: true,
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
	if got := strategy["rolling_review_close_on_snapshot_timeout"]; got != true {
		t.Fatalf("expected rolling_review_close_on_snapshot_timeout true, got %#v", got)
	}
}
