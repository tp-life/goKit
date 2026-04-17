package persistence

import (
	"context"
	"errors"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"gorm.io/gorm"
)

type OpportunityRepo struct {
	client *db.Client
}

func NewOpportunityRepository(client *db.Client) repository.OpportunityRepository {
	return &OpportunityRepo{client: client}
}

func (r *OpportunityRepo) SaveBatch(ctx context.Context, batchID string, items []entity.Opportunity) error {
	if len(items) == 0 {
		return nil
	}
	return r.client.WithTx(ctx, func(ctx context.Context) error {
		for i := range items {
			items[i].BatchID = batchID
		}
		return r.client.GetDB(ctx).Create(&items).Error
	})
}

func (r *OpportunityRepo) ListLatest(ctx context.Context, limit int) ([]entity.Opportunity, error) {
	latestBatchID, err := r.latestBatchID(ctx)
	if err != nil {
		return []entity.Opportunity{}, nil
	}
	var out []entity.Opportunity
	query := r.client.GetDB(ctx).
		Where("batch_id = ?", latestBatchID).
		Order("score desc, net_expected_pnl desc, as_of_time_ms desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err = query.Find(&out).Error
	return out, err
}

func (r *OpportunityRepo) ListLatestSummary(ctx context.Context, limit int) ([]repository.OpportunitySummary, error) {
	latestBatchID, err := r.latestBatchID(ctx)
	if err != nil {
		return []repository.OpportunitySummary{}, nil
	}
	var out []repository.OpportunitySummary
	query := r.client.GetDB(ctx).
		Model(&entity.Opportunity{}).
		Where("batch_id = ?", latestBatchID).
		Order("score desc, net_expected_pnl desc, as_of_time_ms desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err = query.Find(&out).Error
	return out, err
}

func (r *OpportunityRepo) FindByID(ctx context.Context, id uint) (*entity.Opportunity, error) {
	if id == 0 {
		return nil, nil
	}
	var item entity.Opportunity
	if err := r.client.GetDB(ctx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *OpportunityRepo) DeleteOlderThan(ctx context.Context, cutoffMs int64, limit int) (int64, error) {
	if cutoffMs <= 0 {
		return 0, nil
	}
	if limit <= 0 {
		limit = 5000
	}

	dbh := r.client.GetDB(ctx)

	var latest entity.Opportunity
	if err := dbh.
		Model(&entity.Opportunity{}).
		Select("batch_id").
		Order("as_of_time_ms desc").
		Take(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}

	query := dbh.
		Model(&entity.Opportunity{}).
		Where("as_of_time_ms < ?", cutoffMs).
		Where("batch_id <> ?", latest.BatchID).
		Order("id asc").
		Limit(limit)

	var ids []uint
	if err := query.Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	result := dbh.Where("id IN ?", ids).Delete(&entity.Opportunity{})
	return result.RowsAffected, result.Error
}

func (r *OpportunityRepo) latestBatchID(ctx context.Context) (string, error) {
	row := r.client.GetDB(ctx).
		Model(&entity.Opportunity{}).
		Select("batch_id").
		Order("as_of_time_ms desc").
		Limit(1).
		Row()
	var batchID string
	if err := row.Scan(&batchID); err != nil {
		return "", err
	}
	return batchID, nil
}
