package service

import (
	"context"
	"time"

	"goKit/internal/domain/repository"
)

// SnapshotStatsService 提供快照统计查询，方便直接观察当前快照写入压力。
type SnapshotStatsService struct {
	marketRepo repository.MarketDataRepository
}

func NewSnapshotStatsService(marketRepo repository.MarketDataRepository) *SnapshotStatsService {
	return &SnapshotStatsService{
		marketRepo: marketRepo,
	}
}

// Get24hStats 返回最近 24 小时的快照统计。
func (s *SnapshotStatsService) Get24hStats(ctx context.Context) (repository.SnapshotStats, error) {
	since := time.Now().Add(-24 * time.Hour)
	return s.marketRepo.CountSnapshotStats(ctx, since)
}
