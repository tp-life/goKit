package user

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	authapp "goKit/internal/application/auth"
	"goKit/internal/application/shared"
	domainrole "goKit/internal/domain/role"
	domainuser "goKit/internal/domain/user"
	"goKit/pkg/kit/db"
)

type UserService struct {
	userRepo   domainuser.UserRepository
	assignRepo domainrole.AssignRepository
	scope      *shared.DataScopeHelper
	resolver   *authapp.PermResolver
	tx         *db.Client
}

func NewUserService(
	userRepo domainuser.UserRepository,
	assignRepo domainrole.AssignRepository,
	scope *shared.DataScopeHelper,
	resolver *authapp.PermResolver,
	tx *db.Client,
) *UserService {
	return &UserService{userRepo, assignRepo, scope, resolver, tx}
}

func toUserResp(u *domainuser.User) *UserResp {
	return &UserResp{
		ID:        u.ID,
		Username:  u.Username,
		Nickname:  u.Nickname,
		Email:     u.Email,
		DeptID:    u.DeptID,
		Status:    u.Status,
		IsSuper:   u.IsSuper,
		CreatedAt: u.CreatedAt,
	}
}

func (s *UserService) Create(ctx context.Context, req CreateUserReq) (uint64, error) {
	existing, err := s.userRepo.FindByUsername(ctx, req.Username)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return 0, shared.ErrDuplicate
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	user := &domainuser.User{
		Username: req.Username,
		Password: string(hashed),
		Nickname: req.Nickname,
		Email:    req.Email,
		DeptID:   req.DeptID,
		Status:   1,
	}
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.userRepo.Create(ctx, user); err != nil {
			return err
		}
		return s.assignRepo.SetUserRoles(ctx, user.ID, req.RoleIDs)
	})
	return user.ID, err
}

func (s *UserService) Update(ctx context.Context, id uint64, req UpdateUserReq) error {
	user, err := s.mustGet(ctx, id)
	if err != nil {
		return err
	}
	user.Nickname = req.Nickname
	user.Email = req.Email
	user.DeptID = req.DeptID
	user.Status = req.Status
	if err := s.userRepo.Update(ctx, user); err != nil {
		return err
	}
	s.resolver.Invalidate() // 状态变更影响权限
	return nil
}

func (s *UserService) Delete(ctx context.Context, id uint64) error {
	if _, err := s.mustGet(ctx, id); err != nil {
		return err
	}
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.assignRepo.SetUserRoles(ctx, id, nil); err != nil {
			return err
		}
		return s.userRepo.Delete(ctx, id)
	})
	if err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

func (s *UserService) Get(ctx context.Context, id uint64) (*UserResp, error) {
	user, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := toUserResp(user)
	resp.RoleIDs, err = s.assignRepo.GetRoleIDsByUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// List 用户分页列表，自动应用数据权限
func (s *UserService) List(ctx context.Context, page shared.PageReq) (*shared.PageResp[*UserResp], error) {
	page.Normalize()
	filter, err := s.scope.Build(ctx)
	if err != nil {
		return nil, err
	}
	users, total, err := s.userRepo.List(ctx, filter, page.Page, page.PageSize)
	if err != nil {
		return nil, err
	}
	list := make([]*UserResp, 0, len(users))
	for i := range users {
		list = append(list, toUserResp(&users[i]))
	}
	return &shared.PageResp[*UserResp]{List: list, Total: total}, nil
}

// AssignRoles 给用户分配角色（全量重置）
func (s *UserService) AssignRoles(ctx context.Context, id uint64, req AssignRolesReq) error {
	if _, err := s.mustGet(ctx, id); err != nil {
		return err
	}
	if err := s.assignRepo.SetUserRoles(ctx, id, req.RoleIDs); err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

// ResetPassword 管理员重置用户密码
func (s *UserService) ResetPassword(ctx context.Context, id uint64, req ResetPwdReq) error {
	if _, err := s.mustGet(ctx, id); err != nil {
		return err
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.userRepo.UpdatePassword(ctx, id, string(hashed))
}

func (s *UserService) mustGet(ctx context.Context, id uint64) (*domainuser.User, error) {
	user, err := s.userRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, shared.ErrNotFound
	}
	return user, nil
}
