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

func filterOpportunitiesToCurrentSettlementCycle(now time.Time, items []entity.Opportunity) []entity.Opportunity {
	if len(items) == 0 {
		return []entity.Opportunity{}
	}
	nowMs := now.UnixMilli()
	currentCycleMs := int64(0)
	for _, item := range items {
		cycleMs := opportunitySettlementCycleMs(item)
		if cycleMs <= 0 || cycleMs < nowMs {
			continue
		}
		if currentCycleMs == 0 || cycleMs < currentCycleMs {
			currentCycleMs = cycleMs
		}
	}
	if currentCycleMs == 0 {
		return []entity.Opportunity{}
	}
	out := make([]entity.Opportunity, 0, len(items))
	for _, item := range items {
		if opportunitySettlementCycleMs(item) == currentCycleMs {
			out = append(out, item)
		}
	}
	return out
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
