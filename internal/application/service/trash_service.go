package service

import (
	"context"
	"fmt"
	"time"

	"goKit/internal/domain/repository"
)

type TrashService struct {
	trashRepo repository.TrashRepository
}

func NewTrashService(trashRepo repository.TrashRepository) *TrashService {
	return &TrashService{
		trashRepo: trashRepo,
	}
}

// TrashItem 回收站项目
type TrashItem struct {
	ID        uint64    `json:"id"`
	Type      string    `json:"type"` // "memo" 或 "page"
	Title     string    `json:"title"` // Page 的标题，Memo 则为空
	Content   string    `json:"content"` // Memo 的内容，Page 则为空
	DeletedAt time.Time `json:"deleted_at"`
}

// GetTrashResponse 获取回收站列表响应
type GetTrashResponse struct {
	Items []TrashItem `json:"items"`
	Total int         `json:"total"`
}

// GetTrash 获取回收站列表
func (s *TrashService) GetTrash(ctx context.Context, userID uint64, limit, offset int) (*GetTrashResponse, error) {
	// 查询已删除的 Memos
	deletedMemos, err := s.trashRepo.FindDeletedMemos(ctx, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("find deleted memos: %w", err)
	}

	// 查询已删除的 Pages
	deletedPages, err := s.trashRepo.FindDeletedPages(ctx, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("find deleted pages: %w", err)
	}

	// 合并结果
	items := make([]TrashItem, 0, len(deletedMemos)+len(deletedPages))
	
	// 添加 Memos
	for _, memo := range deletedMemos {
		items = append(items, TrashItem{
			ID:        memo.ID,
			Type:      "memo",
			Content:   memo.Content,
			DeletedAt: memo.DeletedAt.Time,
		})
	}

	// 添加 Pages
	for _, page := range deletedPages {
		items = append(items, TrashItem{
			ID:        page.ID,
			Type:      "page",
			Title:     page.Title,
			DeletedAt: page.DeletedAt.Time,
		})
	}

	// 按删除时间倒序排序（最新的在前面）
	// 因为从数据库查询时已经按 deleted_at DESC 排序，这里直接合并即可
	// 但为了确保顺序，我们可能需要重新排序（如果需要的话）

	return &GetTrashResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// RestoreItemResponse 恢复项目响应
type RestoreItemResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// RestoreItem 恢复回收站项目
func (s *TrashService) RestoreItem(ctx context.Context, itemType string, id uint64) (*RestoreItemResponse, error) {
	switch itemType {
	case "memo":
		if err := s.trashRepo.RestoreMemo(ctx, id); err != nil {
			return nil, fmt.Errorf("restore memo: %w", err)
		}
		return &RestoreItemResponse{
			Success: true,
			Message: "Memo restored successfully",
		}, nil
	case "page":
		if err := s.trashRepo.RestorePage(ctx, id); err != nil {
			return nil, fmt.Errorf("restore page: %w", err)
		}
		return &RestoreItemResponse{
			Success: true,
			Message: "Page and its blocks restored successfully",
		}, nil
	default:
		return nil, fmt.Errorf("invalid item type: %s", itemType)
	}
}

// PermanentlyDeleteItemResponse 永久删除项目响应
type PermanentlyDeleteItemResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// PermanentlyDeleteItem 永久删除回收站项目
func (s *TrashService) PermanentlyDeleteItem(ctx context.Context, itemType string, id uint64) (*PermanentlyDeleteItemResponse, error) {
	switch itemType {
	case "memo":
		if err := s.trashRepo.PermanentlyDeleteMemo(ctx, id); err != nil {
			return nil, fmt.Errorf("permanently delete memo: %w", err)
		}
		return &PermanentlyDeleteItemResponse{
			Success: true,
			Message: "Memo permanently deleted",
		}, nil
	case "page":
		if err := s.trashRepo.PermanentlyDeletePage(ctx, id); err != nil {
			return nil, fmt.Errorf("permanently delete page: %w", err)
		}
		return &PermanentlyDeleteItemResponse{
			Success: true,
			Message: "Page and its blocks permanently deleted",
		}, nil
	default:
		return nil, fmt.Errorf("invalid item type: %s", itemType)
	}
}
