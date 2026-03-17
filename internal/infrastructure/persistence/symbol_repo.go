package persistence

import (
	"context"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"gorm.io/gorm/clause"
)

type SymbolRepo struct {
	client *db.Client
}

func NewSymbolRepository(client *db.Client) repository.SymbolRepository {
	return &SymbolRepo{client: client}
}

func (r *SymbolRepo) UpsertBatch(ctx context.Context, symbols []entity.Symbol) error {
	if len(symbols) == 0 {
		return nil
	}
	return r.client.GetDB(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "exchange"}, {Name: "symbol"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"venue_symbol", "base_asset", "quote_asset", "settle_asset", "status", "contract_type",
				"tick_size", "step_size", "min_qty", "min_notional", "funding_interval_hours",
				"venue_asset_id", "extra_meta_json", "enabled", "watched", "updated_at",
			}),
		}).Create(&symbols).Error
}

func (r *SymbolRepo) List(ctx context.Context) ([]entity.Symbol, error) {
	var out []entity.Symbol
	err := r.client.GetDB(ctx).Order("symbol asc, exchange asc").Find(&out).Error
	return out, err
}

func (r *SymbolRepo) ListWatched(ctx context.Context) ([]entity.Symbol, error) {
	var out []entity.Symbol
	err := r.client.GetDB(ctx).Where("watched = ?", true).Order("symbol asc, exchange asc").Find(&out).Error
	return out, err
}
