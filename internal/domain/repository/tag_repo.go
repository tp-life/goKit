package repository

import (
	"context"
	"goKit/internal/domain/entity"
)

type TagRepository interface {
	// Create 创建标签
	Create(ctx context.Context, tag *entity.Tag) error
	// FindOrCreate 查找或创建标签（单个）
	FindOrCreate(ctx context.Context, userID uint64, tagName string) (*entity.Tag, error)
	// FindOrCreateBatch 批量查找或创建标签
	FindOrCreateBatch(ctx context.Context, userID uint64, tagNames []string) ([]*entity.Tag, error)
	// FindByUserID 查询用户的所有标签
	FindByUserID(ctx context.Context, userID uint64) ([]*entity.Tag, error)
	// FindByName 根据名称查询标签
	FindByName(ctx context.Context, userID uint64, name string) (*entity.Tag, error)
	// FindByID 根据ID查询标签
	FindByID(ctx context.Context, id uint64) (*entity.Tag, error)
	// Delete 删除标签
	Delete(ctx context.Context, id uint64) error
	// FindMemosByTagID 根据标签ID查询 Memos
	FindMemosByTagID(ctx context.Context, tagID uint64, limit, offset int) ([]*entity.Memo, error)
	// FindPagesByTagID 根据标签ID查询 Pages
	FindPagesByTagID(ctx context.Context, tagID uint64, limit, offset int) ([]*entity.Page, error)
}
