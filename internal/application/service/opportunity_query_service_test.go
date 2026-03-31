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
	listLimits    []int
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

func (r *limitAwareOpportunityRepo) ListLatest(_ context.Context, limit int) ([]entity.Opportunity, error) {
	r.listLimits = append(r.listLimits, limit)
	out := append([]entity.Opportunity(nil), r.items...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
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

func TestApplyOpportunityLimit_DefaultAndExplicitLimit(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}

	gotDefault := applyOpportunityLimit(items, 0)
	if len(gotDefault) != 5 {
		t.Fatalf("expected default limit to keep all 5 items, got %d", len(gotDefault))
	}

	gotLimited := applyOpportunityLimit(items, 3)
	if len(gotLimited) != 3 {
		t.Fatalf("expected explicit limit to keep 3 items, got %d", len(gotLimited))
	}
}

func TestOpportunityQueryService_ListLatestFetchesWholeLatestBatchBeforeLimit(t *testing.T) {
	now := time.Now()
	repo := &limitAwareOpportunityRepo{
		stubOpportunityRepo: stubOpportunityRepo{
			items: []entity.Opportunity{
				{Symbol: "SOL", ProjectedFundingTimeMs: now.Add(30 * time.Minute).UnixMilli()},
				{Symbol: "BTC", ProjectedFundingTimeMs: now.Add(10 * time.Minute).UnixMilli()},
				{Symbol: "ETH", ProjectedFundingTimeMs: now.Add(50 * time.Minute).UnixMilli()},
			},
		},
	}
	svc := NewOpportunityQueryService(repo, Config{
		OpportunityCalcInterval: 5 * time.Second,
		MaxDataAge:              15 * time.Second,
	})

	got, err := svc.ListLatest(context.Background(), 2)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(repo.listLimits) != 1 || repo.listLimits[0] != 0 {
		t.Fatalf("expected repo ListLatest to be called without pre-limit, got %#v", repo.listLimits)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit to apply after full-batch fetch, got %d items", len(got))
	}
	if got[0].Symbol != "SOL" || got[1].Symbol != "BTC" {
		t.Fatalf("expected service to preserve latest batch order before limit, got %#v", got)
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
	svc := NewOpportunityQueryService(repo, Config{
		OpportunityCalcInterval: 5 * time.Second,
		MaxDataAge:              15 * time.Second,
	})

	got, err := svc.ListLatestSummary(context.Background(), 2)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(repo.summaryLimits) != 1 || repo.summaryLimits[0] != 0 {
		t.Fatalf("expected summary repo to be called without pre-limit, got %#v", repo.summaryLimits)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit to apply after full-batch fetch, got %d items", len(got))
	}
	if got[0].Symbol != "SOL" || got[1].Symbol != "BTC" {
		t.Fatalf("expected service to preserve latest batch order before limit, got %#v", got)
	}
}

func TestOpportunityQueryService_ListLatestReturnsEmptyWhenBatchIsStale(t *testing.T) {
	now := time.Now().UTC()
	repo := stubOpportunityRepo{
		items: []entity.Opportunity{
			{
				ID:         1,
				BatchID:    "old-batch",
				AsOfTimeMs: now.Add(-35 * time.Second).UnixMilli(),
				Symbol:     "BTC",
			},
		},
	}
	svc := NewOpportunityQueryService(repo, Config{
		OpportunityCalcInterval: 5 * time.Second,
		MaxDataAge:              15 * time.Second,
	})

	got, err := svc.ListLatest(context.Background(), 20)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected stale batch to be suppressed, got %#v", got)
	}
}

func TestOpportunityQueryService_ListLatestSummaryReturnsEmptyWhenBatchIsStale(t *testing.T) {
	now := time.Now().UTC()
	repo := stubOpportunityRepo{
		items: []entity.Opportunity{
			{
				ID:         1,
				BatchID:    "old-batch",
				AsOfTimeMs: now.Add(-35 * time.Second).UnixMilli(),
				Symbol:     "BTC",
			},
		},
	}
	svc := NewOpportunityQueryService(repo, Config{
		OpportunityCalcInterval: 5 * time.Second,
		MaxDataAge:              15 * time.Second,
	})

	got, err := svc.ListLatestSummary(context.Background(), 20)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected stale summary batch to be suppressed, got %#v", got)
	}
}
