package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"

	authapp "goKit/internal/application/auth"
	"goKit/internal/application/shared"
	userapp "goKit/internal/application/user"
	domainmenu "goKit/internal/domain/menu"
	domainrole "goKit/internal/domain/role"
	"goKit/internal/domain/shared/datascope"
	domainuser "goKit/internal/domain/user"
	"goKit/internal/interface/http/middleware"
	"goKit/internal/interface/http/response"
	kitauth "goKit/pkg/kit/auth"
	"goKit/pkg/kit/cache"
)

// ---- 手写 fake 仓储与授权器 ----

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
	for _, u := range r.byID {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, nil
}
func (r *fakeUserRepo) FindByIDs(ctx context.Context, ids []uint64) ([]domainuser.User, error) {
	return nil, nil
}
func (r *fakeUserRepo) List(ctx context.Context, filter datascope.Filter, q domainuser.Query, page, pageSize int) ([]domainuser.User, int64, error) {
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
func (r *fakeRoleRepo) List(ctx context.Context, q domainrole.Query, page, pageSize int) ([]domainrole.Role, int64, error) {
	return nil, 0, nil
}
func (r *fakeRoleRepo) FindByUserID(ctx context.Context, userID uint64) ([]domainrole.Role, error) {
	return r.byUser[userID], nil
}

type fakeAssignRepo struct{}

func (r *fakeAssignRepo) SetUserRoles(ctx context.Context, userID uint64, roleIDs []uint64) error {
	return nil
}
func (r *fakeAssignRepo) GetRoleIDsByUser(ctx context.Context, userID uint64) ([]uint64, error) {
	return []uint64{10}, nil
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

type fakeAuthorizer struct {
	allow bool
}

func (a *fakeAuthorizer) CheckPerm(ctx context.Context, userID uint64, permCode string) (bool, error) {
	return a.allow, nil
}

// ---- 测试环境组装 ----

type testEnv struct {
	app    *fiber.App
	tokens *kitauth.TokenManager
	az     *fakeAuthorizer
}

// setupApp 用 fake 仓储组装真实 service 与 handler，按 routes.go 的方式挂载关键路由
func setupApp(t *testing.T) *testEnv {
	t.Helper()

	hashed, err := bcrypt.GenerateFromPassword([]byte("pwd123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userRepo := &fakeUserRepo{byID: map[uint64]*domainuser.User{
		1: {ID: 1, Username: "alice", Password: string(hashed), Nickname: "Alice", Status: 1},
		2: {ID: 2, Username: "admin", Password: string(hashed), Status: 1, IsSuper: true},
	}}
	roleRepo := &fakeRoleRepo{byUser: map[uint64][]domainrole.Role{1: {{ID: 10, Code: "ops"}}}}
	assignRepo := &fakeAssignRepo{}
	menuRepo := &fakeMenuRepo{permCodes: []string{"system:user:list"}}

	resolver := authapp.NewPermResolver(userRepo, assignRepo, menuRepo, cache.NewMemory())
	tokens := kitauth.NewTokenManager(kitauth.Config{Secret: "test-secret", ExpireMinutes: 60})
	authSvc := authapp.NewAuthService(userRepo, roleRepo, resolver, tokens)
	scope := shared.NewDataScopeHelper(userRepo, roleRepo, nil, assignRepo)
	userSvc := userapp.NewUserService(userRepo, assignRepo, scope, resolver, nil)

	authH := NewAuthHandler(authSvc)
	userH := NewUserHandler(userSvc)
	az := &fakeAuthorizer{allow: true}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := fiber.New()
	app.Use(middleware.ErrorHandler(logger))

	v1 := app.Group("/api/v1")
	v1.Post("/auth/login", authH.Login)

	secured := v1.Group("", middleware.JWTAuth(tokens))
	secured.Get("/auth/profile", authH.Profile)
	secured.Post("/users", middleware.RequirePerm(az, "system:user:create"), userH.Create)
	secured.Get("/users/:id", middleware.RequirePerm(az, "system:user:list"), userH.Get)

	return &testEnv{app: app, tokens: tokens, az: az}
}

type baseResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// do 发起请求并解析统一响应结构
func (e *testEnv) do(t *testing.T, method, path string, body any, token string) (int, baseResp) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := e.app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	var br baseResp
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, br
}

func (e *testEnv) mustToken(t *testing.T, userID uint64, username string, isSuper bool) string {
	t.Helper()
	token, err := e.tokens.Generate(userID, username, isSuper)
	if err != nil {
		t.Fatalf("Generate token: %v", err)
	}
	return token
}

// ---- 请求级测试 ----

func TestHTTPLogin(t *testing.T) {
	env := setupApp(t)

	t.Run("密码错误返回 401 业务码", func(t *testing.T) {
		status, br := env.do(t, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"username": "alice", "password": "wrong"}, "")
		if status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
		if br.Code != response.CodeUnauthorized {
			t.Errorf("code = %d, want %d", br.Code, response.CodeUnauthorized)
		}
	})

	t.Run("登录成功返回 token", func(t *testing.T) {
		status, br := env.do(t, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"username": "alice", "password": "pwd123"}, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if br.Code != response.CodeSuccess {
			t.Errorf("code = %d, want %d", br.Code, response.CodeSuccess)
		}
		var data struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(br.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		claims, err := env.tokens.Parse(data.Token)
		if err != nil {
			t.Fatalf("返回的 token 无法解析: %v", err)
		}
		if claims.UserID != 1 || claims.Username != "alice" {
			t.Errorf("claims mismatch: %+v", claims)
		}
	})

	t.Run("缺少参数返回参数错误", func(t *testing.T) {
		status, br := env.do(t, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"username": "alice"}, "")
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		if br.Code != response.CodeParamError {
			t.Errorf("code = %d, want %d", br.Code, response.CodeParamError)
		}
		if !strings.Contains(br.Message, "password") {
			t.Errorf("message = %q, 应指出 password 字段", br.Message)
		}
	})
}

