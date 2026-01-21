package persistence

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type PageRepo struct {
	client *db.Client
}

func NewPageRepo(client *db.Client) repository.PageRepository {
	return &PageRepo{client: client}
}

func (r *PageRepo) Create(ctx context.Context, page *entity.Page) error {
	return r.client.GetDB(ctx).Create(page).Error
}

func (r *PageRepo) Update(ctx context.Context, page *entity.Page) error {
	return r.client.GetDB(ctx).Save(page).Error
}

func (r *PageRepo) Delete(ctx context.Context, id uint64) error {
	// GORM 会自动使用软删除（因为 Page 有 DeletedAt 字段）
	return r.client.GetDB(ctx).Delete(&entity.Page{}, id).Error
}

func (r *PageRepo) FindByID(ctx context.Context, id uint64) (*entity.Page, error) {
	var page entity.Page
	err := r.client.GetDB(ctx).Preload("Tags").First(&page, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &page, err
}

func (r *PageRepo) FindByShareID(ctx context.Context, shareID string) (*entity.Page, error) {
	var page entity.Page
	// share_id 可能为 NULL，需要特殊处理
	err := r.client.GetDB(ctx).Preload("Tags").Where("share_id = ? AND is_shared = ? AND share_id IS NOT NULL", shareID, true).First(&page).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &page, err
}

func (r *PageRepo) FindByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Page, error) {
	var pages []*entity.Page
	err := r.client.GetDB(ctx).
		Preload("Tags").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&pages).Error
	return pages, err
}

func (r *PageRepo) FindByUserIDAndTimeRange(ctx context.Context, userID uint64, startTime, endTime time.Time, limit int) ([]*entity.Page, error) {
	var pages []*entity.Page
	err := r.client.GetDB(ctx).
		Where("user_id = ? AND created_at >= ? AND created_at <= ?", userID, startTime, endTime).
		Order("created_at DESC").
		Limit(limit).
		Find(&pages).Error
	return pages, err
}

func (r *PageRepo) FindPublicPages(ctx context.Context, limit, offset int) ([]*entity.Page, error) {
	var pages []*entity.Page
	err := r.client.GetDB(ctx).
		Preload("Tags").
		Where("is_shared = ? AND share_id IS NOT NULL", true).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&pages).Error
	return pages, err
}
