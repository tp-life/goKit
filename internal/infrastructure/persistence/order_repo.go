package persistence

import (
	"context"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type OrderRepo struct {
	client *db.Client
}

func NewOrderRepository(client *db.Client) repository.OrderRepository {
	return &OrderRepo{client: client}
}

func (r *OrderRepo) Create(ctx context.Context, item *entity.OrderRecord) error {
	return r.client.GetDB(ctx).Create(item).Error
}

func (r *OrderRepo) Update(ctx context.Context, item *entity.OrderRecord) error {
	return r.client.GetDB(ctx).Save(item).Error
}

func (r *OrderRepo) ListByPlanKey(ctx context.Context, planKey string) ([]entity.OrderRecord, error) {
	var out []entity.OrderRecord
	err := r.client.GetDB(ctx).Where("plan_key = ?", planKey).Order("created_at asc").Find(&out).Error
	return out, err
}

func (r *OrderRepo) ListLatest(ctx context.Context, limit int) ([]entity.OrderRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []entity.OrderRecord
	err := r.client.GetDB(ctx).Order("created_at desc").Limit(limit).Find(&out).Error
	return out, err
}
