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
	if limit <= 0 {
		limit = 20
	}
	var latest entity.Opportunity
	if err := r.client.GetDB(ctx).Order("as_of_time_ms desc").First(&latest).Error; err != nil {
		return []entity.Opportunity{}, nil
	}
	var out []entity.Opportunity
	err := r.client.GetDB(ctx).
		Where("batch_id = ?", latest.BatchID).
		Order("score desc, net_expected_pnl desc").
		Limit(limit).
		Find(&out).Error
	return out, err
}
