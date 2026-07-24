package service

import (
	"context"

	"goKit/internal/modules/system/domain/entity"
	"goKit/internal/modules/system/domain/repository"
	domainsvc "goKit/internal/modules/system/domain/service"
	"goKit/pkg/kit/auth"
)

// DataScopeHelper 为当前登录用户构建数据权限过滤器
type DataScopeHelper struct {
	userRepo   repository.UserRepository
	roleRepo   repository.RoleRepository
	deptRepo   repository.DeptRepository
	assignRepo repository.AssignRepository
}

func NewDataScopeHelper(
	userRepo repository.UserRepository,
	roleRepo repository.RoleRepository,
	deptRepo repository.DeptRepository,
	assignRepo repository.AssignRepository,
) *DataScopeHelper {
	return &DataScopeHelper{userRepo, roleRepo, deptRepo, assignRepo}
}

// Build 从 ctx 取当前用户，展开各角色的部门范围后合并
func (h *DataScopeHelper) Build(ctx context.Context) (domainsvc.Filter, error) {
	cu := auth.FromContext(ctx)
	if cu == nil {
		return domainsvc.Filter{DenyAll: true}, nil
	}
	if cu.IsSuper {
		return domainsvc.Filter{All: true}, nil
	}

	user, err := h.userRepo.FindByID(ctx, cu.UserID)
	if err != nil {
		return domainsvc.Filter{}, err
	}
	if user == nil {
		return domainsvc.Filter{DenyAll: true}, nil
	}

	roles, err := h.roleRepo.FindByUserID(ctx, cu.UserID)
	if err != nil {
		return domainsvc.Filter{}, err
	}

	scopes := make([]domainsvc.RoleDataScope, 0, len(roles))
	for _, role := range roles {
		rs := domainsvc.RoleDataScope{Scope: role.DataScope}
		switch role.DataScope {
		case entity.DataScopeCustom:
			rs.DeptIDs, err = h.assignRepo.GetDeptIDsByRole(ctx, role.ID)
		case entity.DataScopeDeptAndChildren:
			rs.DeptIDs, err = h.deptRepo.FindDescendantIDs(ctx, user.DeptID)
		case entity.DataScopeDept:
			rs.DeptIDs = []uint64{user.DeptID}
		}
		if err != nil {
			return domainsvc.Filter{}, err
		}
		scopes = append(scopes, rs)
	}
	return domainsvc.MergeDataScope(cu.UserID, false, scopes), nil
}
