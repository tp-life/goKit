package service

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type OpportunityQueryService struct {
	repo repository.OpportunityRepository
}

func NewOpportunityQueryService(repo repository.OpportunityRepository) *OpportunityQueryService {
	return &OpportunityQueryService{repo: repo}
}

func (s *OpportunityQueryService) ListLatest(ctx context.Context, limit int) ([]entity.Opportunity, error) {
	items, err := s.repo.ListLatest(ctx, limit)
	if err != nil {
		return nil, err
	}
	items = filterOpportunitiesToCurrentSettlementCycle(time.Now(), items)
	if limit <= 0 {
		limit = 20
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *OpportunityQueryService) ListLatestSummary(ctx context.Context, limit int) ([]repository.OpportunitySummary, error) {
	// Summary consumers rely on local filtering/search across the whole current
	// settlement cycle. Fetch the latest batch summaries first, then apply the
	// cycle filter and client-visible limit afterwards.
	items, err := s.repo.ListLatestSummary(ctx, 0)
	if err != nil {
		return nil, err
	}
	items = filterOpportunitySummariesToCurrentSettlementCycle(time.Now(), items)
	if limit <= 0 {
		limit = 20
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *OpportunityQueryService) GetByID(ctx context.Context, id uint) (*entity.Opportunity, error) {
	return s.repo.FindByID(ctx, id)
}

func filterOpportunitiesToCurrentSettlementCycle(now time.Time, items []entity.Opportunity) []entity.Opportunity {
	if len(items) == 0 {
		return []entity.Opportunity{}
	}
	targetCycleMs := selectTargetSettlementCycleMs(now.UnixMilli(), func(yield func(int64) bool) {
		for _, item := range items {
			if !yield(opportunitySettlementCycleMs(item)) {
				return
			}
		}
	})
	if targetCycleMs == 0 {
		return []entity.Opportunity{}
	}
	out := make([]entity.Opportunity, 0, len(items))
	for _, item := range items {
		if opportunitySettlementCycleMs(item) == targetCycleMs {
			out = append(out, item)
		}
	}
	return out
}

func filterOpportunitySummariesToCurrentSettlementCycle(now time.Time, items []repository.OpportunitySummary) []repository.OpportunitySummary {
	if len(items) == 0 {
		return []repository.OpportunitySummary{}
	}
	targetCycleMs := selectTargetSettlementCycleMs(now.UnixMilli(), func(yield func(int64) bool) {
		for _, item := range items {
			if !yield(opportunitySummarySettlementCycleMs(item)) {
				return
			}
		}
	})
	if targetCycleMs == 0 {
		return []repository.OpportunitySummary{}
	}
	out := make([]repository.OpportunitySummary, 0, len(items))
	for _, item := range items {
		if opportunitySummarySettlementCycleMs(item) == targetCycleMs {
			out = append(out, item)
		}
	}
	return out
}

func selectTargetSettlementCycleMs(nowMs int64, iter func(func(int64) bool)) int64 {
	nearestFutureMs := int64(0)
	latestPastMs := int64(0)
	iter(func(cycleMs int64) bool {
		if cycleMs <= 0 {
			return true
		}
		if cycleMs >= nowMs {
			if nearestFutureMs == 0 || cycleMs < nearestFutureMs {
				nearestFutureMs = cycleMs
			}
			return true
		}
		if cycleMs > latestPastMs {
			latestPastMs = cycleMs
		}
		return true
	})
	if nearestFutureMs > 0 {
		return nearestFutureMs
	}
	return latestPastMs
}

func opportunitySettlementCycleMs(item entity.Opportunity) int64 {
	for _, candidate := range []int64{
		item.ProjectedFundingTimeMs,
		item.EarliestFundingTimeMs,
		item.LatestFundingTimeMs,
	} {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}

func opportunitySummarySettlementCycleMs(item repository.OpportunitySummary) int64 {
	for _, candidate := range []int64{
		item.ProjectedFundingTimeMs,
		item.EarliestFundingTimeMs,
		item.LatestFundingTimeMs,
	} {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}
