package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

type OpportunityRepository interface {
	SaveBatch(ctx context.Context, batchID string, items []entity.Opportunity) error
	ListLatest(ctx context.Context, limit int) ([]entity.Opportunity, error)
}
