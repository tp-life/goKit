package repository

import (
	"context"
	"goKit/internal/domain/entity"
	"time"
)

type MemoRepository interface {
	Create(ctx context.Context, memo *entity.Memo) error
	Update(ctx context.Context, memo *entity.Memo) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*entity.Memo, error)
	FindByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Memo, error)
	FindByUserIDAndTimeRange(ctx context.Context, userID uint64, startTime, endTime time.Time, limit int) ([]*entity.Memo, error)
}
