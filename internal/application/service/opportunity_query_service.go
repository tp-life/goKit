package service

import (
	"context"

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
	return s.repo.ListLatest(ctx, limit)
}
