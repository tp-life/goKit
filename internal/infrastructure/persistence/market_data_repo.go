package persistence

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
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

func (r *MarketDataRepo) DeleteOldFundingSnapshots(ctx context.Context, cutoff time.Time) error {
	return r.client.GetDB(ctx).
		Where("created_at < ?", cutoff).
		Delete(&entity.FundingSnapshot{}).Error
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
