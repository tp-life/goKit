package user

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	authapp "goKit/internal/application/auth"
	"goKit/internal/application/shared"
	domainmenu "goKit/internal/domain/menu"
	"goKit/internal/domain/shared/datascope"
	domainuser "goKit/internal/domain/user"
	"goKit/pkg/kit/auth"
	"goKit/pkg/kit/cache"
)

// ---- 手写 fake 仓储 ----

type fakeUserRepo struct {
	byID       map[uint64]*domainuser.User
	updatedPwd map[uint64]string
	listFilter datascope.Filter
}

func newFakeUserRepo(users ...*domainuser.User) *fakeUserRepo {
	r := &fakeUserRepo{byID: map[uint64]*domainuser.User{}, updatedPwd: map[uint64]string{}}
	for _, u := range users {
		r.byID[u.ID] = u
	}
	return r
}

func (r *fakeUserRepo) Create(ctx context.Context, u *domainuser.User) error { return nil }
func (r *fakeUserRepo) Update(ctx context.Context, u *domainuser.User) error {
	r.byID[u.ID] = u
	return nil
}
func (r *fakeUserRepo) Delete(ctx context.Context, id uint64) error { return nil }
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
	r.listFilter = filter
	users := make([]domainuser.User, 0, len(r.byID))
	for _, u := range r.byID {
		users = append(users, *u)
	}
	return users, int64(len(users)), nil
}
func (r *fakeUserRepo) UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error {
	r.updatedPwd[id] = hashedPwd
	return nil
}

type fakeAssignRepo struct {
	roleIDsByUser map[uint64][]uint64
}

func (r *fakeAssignRepo) SetUserRoles(ctx context.Context, userID uint64, roleIDs []uint64) error {
	r.roleIDsByUser[userID] = roleIDs
	return nil
}
func (r *fakeAssignRepo) GetRoleIDsByUser(ctx context.Context, userID uint64) ([]uint64, error) {
	return r.roleIDsByUser[userID], nil
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
	return nil, nil
}
func (r *fakeAssignRepo) SetRoleUsers(ctx context.Context, roleID uint64, userIDs []uint64) error {
	return nil
}
func (r *fakeAssignRepo) GetUserIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return nil, nil
}

type fakeMenuRepo struct{}

func (r *fakeMenuRepo) Create(ctx context.Context, menu *domainmenu.Menu) error { return nil }
func (r *fakeMenuRepo) Update(ctx context.Context, menu *domainmenu.Menu) error { return nil }
func (r *fakeMenuRepo) Delete(ctx context.Context, id uint64) error             { return nil }
func (r *fakeMenuRepo) FindByID(ctx context.Context, id uint64) (*domainmenu.Menu, error) {
	return nil, nil
}
func (r *fakeMenuRepo) List(ctx context.Context) ([]domainmenu.Menu, error) { return nil, nil }
func (r *fakeMenuRepo) FindPermCodesByRoleIDs(ctx context.Context, roleIDs []uint64) ([]string, error) {
	return nil, nil
}

// ---- 测试辅助 ----

// newTestService 组装 UserService；tx 传 nil，仅覆盖不经过 WithTx 的路径
func newTestService(userRepo *fakeUserRepo, assignRepo *fakeAssignRepo) *UserService {
	resolver := authapp.NewPermResolver(userRepo, assignRepo, &fakeMenuRepo{}, cache.NewMemory())
	scope := shared.NewDataScopeHelper(userRepo, nil, nil, assignRepo)
	return NewUserService(userRepo, assignRepo, scope, resolver, nil)
}

func ctxWithSuper() context.Context {
	return auth.WithCurrentUser(context.Background(), &auth.CurrentUser{
		UserID:   100,
		Username: "admin",
		IsSuper:  true,
	})
}

// ---- UserService ----

func TestUserServiceGet(t *testing.T) {
	userRepo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Nickname: "Alice", Status: 1})
	assignRepo := &fakeAssignRepo{roleIDsByUser: map[uint64][]uint64{1: {10, 11}}}
	svc := newTestService(userRepo, assignRepo)

	resp, err := svc.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.ID != 1 || resp.Username != "alice" || resp.Nickname != "Alice" {
		t.Errorf("resp mismatch: %+v", resp)
	}
	if len(resp.RoleIDs) != 2 || resp.RoleIDs[0] != 10 || resp.RoleIDs[1] != 11 {
		t.Errorf("RoleIDs = %v, want [10 11]", resp.RoleIDs)
	}

	_, err = svc.Get(context.Background(), 999)
	if !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUserServiceUpdate(t *testing.T) {
	userRepo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Nickname: "Alice", Status: 1})
	svc := newTestService(userRepo, &fakeAssignRepo{roleIDsByUser: map[uint64][]uint64{}})

	err := svc.Update(context.Background(), 1, UpdateUserReq{Nickname: "NewName", Email: "a@b.c", DeptID: 5, Status: 0})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	u := userRepo.byID[1]
	if u.Nickname != "NewName" || u.Email != "a@b.c" || u.DeptID != 5 || u.Status != 0 {
		t.Errorf("user not updated: %+v", u)
	}

	err = svc.Update(context.Background(), 999, UpdateUserReq{Nickname: "x"})
	if !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUserServiceResetPassword(t *testing.T) {
	userRepo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Status: 1})
	svc := newTestService(userRepo, &fakeAssignRepo{roleIDsByUser: map[uint64][]uint64{}})

	err := svc.ResetPassword(context.Background(), 1, ResetPwdReq{Password: "new-pwd"})
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	hashed := userRepo.updatedPwd[1]
	if hashed == "" {
		t.Fatal("UpdatePassword 未被调用")
	}
	if bcrypt.CompareHashAndPassword([]byte(hashed), []byte("new-pwd")) != nil {
		t.Error("落库的哈希与新密码不匹配")
	}

	err = svc.ResetPassword(context.Background(), 999, ResetPwdReq{Password: "new-pwd"})
	if !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUserServiceAssignRoles(t *testing.T) {
	userRepo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Status: 1})
	assignRepo := &fakeAssignRepo{roleIDsByUser: map[uint64][]uint64{}}
	svc := newTestService(userRepo, assignRepo)

	err := svc.AssignRoles(context.Background(), 1, AssignRolesReq{RoleIDs: []uint64{10, 20}})
	if err != nil {
		t.Fatalf("AssignRoles: %v", err)
	}
	got := assignRepo.roleIDsByUser[1]
	if len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Errorf("roleIDs = %v, want [10 20]", got)
	}

	err = svc.AssignRoles(context.Background(), 999, AssignRolesReq{RoleIDs: []uint64{10}})
	if !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUserServiceList(t *testing.T) {
	userRepo := newFakeUserRepo(
		&domainuser.User{ID: 1, Username: "alice", Status: 1},
		&domainuser.User{ID: 2, Username: "bob", Status: 1},
	)
	svc := newTestService(userRepo, &fakeAssignRepo{roleIDsByUser: map[uint64][]uint64{}})

	resp, err := svc.List(ctxWithSuper(), shared.PageReq{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if resp.Total != 2 || len(resp.List) != 2 {
		t.Errorf("resp = %+v, want total 2", resp)
	}
	// 超管的数据权限应放行全部
	if !userRepo.listFilter.All {
		t.Errorf("filter = %+v, want All", userRepo.listFilter)
	}
	// 分页参数应被 Normalize 修正，不 panic 即可
}
