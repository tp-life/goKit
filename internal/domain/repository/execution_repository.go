package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

type ExecutionRepository interface {
	Upsert(ctx context.Context, item *entity.ExecutionRecord) error
	FindByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error)
	ListLatest(ctx context.Context, limit int) ([]entity.ExecutionRecord, error)
}
