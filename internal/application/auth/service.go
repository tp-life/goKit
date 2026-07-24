package auth

import (
	"context"
	"sort"

	"golang.org/x/crypto/bcrypt"

	"goKit/internal/application/shared"
	domainrole "goKit/internal/domain/role"
	domainuser "goKit/internal/domain/user"
	kitauth "goKit/pkg/kit/auth"
)

type AuthService struct {
	userRepo domainuser.UserRepository
	roleRepo domainrole.RoleRepository
	resolver *PermResolver
	tokens   *kitauth.TokenManager
}

func NewAuthService(
	userRepo domainuser.UserRepository,
	roleRepo domainrole.RoleRepository,
	resolver *PermResolver,
	tokens *kitauth.TokenManager,
) *AuthService {
	return &AuthService{userRepo, roleRepo, resolver, tokens}
}

// Login 账号密码登录，签发 JWT
func (s *AuthService) Login(ctx context.Context, req LoginReq) (*LoginResp, error) {
	user, err := s.userRepo.FindByUsername(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != 1 ||
		bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		return nil, shared.ErrInvalidCredential
	}
	token, err := s.tokens.Generate(user.ID, user.Username, user.IsSuper)
	if err != nil {
		return nil, err
	}
	return &LoginResp{Token: token}, nil
}

// Profile 当前用户信息 + 角色与权限点
func (s *AuthService) Profile(ctx context.Context) (*ProfileResp, error) {
	cu := kitauth.FromContext(ctx)
	if cu == nil {
		return nil, shared.ErrInvalidCredential
	}
	user, err := s.userRepo.FindByID(ctx, cu.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, shared.ErrNotFound
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

	return &ProfileResp{
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
func (s *AuthService) ChangePassword(ctx context.Context, req ChangePwdReq) error {
	cu := kitauth.FromContext(ctx)
	if cu == nil {
		return shared.ErrInvalidCredential
	}
	user, err := s.userRepo.FindByID(ctx, cu.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return shared.ErrNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)) != nil {
		return shared.ErrInvalidCredential
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.userRepo.UpdatePassword(ctx, cu.UserID, string(hashed))
}
