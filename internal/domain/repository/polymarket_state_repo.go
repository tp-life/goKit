package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

// PolymarketStateRepository 定义 Polymarket 状态持久化需要的仓储能力。
type PolymarketStateRepository interface {
	Load(ctx context.Context) (*entity.PolymarketState, error)
	Save(ctx context.Context, state *entity.PolymarketState) error
}
