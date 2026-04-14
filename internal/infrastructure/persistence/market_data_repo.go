package persistence

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"gorm.io/gorm/clause"
)

type MarketDataRepo struct {
	client *db.Client
}

func NewMarketDataRepository(client *db.Client) repository.MarketDataRepository {
	return &MarketDataRepo{client: client}
}

func (r *MarketDataRepo) SaveFundingSnapshots(ctx context.Context, items []entity.FundingSnapshot) error {
	if len(items) == 0 {
		return nil
	}
	return r.client.GetDB(ctx).Create(&items).Error
}

func (r *MarketDataRepo) SaveBookTopSnapshots(ctx context.Context, items []entity.BookTopSnapshot) error {
	if len(items) == 0 {
		return nil
	}
	return r.client.GetDB(ctx).Create(&items).Error
}

func (r *MarketDataRepo) SaveFundingRateHistory(ctx context.Context, items []entity.FundingRateHistory) error {
	if len(items) == 0 {
		return nil
	}
	return r.client.GetDB(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "exchange"}, {Name: "symbol"}, {Name: "funding_time_ms"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"venue_symbol", "funding_rate", "mark_price", "source", "updated_at",
			}),
		}).Create(&items).Error
}

func (r *MarketDataRepo) RecentFundingSnapshots(ctx context.Context, exchangeName, symbol string, since time.Time, limit int) ([]entity.FundingSnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	var items []entity.FundingSnapshot
	err := r.client.GetDB(ctx).
		Where("exchange = ? AND symbol = ? AND event_time_ms >= ?", exchangeName, symbol, since.UnixMilli()).
		Order("event_time_ms desc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *MarketDataRepo) RecentFundingRateHistory(ctx context.Context, exchangeName, symbol string, since time.Time, limit int) ([]entity.FundingRateHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	var items []entity.FundingRateHistory
	err := r.client.GetDB(ctx).
		Where("exchange = ? AND symbol = ? AND funding_time_ms >= ?", exchangeName, symbol, since.UnixMilli()).
		Order("funding_time_ms desc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *MarketDataRepo) DeleteOldFundingSnapshots(ctx context.Context, cutoff time.Time) error {
	return r.client.GetDB(ctx).
		Where("created_at < ?", cutoff).
		Delete(&entity.FundingSnapshot{}).Error
}

func (r *MarketDataRepo) DeleteOldFundingRateHistory(ctx context.Context, cutoff time.Time) error {
	return r.client.GetDB(ctx).
		Where("funding_time_ms < ?", cutoff.UnixMilli()).
		Delete(&entity.FundingRateHistory{}).Error
}

func (r *MarketDataRepo) DeleteOldBookTopSnapshots(ctx context.Context, cutoff time.Time) error {
	return r.client.GetDB(ctx).
		Where("created_at < ?", cutoff).
		Delete(&entity.BookTopSnapshot{}).Error
}

func (r *MarketDataRepo) CountSnapshotStats(ctx context.Context, since time.Time) (repository.SnapshotStats, error) {
	db := r.client.GetDB(ctx)

	var fundingCount int64
	if err := db.Model(&entity.FundingSnapshot{}).
		Where("created_at >= ?", since).
		Count(&fundingCount).Error; err != nil {
		return repository.SnapshotStats{}, err
	}

	var bookTopCount int64
	if err := db.Model(&entity.BookTopSnapshot{}).
		Where("created_at >= ?", since).
		Count(&bookTopCount).Error; err != nil {
		return repository.SnapshotStats{}, err
	}

	return repository.SnapshotStats{
		FundingCount24h: fundingCount,
		BookTopCount24h: bookTopCount,
	}, nil
}
