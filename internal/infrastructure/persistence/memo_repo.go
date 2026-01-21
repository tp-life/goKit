package persistence

import (
	"context"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"time"
)

type MemoRepo struct {
	client *db.Client
}

func NewMemoRepo(client *db.Client) repository.MemoRepository {
	return &MemoRepo{client: client}
}

func (r *MemoRepo) Create(ctx context.Context, memo *entity.Memo) error {
	return r.client.GetDB(ctx).Create(memo).Error
}

func (r *MemoRepo) Update(ctx context.Context, memo *entity.Memo) error {
	return r.client.GetDB(ctx).Updates(memo).Error
}

func (r *MemoRepo) Delete(ctx context.Context, id uint64) error {
	// GORM 会自动使用软删除（因为 Memo 有 DeletedAt 字段）
	return r.client.GetDB(ctx).Delete(&entity.Memo{}, id).Error
}

func (r *MemoRepo) FindByID(ctx context.Context, id uint64) (*entity.Memo, error) {
	var memo entity.Memo
	err := r.client.GetDB(ctx).Preload("Tags").Where("id = ?", id).First(&memo).Error
	if err != nil {
		return nil, err
	}
	return &memo, nil
}

func (r *MemoRepo) FindByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Memo, error) {
	var memos []*entity.Memo
	err := r.client.GetDB(ctx).
		Preload("Tags").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&memos).Error
	return memos, err
}

func (r *MemoRepo) FindByUserIDAndTimeRange(ctx context.Context, userID uint64, startTime, endTime time.Time, limit int) ([]*entity.Memo, error) {
	var memos []*entity.Memo
	err := r.client.GetDB(ctx).
		Where("user_id = ? AND created_at >= ? AND created_at <= ?", userID, startTime, endTime).
		Order("created_at DESC").
		Limit(limit).
		Find(&memos).Error
	return memos, err
}
