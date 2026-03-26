package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

type ExecutionRepository interface {
	Upsert(ctx context.Context, item *entity.ExecutionRecord) error
	TryClaimAction(ctx context.Context, item *entity.ExecutionRecord, allowedCurrentStatuses []string) (*entity.ExecutionRecord, bool, error)
	FindByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error)
	ListLatest(ctx context.Context, limit int) ([]entity.ExecutionRecord, error)
	ListActiveLive(ctx context.Context) ([]entity.ExecutionRecord, error)
}
