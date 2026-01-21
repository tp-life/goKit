package persistence

import (
	"context"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"

	"gorm.io/gorm"
)

type TrashRepo struct {
	client *db.Client
}

func NewTrashRepo(client *db.Client) repository.TrashRepository {
	return &TrashRepo{client: client}
}

func (r *TrashRepo) FindDeletedMemos(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Memo, error) {
	var memos []*entity.Memo
	err := r.client.GetDB(ctx).
		Unscoped().
		Where("user_id = ? AND deleted_at IS NOT NULL", userID).
		Order("deleted_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&memos).Error
	return memos, err
}

func (r *TrashRepo) FindDeletedPages(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Page, error) {
	var pages []*entity.Page
	err := r.client.GetDB(ctx).
		Unscoped().
		Where("user_id = ? AND deleted_at IS NOT NULL", userID).
		Order("deleted_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&pages).Error
	return pages, err
}

func (r *TrashRepo) RestoreMemo(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).
		Unscoped().
		Model(&entity.Memo{}).
		Where("id = ?", id).
		Update("deleted_at", nil).Error
}

func (r *TrashRepo) RestorePage(ctx context.Context, id uint64) error {
	// 在事务中恢复 Page 和其 Blocks
	return r.client.GetDB(ctx).Transaction(func(tx *gorm.DB) error {
		// 恢复 Page
		if err := tx.Unscoped().
			Model(&entity.Page{}).
			Where("id = ?", id).
			Update("deleted_at", nil).Error; err != nil {
			return err
		}

		// 恢复该 Page 下的所有 Blocks
		if err := tx.Unscoped().
			Model(&entity.Block{}).
			Where("page_id = ?", id).
			Update("deleted_at", nil).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *TrashRepo) PermanentlyDeleteMemo(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).
		Unscoped().
		Delete(&entity.Memo{}, id).Error
}

func (r *TrashRepo) PermanentlyDeletePage(ctx context.Context, id uint64) error {
	// 在事务中永久删除 Page 和其 Blocks
	return r.client.GetDB(ctx).Transaction(func(tx *gorm.DB) error {
		// 先删除 Blocks
		if err := tx.Unscoped().
			Delete(&entity.Block{}, "page_id = ?", id).Error; err != nil {
			return err
		}

		// 再删除 Page
		if err := tx.Unscoped().
			Delete(&entity.Page{}, id).Error; err != nil {
			return err
		}

		return nil
	})
}
