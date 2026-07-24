package shared

import (
	"context"
	"testing"

	domaindept "goKit/internal/domain/dept"
	domainrole "goKit/internal/domain/role"
	"goKit/internal/domain/shared/datascope"
	domainuser "goKit/internal/domain/user"
	"goKit/pkg/kit/auth"
)

// ---- 手写 fake 仓储 ----

type fakeUserRepo struct {
	byID map[uint64]*domainuser.User
}

func (r *fakeUserRepo) Create(ctx context.Context, u *domainuser.User) error { return nil }
func (r *fakeUserRepo) Update(ctx context.Context, u *domainuser.User) error { return nil }
func (r *fakeUserRepo) Delete(ctx context.Context, id uint64) error          { return nil }
func (r *fakeUserRepo) FindByID(ctx context.Context, id uint64) (*domainuser.User, error) {
	return r.byID[id], nil
}
func (r *fakeUserRepo) FindByUsername(ctx context.Context, username string) (*domainuser.User, error) {
	return nil, nil
}
func (r *fakeUserRepo) FindByIDs(ctx context.Context, ids []uint64) ([]domainuser.User, error) {
	return nil, nil
}
func (r *fakeUserRepo) List(ctx context.Context, filter datascope.Filter, page, pageSize int) ([]domainuser.User, int64, error) {
	return nil, 0, nil
}
func (r *fakeUserRepo) UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error {
	return nil
}

type fakeRoleRepo struct {
	byUser map[uint64][]domainrole.Role
}

func (r *fakeRoleRepo) Create(ctx context.Context, role *domainrole.Role) error { return nil }
func (r *fakeRoleRepo) Update(ctx context.Context, role *domainrole.Role) error { return nil }
func (r *fakeRoleRepo) Delete(ctx context.Context, id uint64) error             { return nil }
func (r *fakeRoleRepo) FindByID(ctx context.Context, id uint64) (*domainrole.Role, error) {
	return nil, nil
}
func (r *fakeRoleRepo) List(ctx context.Context, page, pageSize int) ([]domainrole.Role, int64, error) {
	return nil, 0, nil
}
func (r *fakeRoleRepo) FindByUserID(ctx context.Context, userID uint64) ([]domainrole.Role, error) {
	return r.byUser[userID], nil
}

type fakeDeptRepo struct {
	descendants map[uint64][]uint64
}

func (r *fakeDeptRepo) Create(ctx context.Context, dept *domaindept.Dept) error { return nil }
func (r *fakeDeptRepo) Update(ctx context.Context, dept *domaindept.Dept) error { return nil }
func (r *fakeDeptRepo) Delete(ctx context.Context, id uint64) error             { return nil }
func (r *fakeDeptRepo) FindByID(ctx context.Context, id uint64) (*domaindept.Dept, error) {
	return nil, nil
}
func (r *fakeDeptRepo) List(ctx context.Context) ([]domaindept.Dept, error) { return nil, nil }
func (r *fakeDeptRepo) FindDescendantIDs(ctx context.Context, deptID uint64) ([]uint64, error) {
	return r.descendants[deptID], nil
}

type fakeAssignRepo struct {
	deptIDsByRole map[uint64][]uint64
}

func (r *fakeAssignRepo) SetUserRoles(ctx context.Context, userID uint64, roleIDs []uint64) error {
	return nil
}
func (r *fakeAssignRepo) GetRoleIDsByUser(ctx context.Context, userID uint64) ([]uint64, error) {
	return nil, nil
}
func (r *fakeAssignRepo) SetRoleMenus(ctx context.Context, roleID uint64, menuIDs []uint64) error {
	return nil
}
func (r *fakeAssignRepo) GetMenuIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return nil, nil
}
func (r *fakeAssignRepo) SetRoleDepts(ctx context.Context, roleID uint64, deptIDs []uint64) error {
	return nil
}
func (r *fakeAssignRepo) GetDeptIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return r.deptIDsByRole[roleID], nil
}
func (r *fakeAssignRepo) SetRoleUsers(ctx context.Context, roleID uint64, userIDs []uint64) error {
	return nil
}
func (r *fakeAssignRepo) GetUserIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return nil, nil
}

// ---- DataScopeHelper.Build ----

