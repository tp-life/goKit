package persistence

import (
	"context"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
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

func (r *OpportunityRepo) latestBatchID(ctx context.Context) (string, error) {
	var latest entity.Opportunity
	if err := r.client.GetDB(ctx).Order("as_of_time_ms desc").First(&latest).Error; err != nil {
		return "", err
	}
	return latest.BatchID, nil
}
