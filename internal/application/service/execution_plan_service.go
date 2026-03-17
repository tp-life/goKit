package service

import (
	"context"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type ExecutionPlanService struct {
	repo repository.ExecutionPlanRepository
}

func NewExecutionPlanService(repo repository.ExecutionPlanRepository) *ExecutionPlanService {
	return &ExecutionPlanService{repo: repo}
}

func (s *ExecutionPlanService) ListLatest(ctx context.Context, limit int) ([]entity.ExecutionPlan, error) {
	return s.repo.ListLatest(ctx, limit)
}

func (s *ExecutionPlanService) ListByOpportunityBatch(ctx context.Context, opportunityBatchID string, limit int) ([]entity.ExecutionPlan, error) {
	return s.repo.ListByOpportunityBatch(ctx, opportunityBatchID, limit)
}
