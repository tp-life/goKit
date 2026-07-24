package auth

import (
	"context"

	domainuser "goKit/internal/domain/user"
	kitauth "goKit/pkg/kit/auth"
)

// LocalUserProvider 本地用户信息提供者（单机模式），实现 auth.UserProvider 端口
type LocalUserProvider struct {
	userRepo domainuser.UserRepository
}

func NewLocalUserProvider(userRepo domainuser.UserRepository) *LocalUserProvider {
	return &LocalUserProvider{userRepo: userRepo}
}

func toUserInfo(u *domainuser.User) *kitauth.UserInfo {
	return &kitauth.UserInfo{
		ID:       u.ID,
		Username: u.Username,
		Nickname: u.Nickname,
		Email:    u.Email,
		DeptID:   u.DeptID,
		Status:   u.Status,
		IsSuper:  u.IsSuper,
	}
}

func (p *LocalUserProvider) GetUser(ctx context.Context, id uint64) (*kitauth.UserInfo, error) {
	user, err := p.userRepo.FindByID(ctx, id)
	if err != nil || user == nil {
		return nil, err
	}
	return toUserInfo(user), nil
}

func (p *LocalUserProvider) GetUsers(ctx context.Context, ids []uint64) (map[uint64]*kitauth.UserInfo, error) {
	users, err := p.userRepo.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]*kitauth.UserInfo, len(users))
	for i := range users {
		result[users[i].ID] = toUserInfo(&users[i])
	}
	return result, nil
}
