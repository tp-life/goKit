package role

import (
	"context"

	authapp "goKit/internal/application/auth"
	"goKit/internal/application/shared"
	domainrole "goKit/internal/domain/role"
	"goKit/pkg/kit/db"
)

type RoleService struct {
	roleRepo   domainrole.RoleRepository
	assignRepo domainrole.AssignRepository
	resolver   *authapp.PermResolver
	tx         *db.Client
}

func NewRoleService(
	roleRepo domainrole.RoleRepository,
	assignRepo domainrole.AssignRepository,
	resolver *authapp.PermResolver,
	tx *db.Client,
) *RoleService {
	return &RoleService{roleRepo, assignRepo, resolver, tx}
}

func toRoleResp(r *domainrole.Role) *RoleResp {
	return &RoleResp{
		ID:        r.ID,
		Name:      r.Name,
		Code:      r.Code,
		DataScope: r.DataScope,
		Status:    r.Status,
		Remark:    r.Remark,
		CreatedAt: r.CreatedAt,
	}
}

func (s *RoleService) Create(ctx context.Context, req CreateRoleReq) (uint64, error) {
	role := &domainrole.Role{
		Name:      req.Name,
		Code:      req.Code,
		DataScope: req.DataScope,
		Remark:    req.Remark,
		Status:    1,
	}
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.roleRepo.Create(ctx, role); err != nil {
			return err
		}
		if err := s.assignRepo.SetRoleMenus(ctx, role.ID, req.MenuIDs); err != nil {
			return err
		}
		return s.assignRepo.SetRoleDepts(ctx, role.ID, req.DeptIDs)
	})
	return role.ID, err
}

func (s *RoleService) Update(ctx context.Context, id uint64, req UpdateRoleReq) error {
	role, err := s.mustGet(ctx, id)
	if err != nil {
		return err
	}
	role.Name = req.Name
	role.Code = req.Code
	role.DataScope = req.DataScope
	role.Status = req.Status
	role.Remark = req.Remark
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.roleRepo.Update(ctx, role); err != nil {
			return err
		}
		if req.DeptIDs != nil {
			return s.assignRepo.SetRoleDepts(ctx, id, req.DeptIDs)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

func (s *RoleService) Delete(ctx context.Context, id uint64) error {
	if _, err := s.mustGet(ctx, id); err != nil {
		return err
	}
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.assignRepo.SetRoleMenus(ctx, id, nil); err != nil {
			return err
		}
		if err := s.assignRepo.SetRoleDepts(ctx, id, nil); err != nil {
			return err
		}
		if err := s.assignRepo.SetRoleUsers(ctx, id, nil); err != nil {
			return err
		}
		return s.roleRepo.Delete(ctx, id)
	})
	if err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

// Get 角色详情（含已分配的菜单与自定义部门）
func (s *RoleService) Get(ctx context.Context, id uint64) (*RoleResp, error) {
	role, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := toRoleResp(role)
	if resp.MenuIDs, err = s.assignRepo.GetMenuIDsByRole(ctx, id); err != nil {
		return nil, err
	}
	if resp.DeptIDs, err = s.assignRepo.GetDeptIDsByRole(ctx, id); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *RoleService) List(ctx context.Context, page shared.PageReq) (*shared.PageResp[*RoleResp], error) {
	page.Normalize()
	roles, total, err := s.roleRepo.List(ctx, page.Page, page.PageSize)
	if err != nil {
		return nil, err
	}
	list := make([]*RoleResp, 0, len(roles))
	for i := range roles {
		list = append(list, toRoleResp(&roles[i]))
	}
	return &shared.PageResp[*RoleResp]{List: list, Total: total}, nil
}

// AssignMenus 给角色分配菜单权限（全量重置）
func (s *RoleService) AssignMenus(ctx context.Context, id uint64, req AssignMenusReq) error {
	if _, err := s.mustGet(ctx, id); err != nil {
		return err
	}
	if err := s.assignRepo.SetRoleMenus(ctx, id, req.MenuIDs); err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

// AssignUsers 给角色分配用户（全量重置）
func (s *RoleService) AssignUsers(ctx context.Context, id uint64, req AssignUsersReq) error {
	if _, err := s.mustGet(ctx, id); err != nil {
		return err
	}
	if err := s.assignRepo.SetRoleUsers(ctx, id, req.UserIDs); err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

// GetUserIDs 查询角色下的用户 ID 列表
func (s *RoleService) GetUserIDs(ctx context.Context, id uint64) ([]uint64, error) {
	if _, err := s.mustGet(ctx, id); err != nil {
		return nil, err
	}
	return s.assignRepo.GetUserIDsByRole(ctx, id)
}

func (s *RoleService) mustGet(ctx context.Context, id uint64) (*domainrole.Role, error) {
	role, err := s.roleRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, shared.ErrNotFound
	}
	return role, nil
}
