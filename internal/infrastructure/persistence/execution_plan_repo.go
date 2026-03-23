package persistence

import (
	"context"
	"errors"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ExecutionPlanRepo struct {
	client *db.Client
}

func NewExecutionPlanRepository(client *db.Client) repository.ExecutionPlanRepository {
	return &ExecutionPlanRepo{client: client}
}

func (r *ExecutionPlanRepo) SaveBatch(ctx context.Context, batchID, opportunityBatchID string, items []entity.ExecutionPlan) error {
	if len(items) == 0 {
		return nil
	}
	return r.client.WithTx(ctx, func(ctx context.Context) error {
		for i := range items {
			items[i].BatchID = batchID
			items[i].OpportunityBatchID = opportunityBatchID
		}
		return r.client.GetDB(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "plan_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"batch_id", "opportunity_batch_id", "symbol", "long_exchange", "short_exchange",
				"long_venue_symbol", "short_venue_symbol", "status", "entry_mode", "exit_mode",
				"target_leverage", "capital_allocated_usdt", "target_notional_usdt", "rounded_notional_usdt",
				"long_entry_price", "short_entry_price", "long_qty", "short_qty", "long_min_qty",
				"short_min_qty", "long_min_notional_usdt", "short_min_notional_usdt", "cross_venue_basis_bps",
				"funding_carry_pnl", "entry_fee_pnl", "exit_fee_pnl", "slippage_pnl", "safety_buffer_pnl",
				"entry_penalty_bps", "exit_penalty_bps", "hedge_penalty_bps", "execution_penalty_bps",
				"execution_penalty_model", "execution_penalty_bucket",
				"net_expected_pnl", "net_expected_pnl_bps", "score", "earliest_funding_time_ms",
				"latest_funding_time_ms", "projected_funding_time_ms", "required_entry_by_funding_time_ms",
				"long_funding_event_count", "short_funding_event_count", "funding_window_hours", "funding_computation_mode",
				"entry_window_open_ms", "entry_window_close_ms", "target_close_time_ms",
				"as_of_time_ms", "ready_now", "reject_reason", "updated_at",
			}),
		}).Create(&items).Error
	})
}

func (r *ExecutionPlanRepo) ListLatest(ctx context.Context, limit int) ([]entity.ExecutionPlan, error) {
	if limit <= 0 {
		limit = 20
	}
	var latest entity.ExecutionPlan
	if err := r.client.GetDB(ctx).Order("as_of_time_ms desc").First(&latest).Error; err != nil {
		return []entity.ExecutionPlan{}, nil
	}
	var out []entity.ExecutionPlan
	err := r.client.GetDB(ctx).
		Where("batch_id = ?", latest.BatchID).
		Order("ready_now desc, score desc, net_expected_pnl desc").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// ListByOpportunityBatch 用于把 plans 和当前机会批次强行对齐。
// 当前端已经知道 opportunities 的 batch_id 时，应该优先使用这个查询。
// 这样可以避免：
// - opportunities 是新一批
// - 但 plans 因为这一轮没有新计划，仍返回旧一批
func (r *ExecutionPlanRepo) ListByOpportunityBatch(ctx context.Context, opportunityBatchID string, limit int) ([]entity.ExecutionPlan, error) {
	if limit <= 0 {
		limit = 20
	}
	if opportunityBatchID == "" {
		return []entity.ExecutionPlan{}, nil
	}

	var out []entity.ExecutionPlan
	err := r.client.GetDB(ctx).
		Where("opportunity_batch_id = ?", opportunityBatchID).
		Order("ready_now desc, score desc, net_expected_pnl desc").
		Limit(limit).
		Find(&out).Error
	return out, err
}

func (r *ExecutionPlanRepo) FindByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionPlan, error) {
	var out entity.ExecutionPlan
	if err := r.client.GetDB(ctx).Where("plan_key = ?", planKey).Order("created_at desc").First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}
