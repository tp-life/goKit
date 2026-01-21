package persistence

import (
	"context"
	"errors"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"

	"gorm.io/gorm"
)

type SessionRepo struct {
	client *db.Client
}

func NewSessionRepo(client *db.Client) repository.SessionRepository {
	return &SessionRepo{client: client}
}

func (r *SessionRepo) Create(ctx context.Context, session *entity.UserSession) error {
	return r.client.GetDB(ctx).Create(session).Error
}

func (r *SessionRepo) Update(ctx context.Context, session *entity.UserSession) error {
	return r.client.GetDB(ctx).Updates(session).Error
}

func (r *SessionRepo) FindByRefreshToken(ctx context.Context, refreshToken string) (*entity.UserSession, error) {
	var session entity.UserSession
	err := r.client.GetDB(ctx).Where("refresh_token = ?", refreshToken).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &session, err
}

func (r *SessionRepo) DeleteByRefreshToken(ctx context.Context, refreshToken string) error {
	return r.client.GetDB(ctx).Where("refresh_token = ?", refreshToken).Delete(&entity.UserSession{}).Error
}

func (r *SessionRepo) DeleteByUserID(ctx context.Context, userID uint64) error {
	return r.client.GetDB(ctx).Where("user_id = ?", userID).Delete(&entity.UserSession{}).Error
}
