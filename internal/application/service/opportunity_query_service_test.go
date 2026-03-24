package service

import (
	"context"
	"testing"
	"time"

	"goKit/internal/domain/entity"
)

type stubOpportunityRepo struct {
	items []entity.Opportunity
}

func (r stubOpportunityRepo) SaveBatch(_ context.Context, _ string, _ []entity.Opportunity) error {
	return nil
}

func (r stubOpportunityRepo) ListLatest(_ context.Context, _ int) ([]entity.Opportunity, error) {
	return r.items, nil
}

func TestFilterOpportunitiesToCurrentSettlementCycle_KeepsNearestFutureCycle(t *testing.T) {
	now := time.Date(2026, 3, 24, 18, 40, 0, 0, time.UTC)
	items := []entity.Opportunity{
		{Symbol: "BTC", ProjectedFundingTimeMs: now.Add(20 * time.Minute).UnixMilli()},
		{Symbol: "ETH", ProjectedFundingTimeMs: now.Add(20 * time.Minute).UnixMilli()},
		{Symbol: "SOL", ProjectedFundingTimeMs: now.Add(80 * time.Minute).UnixMilli()},
	}

	got := filterOpportunitiesToCurrentSettlementCycle(now, items)
	if len(got) != 2 {
		t.Fatalf("expected 2 opportunities in nearest future cycle, got %d", len(got))
	}
	if got[0].Symbol != "BTC" || got[1].Symbol != "ETH" {
		t.Fatalf("expected to preserve original order for nearest cycle, got %#v", got)
	}
}

func TestFilterOpportunitiesToCurrentSettlementCycle_DropsPastCycles(t *testing.T) {
	now := time.Date(2026, 3, 24, 18, 40, 0, 0, time.UTC)
	items := []entity.Opportunity{
		{Symbol: "BTC", ProjectedFundingTimeMs: now.Add(-10 * time.Minute).UnixMilli()},
		{Symbol: "ETH", EarliestFundingTimeMs: now.Add(-5 * time.Minute).UnixMilli()},
	}

	got := filterOpportunitiesToCurrentSettlementCycle(now, items)
	if len(got) != 0 {
		t.Fatalf("expected no opportunities when every cycle is already in the past, got %d", len(got))
	}
}

func TestOpportunityQueryService_ListLatestAppliesCurrentCycleFilterAndLimit(t *testing.T) {
	now := time.Now()
	svc := NewOpportunityQueryService(stubOpportunityRepo{
		items: []entity.Opportunity{
			{Symbol: "BTC", ProjectedFundingTimeMs: now.Add(10 * time.Minute).UnixMilli()},
			{Symbol: "ETH", ProjectedFundingTimeMs: now.Add(10 * time.Minute).UnixMilli()},
			{Symbol: "SOL", ProjectedFundingTimeMs: now.Add(30 * time.Minute).UnixMilli()},
		},
	})

	got, err := svc.ListLatest(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected limit to apply after cycle filter, got %d items", len(got))
	}
	if got[0].Symbol != "BTC" {
		t.Fatalf("expected first nearest-cycle item to survive limit, got %s", got[0].Symbol)
	}
}
