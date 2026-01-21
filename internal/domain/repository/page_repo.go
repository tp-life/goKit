package repository

import (
	"context"
	"goKit/internal/domain/entity"
	"time"
)

type PageRepository interface {
	Create(ctx context.Context, page *entity.Page) error
	Update(ctx context.Context, page *entity.Page) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*entity.Page, error)
	FindByShareID(ctx context.Context, shareID string) (*entity.Page, error)
	FindByUserID(ctx context.Context, userID uint64, limit, offset int) ([]*entity.Page, error)
	FindByUserIDAndTimeRange(ctx context.Context, userID uint64, startTime, endTime time.Time, limit int) ([]*entity.Page, error)
	FindPublicPages(ctx context.Context, limit, offset int) ([]*entity.Page, error) // 查询公开页面（访客模式）
}
