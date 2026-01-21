package repository

import (
	"context"
	"goKit/internal/domain/entity"
)

// TrashRepository 回收站仓库接口
type TrashRepository interface {
	// FindDeletedMemos 查询已删除的 Memos
	FindDeletedMemos(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Memo, error)
	// FindDeletedPages 查询已删除的 Pages
	FindDeletedPages(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Page, error)
	// RestoreMemo 恢复 Memo
	RestoreMemo(ctx context.Context, id uint64) error
	// RestorePage 恢复 Page
	RestorePage(ctx context.Context, id uint64) error
	// PermanentlyDeleteMemo 永久删除 Memo
	PermanentlyDeleteMemo(ctx context.Context, id uint64) error
	// PermanentlyDeletePage 永久删除 Page
	PermanentlyDeletePage(ctx context.Context, id uint64) error
}

// SearchRepository 搜索仓库接口
type SearchRepository interface {
	// SearchMemos 搜索 Memos（全文检索）
	SearchMemos(ctx context.Context, userID uint64, query string, limit, offset int) ([]*entity.Memo, error)
	// SearchPages 搜索 Pages（通过 Blocks 关联）
	SearchPages(ctx context.Context, userID uint64, query string, limit, offset int) ([]*SearchPageResult, error)
}

// SearchPageResult 搜索结果中的 Page 信息
type SearchPageResult struct {
	Page        *entity.Page
	HitFragment string // 命中的文本片段
}
