package service

import (
	"context"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type stubOpportunityRepo struct {
	items []entity.Opportunity
}

type limitAwareOpportunityRepo struct {
	stubOpportunityRepo
	summaryLimits []int
}

func (r stubOpportunityRepo) SaveBatch(_ context.Context, _ string, _ []entity.Opportunity) error {
	return nil
}

func (r stubOpportunityRepo) ListLatest(_ context.Context, _ int) ([]entity.Opportunity, error) {
	return r.items, nil
}

func (r stubOpportunityRepo) ListLatestSummary(_ context.Context, _ int) ([]repository.OpportunitySummary, error) {
	return summariesFromOpportunities(r.items), nil
}

func (r *limitAwareOpportunityRepo) ListLatestSummary(_ context.Context, limit int) ([]repository.OpportunitySummary, error) {
	r.summaryLimits = append(r.summaryLimits, limit)
	out := summariesFromOpportunities(r.items)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func summariesFromOpportunities(items []entity.Opportunity) []repository.OpportunitySummary {
	out := make([]repository.OpportunitySummary, 0, len(items))
	for _, item := range items {
		out = append(out, repository.OpportunitySummary{
			ID:                        item.ID,
			BatchID:                   item.BatchID,
			AsOfTimeMs:                item.AsOfTimeMs,
			Symbol:                    item.Symbol,
			LongExchange:              item.LongExchange,
			ShortExchange:             item.ShortExchange,
			LongVenueSymbol:           item.LongVenueSymbol,
			ShortVenueSymbol:          item.ShortVenueSymbol,
			ProjectedFundingTimeMs:    item.ProjectedFundingTimeMs,
			EarliestFundingTimeMs:     item.EarliestFundingTimeMs,
			LatestFundingTimeMs:       item.LatestFundingTimeMs,
			NetExpectedPNL:            item.NetExpectedPNL,
			GrossEdgeHourly:           item.GrossEdgeHourly,
			Status:                    item.Status,
			RejectReason:              item.RejectReason,
			EligibleForExecution:      item.EligibleForExecution,
			CreatedAt:                 item.CreatedAt,
			UpdatedAt:                 item.UpdatedAt,
			FundingWindowHours:        item.FundingWindowHours,
			FundingComputationMode:    item.FundingComputationMode,
			LongFundingRate:           item.LongFundingRate,
			ShortFundingRate:          item.ShortFundingRate,
			LongFundingTimeMs:         item.LongFundingTimeMs,
			ShortFundingTimeMs:        item.ShortFundingTimeMs,
			LongFundingIntervalHours:  item.LongFundingIntervalHours,
			ShortFundingIntervalHours: item.ShortFundingIntervalHours,
		})
	}
	return out
}

func (r stubOpportunityRepo) FindByID(_ context.Context, id uint) (*entity.Opportunity, error) {
	for i := range r.items {
		if r.items[i].ID == id {
			item := r.items[i]
			return &item, nil
		}
	}
	return nil, nil
}

func (r stubOpportunityRepo) DeleteOlderThan(_ context.Context, _ int64, _ int) (int64, error) {
	return 0, nil
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

func TestFilterOpportunitiesToCurrentSettlementCycle_FallsBackToLatestPastCycle(t *testing.T) {
	now := time.Date(2026, 3, 24, 18, 40, 0, 0, time.UTC)
	items := []entity.Opportunity{
		{Symbol: "BTC", ProjectedFundingTimeMs: now.Add(-5 * time.Minute).UnixMilli()},
		{Symbol: "ETH", EarliestFundingTimeMs: now.Add(-5 * time.Minute).UnixMilli()},
		{Symbol: "SOL", LatestFundingTimeMs: now.Add(-25 * time.Minute).UnixMilli()},
	}

	got := filterOpportunitiesToCurrentSettlementCycle(now, items)
	if len(got) != 2 {
		t.Fatalf("expected latest past cycle to remain visible, got %d items", len(got))
	}
	if got[0].Symbol != "BTC" || got[1].Symbol != "ETH" {
		t.Fatalf("expected latest past-cycle items in original order, got %#v", got)
	}
}

func TestFilterOpportunitiesToCurrentSettlementCycle_DropsItemsWithoutCycle(t *testing.T) {
	now := time.Date(2026, 3, 24, 18, 40, 0, 0, time.UTC)
	items := []entity.Opportunity{{Symbol: "BTC"}, {Symbol: "ETH"}}

	got := filterOpportunitiesToCurrentSettlementCycle(now, items)
	if len(got) != 0 {
		t.Fatalf("expected no opportunities when cycle time is missing, got %d", len(got))
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

func TestOpportunityQueryService_ListLatestSummaryFetchesWholeBatchBeforeLimit(t *testing.T) {
	now := time.Now()
	repo := &limitAwareOpportunityRepo{
		stubOpportunityRepo: stubOpportunityRepo{
			items: []entity.Opportunity{
				{Symbol: "SOL", ProjectedFundingTimeMs: now.Add(30 * time.Minute).UnixMilli()},
				{Symbol: "BTC", ProjectedFundingTimeMs: now.Add(10 * time.Minute).UnixMilli()},
				{Symbol: "ETH", ProjectedFundingTimeMs: now.Add(10 * time.Minute).UnixMilli()},
			},
		},
	}
	svc := NewOpportunityQueryService(repo)

	got, err := svc.ListLatestSummary(context.Background(), 2)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(repo.summaryLimits) != 1 || repo.summaryLimits[0] != 0 {
		t.Fatalf("expected summary repo to be called without pre-limit, got %#v", repo.summaryLimits)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit to apply after cycle filter, got %d items", len(got))
	}
	if got[0].Symbol != "BTC" || got[1].Symbol != "ETH" {
		t.Fatalf("expected nearest-cycle summaries to survive, got %#v", got)
	}
}
