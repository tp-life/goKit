package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

type ExecutionPlanRepository interface {
	SaveBatch(ctx context.Context, batchID, opportunityBatchID string, items []entity.ExecutionPlan) error
	ListLatest(ctx context.Context, limit int) ([]entity.ExecutionPlan, error)
	ListByOpportunityBatch(ctx context.Context, opportunityBatchID string, limit int) ([]entity.ExecutionPlan, error)
	FindByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionPlan, error)
}
