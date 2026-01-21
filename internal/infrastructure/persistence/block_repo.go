package persistence

import (
	"context"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type BlockRepo struct {
	client *db.Client
}

func NewBlockRepo(client *db.Client) repository.BlockRepository {
	return &BlockRepo{client: client}
}

func (r *BlockRepo) Create(ctx context.Context, block *entity.Block) error {
	return r.client.GetDB(ctx).Create(block).Error
}

func (r *BlockRepo) CreateBatch(ctx context.Context, blocks []*entity.Block) error {
	if len(blocks) == 0 {
		return nil
	}
	return r.client.GetDB(ctx).CreateInBatches(blocks, 100).Error
}

func (r *BlockRepo) DeleteByPageID(ctx context.Context, pageID uint64) error {
	// GORM 会自动使用软删除（因为 Block 有 DeletedAt 字段）
	return r.client.GetDB(ctx).Where("page_id = ?", pageID).Delete(&entity.Block{}).Error
}

func (r *BlockRepo) PermanentlyDeleteByPageID(ctx context.Context, pageID uint64) error {
	// 物理删除，用于更新页面时清除旧 blocks
	return r.client.GetDB(ctx).Unscoped().Where("page_id = ?", pageID).Delete(&entity.Block{}).Error
}

func (r *BlockRepo) FindByPageID(ctx context.Context, pageID uint64) ([]*entity.Block, error) {
	var blocks []*entity.Block
	err := r.client.GetDB(ctx).
		Where("page_id = ?", pageID).
		Order("sort_order ASC").
		Find(&blocks).Error
	return blocks, err
}