func ctxWithUser(userID uint64, isSuper bool) context.Context {
	return auth.WithCurrentUser(context.Background(), &auth.CurrentUser{
		UserID:   userID,
		Username: "tester",
		IsSuper:  isSuper,
	})
}

func newHelper(user *domainuser.User, roles []domainrole.Role) *DataScopeHelper {
	userRepo := &fakeUserRepo{byID: map[uint64]*domainuser.User{}}
	if user != nil {
		userRepo.byID[user.ID] = user
	}
	roleRepo := &fakeRoleRepo{byUser: map[uint64][]domainrole.Role{}}
	if user != nil && roles != nil {
		roleRepo.byUser[user.ID] = roles
	}
	deptRepo := &fakeDeptRepo{descendants: map[uint64][]uint64{10: {10, 11, 12}}}
	assignRepo := &fakeAssignRepo{deptIDsByRole: map[uint64][]uint64{20: {30, 31}}}
	return NewDataScopeHelper(userRepo, roleRepo, deptRepo, assignRepo)
}

func TestDataScopeHelperBuild(t *testing.T) {
	normalUser := &domainuser.User{ID: 1, Username: "alice", DeptID: 10, Status: 1}

	t.Run("未登录上下文 DenyAll", func(t *testing.T) {
		h := newHelper(normalUser, nil)
		f, err := h.Build(context.Background())
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !f.DenyAll {
			t.Errorf("filter = %+v, want DenyAll", f)
		}
	})

	t.Run("超管放行全部", func(t *testing.T) {
		h := newHelper(normalUser, nil)
		f, err := h.Build(ctxWithUser(1, true))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !f.All {
			t.Errorf("filter = %+v, want All", f)
		}
	})

	t.Run("用户不存在 DenyAll", func(t *testing.T) {
		h := newHelper(nil, nil)
		f, err := h.Build(ctxWithUser(999, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !f.DenyAll {
			t.Errorf("filter = %+v, want DenyAll", f)
		}
	})

	t.Run("无角色 DenyAll", func(t *testing.T) {
		h := newHelper(normalUser, nil)
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !f.DenyAll {
			t.Errorf("filter = %+v, want DenyAll", f)
		}
	})

	t.Run("角色数据范围为全部", func(t *testing.T) {
		h := newHelper(normalUser, []domainrole.Role{{ID: 20, DataScope: domainrole.DataScopeAll}})
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !f.All {
			t.Errorf("filter = %+v, want All", f)
		}
	})

	t.Run("自定义部门", func(t *testing.T) {
		h := newHelper(normalUser, []domainrole.Role{{ID: 20, DataScope: domainrole.DataScopeCustom}})
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if len(f.DeptIDs) != 2 || f.All || f.DenyAll {
			t.Errorf("filter = %+v, want DeptIDs from assignRepo {30,31}", f)
		}
	})

	t.Run("本部门及以下", func(t *testing.T) {
		h := newHelper(normalUser, []domainrole.Role{{ID: 21, DataScope: domainrole.DataScopeDeptAndChildren}})
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if len(f.DeptIDs) != 3 {
			t.Errorf("filter = %+v, want DeptIDs from deptRepo {10,11,12}", f)
		}
	})

	t.Run("本部门", func(t *testing.T) {
		h := newHelper(normalUser, []domainrole.Role{{ID: 22, DataScope: domainrole.DataScopeDept}})
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if len(f.DeptIDs) != 1 || f.DeptIDs[0] != 10 {
			t.Errorf("filter = %+v, want DeptIDs {10}", f)
		}
	})

	t.Run("仅本人", func(t *testing.T) {
		h := newHelper(normalUser, []domainrole.Role{{ID: 23, DataScope: domainrole.DataScopeSelf}})
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if f.SelfID != 1 || f.All || f.DenyAll {
			t.Errorf("filter = %+v, want SelfID=1", f)
		}
	})

	t.Run("多角色合并取并集", func(t *testing.T) {
		h := newHelper(normalUser, []domainrole.Role{
			{ID: 22, DataScope: domainrole.DataScopeDept},
			{ID: 23, DataScope: domainrole.DataScopeSelf},
		})
		f, err := h.Build(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if len(f.DeptIDs) != 1 || f.DeptIDs[0] != 10 || f.SelfID != 1 {
			t.Errorf("filter = %+v, want DeptIDs {10} + SelfID 1", f)
		}
	})
}
