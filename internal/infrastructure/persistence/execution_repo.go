package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"gorm.io/gorm"
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
			"batch_id", "opportunity_batch_id", "rolling_group_key", "symbol", "long_exchange", "short_exchange",
			"arbitrage_mode", "strategy_mode", "status", "live_trading", "auto_close", "allocated_notional_usdt", "target_close_time_ms",
			"next_review_time_ms", "current_sync_boundary_ms", "opened_at_ms", "closed_at_ms", "review_count",
			"last_review_at_ms", "last_review_reason", "predecessor_plan_key", "successor_plan_key",
			"last_transition_at_ms", "last_transition_event", "status_reason",
			"last_error", "open_order_count", "close_order_count", "updated_at",
		}),
	}).Create(item).Error
}

func (r *ExecutionRepo) TryClaimAction(ctx context.Context, item *entity.ExecutionRecord, allowedCurrentStatuses []string) (*entity.ExecutionRecord, bool, error) {
	if item == nil {
		return nil, false, fmt.Errorf("nil execution record")
	}

	allowed := make(map[string]struct{}, len(allowedCurrentStatuses))
	for _, status := range allowedCurrentStatuses {
		allowed[normalizeExecutionRepoStatus(status)] = struct{}{}
	}

	var (
		current *entity.ExecutionRecord
		claimed bool
	)
	err := r.client.WithTx(ctx, func(ctx context.Context) error {
		dbh := r.client.GetDB(ctx)
		var existing entity.ExecutionRecord
		err := dbh.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("plan_key = ?", item.PlanKey).
			First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if _, ok := allowed[""]; !ok {
				return nil
			}
			res := dbh.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "plan_key"}},
				DoNothing: true,
			}).Create(item)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				cp := *item
				current = &cp
				claimed = true
				return nil
			}
			if err := dbh.Where("plan_key = ?", item.PlanKey).First(&existing).Error; err != nil {
				return err
			}
			cp := existing
			current = &cp
			return nil
		case err != nil:
			return err
		}

		cp := existing
		current = &cp
		if _, ok := allowed[normalizeExecutionRepoStatus(existing.Status)]; !ok {
			return nil
		}

		res := dbh.Model(&entity.ExecutionRecord{}).
			Where("id = ? AND status = ?", existing.ID, existing.Status).
			Updates(executionRecordAssignments(item))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := dbh.Where("id = ?", existing.ID).First(&existing).Error; err != nil {
				return err
			}
			cp := existing
			current = &cp
			return nil
		}
		if err := dbh.Where("id = ?", existing.ID).First(&existing).Error; err != nil {
			return err
		}
		cp = existing
		current = &cp
		claimed = true
		return nil
	})
	return current, claimed, err
}

func (r *ExecutionRepo) FindByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error) {
	var out entity.ExecutionRecord
	if err := r.client.GetDB(ctx).Where("plan_key = ?", planKey).First(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
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

func (r *ExecutionRepo) ListActiveLive(ctx context.Context) ([]entity.ExecutionRecord, error) {
	var out []entity.ExecutionRecord
	err := r.client.GetDB(ctx).
		Where("live_trading = ?", true).
		Where("status IN ?", []string{
			"pending_open",
			"opened",
			"open_partial_failed",
			"open_hedging",
			"pending_close",
			"close_partial_failed",
			"close_failed",
			"close_hedging",
		}).
		Order("updated_at desc").
		Find(&out).Error
	return out, err
}

func executionRecordAssignments(item *entity.ExecutionRecord) map[string]any {
	return map[string]any{
		"batch_id":                 item.BatchID,
		"opportunity_batch_id":     item.OpportunityBatchID,
		"rolling_group_key":        item.RollingGroupKey,
		"symbol":                   item.Symbol,
		"long_exchange":            item.LongExchange,
		"short_exchange":           item.ShortExchange,
		"arbitrage_mode":           item.ArbitrageMode,
		"strategy_mode":            item.StrategyMode,
		"status":                   item.Status,
		"live_trading":             item.LiveTrading,
		"auto_close":               item.AutoClose,
		"allocated_notional_usdt":  item.AllocatedNotionalUSDT,
		"target_close_time_ms":     item.TargetCloseTimeMs,
		"next_review_time_ms":      item.NextReviewTimeMs,
		"current_sync_boundary_ms": item.CurrentSyncBoundaryMs,
		"opened_at_ms":             item.OpenedAtMs,
		"closed_at_ms":             item.ClosedAtMs,
		"review_count":             item.ReviewCount,
		"last_review_at_ms":        item.LastReviewAtMs,
		"last_review_reason":       item.LastReviewReason,
		"predecessor_plan_key":     item.PredecessorPlanKey,
		"successor_plan_key":       item.SuccessorPlanKey,
		"last_transition_at_ms":    item.LastTransitionAtMs,
		"last_transition_event":    item.LastTransitionEvent,
		"status_reason":            item.StatusReason,
		"last_error":               item.LastError,
		"open_order_count":         item.OpenOrderCount,
		"close_order_count":        item.CloseOrderCount,
	}
}

func normalizeExecutionRepoStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}
