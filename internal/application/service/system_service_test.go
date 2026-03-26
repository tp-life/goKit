package service

import (
	"testing"

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
}
