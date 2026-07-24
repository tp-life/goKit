package service

import (
	"context"
	"errors"
	"sort"

	"golang.org/x/crypto/bcrypt"

	"goKit/internal/modules/system/application/dto"
	"goKit/internal/modules/system/domain/repository"
	"goKit/pkg/kit/auth"
)

// 业务错误
var (
	ErrNotFound          = errors.New("record not found")
	ErrInvalidCredential = errors.New("invalid credential")
	ErrForbidden         = errors.New("forbidden")
	ErrDuplicate         = errors.New("duplicate")
)

type AuthService struct {
	userRepo repository.UserRepository
	roleRepo repository.RoleRepository
	resolver *PermResolver
	tokens   *auth.TokenManager
}

func NewAuthService(
	userRepo repository.UserRepository,
	roleRepo repository.RoleRepository,
	resolver *PermResolver,
	tokens *auth.TokenManager,
) *AuthService {
	return &AuthService{userRepo, roleRepo, resolver, tokens}
}

// Login 账号密码登录，签发 JWT
func (s *AuthService) Login(ctx context.Context, req dto.LoginReq) (*dto.LoginResp, error) {
	user, err := s.userRepo.FindByUsername(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != 1 ||
		bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		return nil, ErrInvalidCredential
	}
	token, err := s.tokens.Generate(user.ID, user.Username, user.IsSuper)
	if err != nil {
		return nil, err
	}
	return &dto.LoginResp{Token: token}, nil
}

// Profile 当前用户信息 + 角色与权限点
func (s *AuthService) Profile(ctx context.Context) (*dto.ProfileResp, error) {
	cu := auth.FromContext(ctx)
	if cu == nil {
		return nil, ErrInvalidCredential
	}
	user, err := s.userRepo.FindByID(ctx, cu.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrNotFound
	}

	roles, err := s.roleRepo.FindByUserID(ctx, cu.UserID)
	if err != nil {
		return nil, err
	}
	roleCodes := make([]string, 0, len(roles))
	for _, r := range roles {
		roleCodes = append(roleCodes, r.Code)
	}

	set, err := s.resolver.Resolve(ctx, cu.UserID)
	if err != nil {
		return nil, err
	}
	perms := make([]string, 0, len(set.Codes))
	for c := range set.Codes {
		perms = append(perms, c)
	}
	sort.Strings(perms)

	return &dto.ProfileResp{
		ID:       user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
		Email:    user.Email,
		DeptID:   user.DeptID,
		IsSuper:  user.IsSuper,
		Roles:    roleCodes,
		Perms:    perms,
	}, nil
}

// ChangePassword 修改自己的密码
func (s *AuthService) ChangePassword(ctx context.Context, req dto.ChangePwdReq) error {
	cu := auth.FromContext(ctx)
	if cu == nil {
		return ErrInvalidCredential
	}
	user, err := s.userRepo.FindByID(ctx, cu.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)) != nil {
		return ErrInvalidCredential
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.userRepo.UpdatePassword(ctx, cu.UserID, string(hashed))
}
