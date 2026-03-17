package persistence

import (
	"context"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"gorm.io/gorm/clause"
)

type ExecutionRepo struct {
	client *db.Client
}

func NewExecutionRepository(client *db.Client) repository.ExecutionRepository {
	return &ExecutionRepo{client: client}
}

func (r *ExecutionRepo) Upsert(ctx context.Context, item *entity.ExecutionRecord) error {
	return r.client.GetDB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "plan_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"batch_id", "opportunity_batch_id", "symbol", "long_exchange", "short_exchange",
			"status", "live_trading", "auto_close", "target_close_time_ms", "opened_at_ms",
			"closed_at_ms", "last_error", "open_order_count", "close_order_count", "updated_at",
		}),
	}).Create(item).Error
}

func (r *ExecutionRepo) FindByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error) {
	var out entity.ExecutionRecord
	if err := r.client.GetDB(ctx).Where("plan_key = ?", planKey).First(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *ExecutionRepo) ListLatest(ctx context.Context, limit int) ([]entity.ExecutionRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []entity.ExecutionRecord
	err := r.client.GetDB(ctx).Order("updated_at desc").Limit(limit).Find(&out).Error
	return out, err
}
