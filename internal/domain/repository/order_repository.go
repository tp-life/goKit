package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

type OrderRepository interface {
	Create(ctx context.Context, item *entity.OrderRecord) error
	ListByPlanKey(ctx context.Context, planKey string) ([]entity.OrderRecord, error)
	ListLatest(ctx context.Context, limit int) ([]entity.OrderRecord, error)
}
