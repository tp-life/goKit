package service

import (
	"context"
	"errors"

	"goKit/internal/application/dto"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type UserService struct {
	repo repository.UserRepository
	tx   *db.Client
}

func NewUserService(repo repository.UserRepository, tx *db.Client) *UserService {
	return &UserService{repo: repo, tx: tx}
}

func (s *UserService) CreateUser(ctx context.Context, req dto.CreateUserReq) (uint64, error) {
	var userID uint64
	// 事务演示
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		user := &entity.User{
			Name:  req.Name,
			Email: req.Email,
		}
		if err := s.repo.Create(ctx, user); err != nil {
			return err
		}
		userID = user.ID
		return nil
	})
	return userID, err
}

func (s *UserService) GetUser(ctx context.Context, id uint64) (*dto.UserResp, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, errors.New("user not found")
	}
	return &dto.UserResp{ID: u.ID, Name: u.Name, Email: u.Email}, nil
}