func TestHTTPValidation(t *testing.T) {
	env := setupApp(t)
	token := env.mustToken(t, 1, "alice", false)

	t.Run("创建用户缺 username 返回 400", func(t *testing.T) {
		status, br := env.do(t, http.MethodPost, "/api/v1/users",
			map[string]string{"password": "pwd123456"}, token)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		if br.Code != response.CodeParamError {
			t.Errorf("code = %d, want %d", br.Code, response.CodeParamError)
		}
		if !strings.Contains(br.Message, "username") {
			t.Errorf("message = %q, 应指出 username 字段", br.Message)
		}
	})

	t.Run("创建用户密码过短返回 400", func(t *testing.T) {
		status, br := env.do(t, http.MethodPost, "/api/v1/users",
			map[string]string{"username": "alice2", "password": "short"}, token)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		if br.Code != response.CodeParamError {
			t.Errorf("code = %d, want %d", br.Code, response.CodeParamError)
		}
	})

	t.Run("创建用户 email 格式非法返回 400", func(t *testing.T) {
		status, br := env.do(t, http.MethodPost, "/api/v1/users",
			map[string]string{"username": "alice2", "password": "pwd123456", "email": "not-an-email"}, token)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		if br.Code != response.CodeParamError {
			t.Errorf("code = %d, want %d", br.Code, response.CodeParamError)
		}
	})
}

func TestHTTPProtectedRoutes(t *testing.T) {
	env := setupApp(t)

	t.Run("无 token 访问受保护路由返回 401", func(t *testing.T) {
		status, br := env.do(t, http.MethodGet, "/api/v1/auth/profile", nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
		if br.Code != response.CodeUnauthorized {
			t.Errorf("code = %d, want %d", br.Code, response.CodeUnauthorized)
		}
	})

	t.Run("非法 token 返回 401", func(t *testing.T) {
		status, _ := env.do(t, http.MethodGet, "/api/v1/auth/profile", nil, "not-a-token")
		if status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})

	t.Run("有 token 无权限返回 403", func(t *testing.T) {
		env.az.allow = false
		defer func() { env.az.allow = true }()
		token := env.mustToken(t, 1, "alice", false)
		status, br := env.do(t, http.MethodGet, "/api/v1/users/1", nil, token)
		if status != http.StatusForbidden {
			t.Errorf("status = %d, want 403", status)
		}
		if br.Code != response.CodeForbidden {
			t.Errorf("code = %d, want %d", br.Code, response.CodeForbidden)
		}
	})

	t.Run("profile 正常返回 200", func(t *testing.T) {
		token := env.mustToken(t, 1, "alice", false)
		status, br := env.do(t, http.MethodGet, "/api/v1/auth/profile", nil, token)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if br.Code != response.CodeSuccess {
			t.Errorf("code = %d, want %d", br.Code, response.CodeSuccess)
		}
		var data struct {
			ID       uint64   `json:"id"`
			Username string   `json:"username"`
			Roles    []string `json:"roles"`
			Perms    []string `json:"perms"`
		}
		if err := json.Unmarshal(br.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if data.ID != 1 || data.Username != "alice" {
			t.Errorf("profile mismatch: %+v", data)
		}
		if len(data.Roles) != 1 || data.Roles[0] != "ops" {
			t.Errorf("roles = %v, want [ops]", data.Roles)
		}
		if len(data.Perms) != 1 || data.Perms[0] != "system:user:list" {
			t.Errorf("perms = %v, want [system:user:list]", data.Perms)
		}
	})

	t.Run("users/:id 正常返回 200", func(t *testing.T) {
		token := env.mustToken(t, 1, "alice", false)
		status, br := env.do(t, http.MethodGet, "/api/v1/users/1", nil, token)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if br.Code != response.CodeSuccess {
			t.Errorf("code = %d, want %d", br.Code, response.CodeSuccess)
		}
		var data struct {
			ID       uint64   `json:"id"`
			Username string   `json:"username"`
			RoleIDs  []uint64 `json:"role_ids"`
		}
		if err := json.Unmarshal(br.Data, &data); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if data.ID != 1 || data.Username != "alice" {
			t.Errorf("user mismatch: %+v", data)
		}
		if len(data.RoleIDs) != 1 || data.RoleIDs[0] != 10 {
			t.Errorf("role_ids = %v, want [10]", data.RoleIDs)
		}
	})

	t.Run("users/:id 非法 ID 返回参数错误", func(t *testing.T) {
		token := env.mustToken(t, 1, "alice", false)
		status, br := env.do(t, http.MethodGet, "/api/v1/users/abc", nil, token)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		if br.Code != response.CodeParamError {
			t.Errorf("code = %d, want %d", br.Code, response.CodeParamError)
		}
	})

	t.Run("users/:id 不存在返回 404 业务码", func(t *testing.T) {
		token := env.mustToken(t, 1, "alice", false)
		status, br := env.do(t, http.MethodGet, "/api/v1/users/999", nil, token)
		if status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
		if br.Code != response.CodeNotFound {
			t.Errorf("code = %d, want %d", br.Code, response.CodeNotFound)
		}
	})
}
