package repository

import (
	"context"
	"goKit/internal/domain/entity"
)

type SessionRepository interface {
	Create(ctx context.Context, session *entity.UserSession) error
	Update(ctx context.Context, session *entity.UserSession) error
	FindByRefreshToken(ctx context.Context, refreshToken string) (*entity.UserSession, error)
	DeleteByRefreshToken(ctx context.Context, refreshToken string) error
	DeleteByUserID(ctx context.Context, userID uint64) error
}
