package repository

import (
	"context"
	"goKit/internal/domain/entity"
)

type BlockRepository interface {
	Create(ctx context.Context, block *entity.Block) error
	CreateBatch(ctx context.Context, blocks []*entity.Block) error
	DeleteByPageID(ctx context.Context, pageID uint64) error
	PermanentlyDeleteByPageID(ctx context.Context, pageID uint64) error // 物理删除
	FindByPageID(ctx context.Context, pageID uint64) ([]*entity.Block, error)
}
