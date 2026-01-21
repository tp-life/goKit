package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type UserRepo struct {
	client *db.Client
}

func NewUserRepo(client *db.Client) repository.UserRepository {
	return &UserRepo{client: client}
}

func (r *UserRepo) Create(ctx context.Context, user *entity.User) error {
	return r.client.GetDB(ctx).Create(user).Error
}

func (r *UserRepo) FindByID(ctx context.Context, id uint64) (*entity.User, error) {
	var user entity.User
	err := r.client.GetDB(ctx).First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	var user entity.User
	err := r.client.GetDB(ctx).Where("email = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}
