package repository

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
)

type SnapshotStats struct {
	FundingCount24h int64 `json:"funding_count_24h"`
	BookTopCount24h int64 `json:"book_top_count_24h"`
}

type MarketDataRepository interface {
	SaveFundingSnapshots(ctx context.Context, items []entity.FundingSnapshot) error
	SaveBookTopSnapshots(ctx context.Context, items []entity.BookTopSnapshot) error

	// DeleteOldFundingSnapshots 删除保留期之外的 funding 快照。
	DeleteOldFundingSnapshots(ctx context.Context, cutoff time.Time) error

	// DeleteOldBookTopSnapshots 删除保留期之外的盘口快照。
	DeleteOldBookTopSnapshots(ctx context.Context, cutoff time.Time) error

	// CountSnapshotStats 统计最近一段时间内两张快照表的行数。
	CountSnapshotStats(ctx context.Context, since time.Time) (SnapshotStats, error)
}
