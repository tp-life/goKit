package repository

import (
	"context"
	"goKit/internal/domain/entity"
)

type UserRepository interface {
	Create(ctx context.Context, user *entity.User) error
	FindByID(ctx context.Context, id uint64) (*entity.User, error)
}
