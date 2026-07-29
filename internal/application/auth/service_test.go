package auth

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"goKit/internal/application/shared"
	domainmenu "goKit/internal/domain/menu"
	domainrole "goKit/internal/domain/role"
	"goKit/internal/domain/shared/datascope"
	domainuser "goKit/internal/domain/user"
	kitauth "goKit/pkg/kit/auth"
	"goKit/pkg/kit/cache"
)

// ---- 手写 fake 仓储 ----

type fakeUserRepo struct {
	byID       map[uint64]*domainuser.User
	updatedPwd map[uint64]string
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
	for _, u := range r.byID {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, nil
}
func (r *fakeUserRepo) FindByIDs(ctx context.Context, ids []uint64) ([]domainuser.User, error) {
	users := make([]domainuser.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := r.byID[id]; ok {
			users = append(users, *u)
		}
	}
	return users, nil
}
func (r *fakeUserRepo) List(ctx context.Context, filter datascope.Filter, q domainuser.Query, page, pageSize int) ([]domainuser.User, int64, error) {
	return nil, 0, nil
}
func (r *fakeUserRepo) UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error {
	r.updatedPwd[id] = hashedPwd
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
func (r *fakeRoleRepo) List(ctx context.Context, q domainrole.Query, page, pageSize int) ([]domainrole.Role, int64, error) {
	return nil, 0, nil
}
func (r *fakeRoleRepo) FindByUserID(ctx context.Context, userID uint64) ([]domainrole.Role, error) {
	return r.byUser[userID], nil
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

type fakeMenuRepo struct {
	permCodes []string
}

func (r *fakeMenuRepo) Create(ctx context.Context, menu *domainmenu.Menu) error { return nil }
func (r *fakeMenuRepo) Update(ctx context.Context, menu *domainmenu.Menu) error { return nil }
func (r *fakeMenuRepo) Delete(ctx context.Context, id uint64) error             { return nil }
func (r *fakeMenuRepo) FindByID(ctx context.Context, id uint64) (*domainmenu.Menu, error) {
	return nil, nil
}
func (r *fakeMenuRepo) List(ctx context.Context) ([]domainmenu.Menu, error) { return nil, nil }
func (r *fakeMenuRepo) FindPermCodesByRoleIDs(ctx context.Context, roleIDs []uint64) ([]string, error) {
	return r.permCodes, nil
}

func (r *fakeMenuRepo) FindIDsByRoleIDs(ctx context.Context, roleIDs []uint64) ([]uint64, error) {
	return nil, nil
}

// ---- 测试辅助 ----

func hashPwd(t *testing.T, pwd string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return string(h)
}

func ctxWithUser(userID uint64, isSuper bool) context.Context {
	return kitauth.WithCurrentUser(context.Background(), &kitauth.CurrentUser{
		UserID:   userID,
		Username: "tester",
		IsSuper:  isSuper,
	})
}

func newTestTokenManager() *kitauth.TokenManager {
	return kitauth.NewTokenManager(kitauth.Config{Secret: "test", ExpireMinutes: 60})
}

// ---- AuthService ----

func TestAuthServiceLogin(t *testing.T) {
	tokens := newTestTokenManager()

	newSvc := func(users ...*domainuser.User) *AuthService {
		userRepo := newFakeUserRepo(users...)
		resolver := NewPermResolver(userRepo, &fakeAssignRepo{}, &fakeMenuRepo{}, cache.NewMemory())
		return NewAuthService(userRepo, &fakeRoleRepo{}, resolver, tokens)
	}

	t.Run("登录成功", func(t *testing.T) {
		svc := newSvc(&domainuser.User{ID: 1, Username: "alice", Password: hashPwd(t, "pwd123"), Status: 1})
		resp, err := svc.Login(context.Background(), LoginReq{Username: "alice", Password: "pwd123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		claims, err := tokens.Parse(resp.Token)
		if err != nil {
			t.Fatalf("Parse token: %v", err)
		}
		if claims.UserID != 1 || claims.Username != "alice" {
			t.Errorf("claims mismatch: %+v", claims)
		}
	})

	t.Run("密码错误", func(t *testing.T) {
		svc := newSvc(&domainuser.User{ID: 1, Username: "alice", Password: hashPwd(t, "pwd123"), Status: 1})
		_, err := svc.Login(context.Background(), LoginReq{Username: "alice", Password: "bad"})
		if !errors.Is(err, shared.ErrInvalidCredential) {
			t.Errorf("err = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("用户不存在", func(t *testing.T) {
		svc := newSvc()
		_, err := svc.Login(context.Background(), LoginReq{Username: "ghost", Password: "pwd123"})
		if !errors.Is(err, shared.ErrInvalidCredential) {
			t.Errorf("err = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("用户停用", func(t *testing.T) {
		svc := newSvc(&domainuser.User{ID: 1, Username: "alice", Password: hashPwd(t, "pwd123"), Status: 0})
		_, err := svc.Login(context.Background(), LoginReq{Username: "alice", Password: "pwd123"})
		if !errors.Is(err, shared.ErrInvalidCredential) {
			t.Errorf("err = %v, want ErrInvalidCredential", err)
		}
	})
}

func TestAuthServiceChangePassword(t *testing.T) {
	setup := func() (*AuthService, *fakeUserRepo) {
		userRepo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Password: hashPwd(t, "old-pwd"), Status: 1})
		resolver := NewPermResolver(userRepo, &fakeAssignRepo{}, &fakeMenuRepo{}, cache.NewMemory())
		return NewAuthService(userRepo, &fakeRoleRepo{}, resolver, newTestTokenManager()), userRepo
	}

	t.Run("修改成功", func(t *testing.T) {
		svc, userRepo := setup()
		err := svc.ChangePassword(ctxWithUser(1, false), ChangePwdReq{OldPassword: "old-pwd", NewPassword: "new-pwd"})
		if err != nil {
			t.Fatalf("ChangePassword: %v", err)
		}
		hashed := userRepo.updatedPwd[1]
		if hashed == "" {
			t.Fatal("UpdatePassword 未被调用")
		}
		if bcrypt.CompareHashAndPassword([]byte(hashed), []byte("new-pwd")) != nil {
			t.Error("落库的哈希与新密码不匹配")
		}
	})

	t.Run("旧密码错误", func(t *testing.T) {
		svc, userRepo := setup()
		err := svc.ChangePassword(ctxWithUser(1, false), ChangePwdReq{OldPassword: "wrong", NewPassword: "new-pwd"})
		if !errors.Is(err, shared.ErrInvalidCredential) {
			t.Errorf("err = %v, want ErrInvalidCredential", err)
		}
		if len(userRepo.updatedPwd) != 0 {
			t.Error("旧密码错误时不应更新密码")
		}
	})

	t.Run("用户不存在", func(t *testing.T) {
		svc, _ := setup()
		err := svc.ChangePassword(ctxWithUser(999, false), ChangePwdReq{OldPassword: "old-pwd", NewPassword: "new-pwd"})
		if !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("未登录上下文", func(t *testing.T) {
		svc, _ := setup()
		err := svc.ChangePassword(context.Background(), ChangePwdReq{OldPassword: "old-pwd", NewPassword: "new-pwd"})
		if !errors.Is(err, shared.ErrInvalidCredential) {
			t.Errorf("err = %v, want ErrInvalidCredential", err)
		}
	})
}

func TestAuthServiceProfile(t *testing.T) {
	setup := func() *AuthService {
		userRepo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Nickname: "Alice", Status: 1})
		roleRepo := &fakeRoleRepo{byUser: map[uint64][]domainrole.Role{
			1: {{ID: 10, Code: "admin"}, {ID: 11, Code: "ops"}},
		}}
		assignRepo := &fakeAssignRepo{roleIDsByUser: map[uint64][]uint64{1: {10, 11}}}
		menuRepo := &fakeMenuRepo{permCodes: []string{"system:user:list", "system:user:create"}}
		resolver := NewPermResolver(userRepo, assignRepo, menuRepo, cache.NewMemory())
		return NewAuthService(userRepo, roleRepo, resolver, newTestTokenManager())
	}

	t.Run("查询成功", func(t *testing.T) {
		svc := setup()
		resp, err := svc.Profile(ctxWithUser(1, false))
		if err != nil {
			t.Fatalf("Profile: %v", err)
		}
		if resp.ID != 1 || resp.Username != "alice" || resp.Nickname != "Alice" {
			t.Errorf("profile mismatch: %+v", resp)
		}
		if len(resp.Roles) != 2 || resp.Roles[0] != "admin" || resp.Roles[1] != "ops" {
			t.Errorf("roles = %v", resp.Roles)
		}
		// 权限点应按字典序排序
		want := []string{"system:user:create", "system:user:list"}
		if len(resp.Perms) != len(want) {
			t.Fatalf("perms = %v", resp.Perms)
		}
		for i := range want {
			if resp.Perms[i] != want[i] {
				t.Errorf("perms = %v, want %v", resp.Perms, want)
				break
			}
		}
	})

	t.Run("未登录上下文", func(t *testing.T) {
		svc := setup()
		_, err := svc.Profile(context.Background())
		if !errors.Is(err, shared.ErrInvalidCredential) {
			t.Errorf("err = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("用户不存在", func(t *testing.T) {
		svc := setup()
		_, err := svc.Profile(ctxWithUser(999, false))
		if !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

// ---- LocalUserProvider ----

func TestLocalUserProviderGetUser(t *testing.T) {
	repo := newFakeUserRepo(&domainuser.User{ID: 1, Username: "alice", Nickname: "Alice", DeptID: 7, Status: 1})
	p := NewLocalUserProvider(repo)

	info, err := p.GetUser(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if info == nil || info.ID != 1 || info.Username != "alice" || info.Nickname != "Alice" || info.DeptID != 7 {
		t.Errorf("user info mismatch: %+v", info)
	}

	// 不存在返回 (nil, nil)，不视为错误
	info, err = p.GetUser(context.Background(), 999)
	if err != nil || info != nil {
		t.Errorf("missing user should return (nil, nil), got (%v, %v)", info, err)
	}
}

func TestLocalUserProviderGetUsers(t *testing.T) {
	repo := newFakeUserRepo(
		&domainuser.User{ID: 1, Username: "alice"},
		&domainuser.User{ID: 2, Username: "bob"},
	)
	p := NewLocalUserProvider(repo)

	users, err := p.GetUsers(context.Background(), []uint64{1, 2, 999})
	if err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[1].Username != "alice" || users[2].Username != "bob" {
		t.Errorf("users mismatch: %+v", users)
	}
}
