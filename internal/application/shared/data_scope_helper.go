package shared

import (
	"context"

	domaindept "goKit/internal/domain/dept"
	domainrole "goKit/internal/domain/role"
	"goKit/internal/domain/shared/datascope"
	domainuser "goKit/internal/domain/user"
	"goKit/pkg/kit/auth"
)

// DataScopeHelper 为当前登录用户构建数据权限过滤器
type DataScopeHelper struct {
	userRepo   domainuser.UserRepository
	roleRepo   domainrole.RoleRepository
	deptRepo   domaindept.DeptRepository
	assignRepo domainrole.AssignRepository
}

func NewDataScopeHelper(
	userRepo domainuser.UserRepository,
	roleRepo domainrole.RoleRepository,
	deptRepo domaindept.DeptRepository,
	assignRepo domainrole.AssignRepository,
) *DataScopeHelper {
	return &DataScopeHelper{userRepo, roleRepo, deptRepo, assignRepo}
}

// Build 从 ctx 取当前用户，展开各角色的部门范围后合并
func (h *DataScopeHelper) Build(ctx context.Context) (datascope.Filter, error) {
	cu := auth.FromContext(ctx)
	if cu == nil {
		return datascope.Filter{DenyAll: true}, nil
	}
	if cu.IsSuper {
		return datascope.Filter{All: true}, nil
	}

	user, err := h.userRepo.FindByID(ctx, cu.UserID)
	if err != nil {
		return datascope.Filter{}, err
	}
	if user == nil {
		return datascope.Filter{DenyAll: true}, nil
	}

	roles, err := h.roleRepo.FindByUserID(ctx, cu.UserID)
	if err != nil {
		return datascope.Filter{}, err
	}

	scopes := make([]datascope.RoleDataScope, 0, len(roles))
	for _, role := range roles {
		rs := datascope.RoleDataScope{Scope: role.DataScope}
		switch role.DataScope {
		case domainrole.DataScopeCustom:
			rs.DeptIDs, err = h.assignRepo.GetDeptIDsByRole(ctx, role.ID)
		case domainrole.DataScopeDeptAndChildren:
			rs.DeptIDs, err = h.deptRepo.FindDescendantIDs(ctx, user.DeptID)
		case domainrole.DataScopeDept:
			rs.DeptIDs = []uint64{user.DeptID}
		}
		if err != nil {
			return datascope.Filter{}, err
		}
		scopes = append(scopes, rs)
	}
	return datascope.MergeDataScope(cu.UserID, false, scopes), nil
}
