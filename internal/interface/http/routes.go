// Package http 提供 system 模块的 HTTP 路由注册
package http

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/handler"
	"goKit/internal/interface/http/middleware"
	"goKit/pkg/kit/auth"
)

// HTTPModule 聚合 system 模块全部 Handler 与依赖
type HTTPModule struct {
	auth   *handler.AuthHandler
	user   *handler.UserHandler
	role   *handler.RoleHandler
	dept   *handler.DeptHandler
	menu   *handler.MenuHandler
	tokens *auth.TokenManager
	az     auth.Authorizer
}

func NewHTTPModule(
	authH *handler.AuthHandler,
	userH *handler.UserHandler,
	roleH *handler.RoleHandler,
	deptH *handler.DeptHandler,
	menuH *handler.MenuHandler,
	tokens *auth.TokenManager,
	az auth.Authorizer,
) *HTTPModule {
	return &HTTPModule{auth: authH, user: userH, role: roleH, dept: deptH, menu: menuH, tokens: tokens, az: az}
}

// RegisterRoutes 将 system 模块路由挂载到 /api/v1 分组。
// /auth/login 公开，其余全部经过 JWT 认证，并按权限点逐一授权。
func (m *HTTPModule) RegisterRoutes(v1 fiber.Router) {
	// 公开接口
	v1.Post("/auth/login", m.auth.Login)

	// 需登录接口
	secured := v1.Group("", middleware.JWTAuth(m.tokens))
	secured.Get("/auth/profile", m.auth.Profile)
	secured.Put("/auth/password", m.auth.ChangePassword)

	perm := middleware.RequirePerm

	// 用户管理
	users := secured.Group("/users")
	users.Get("", perm(m.az, "system:user:list"), m.user.List)
	users.Post("", perm(m.az, "system:user:create"), m.user.Create)
	users.Get("/:id", perm(m.az, "system:user:list"), m.user.Get)
	users.Put("/:id", perm(m.az, "system:user:update"), m.user.Update)
	users.Delete("/:id", perm(m.az, "system:user:delete"), m.user.Delete)
	users.Put("/:id/roles", perm(m.az, "system:user:assign-role"), m.user.AssignRoles)
	users.Put("/:id/password", perm(m.az, "system:user:reset-pwd"), m.user.ResetPassword)

	// 角色管理
	roles := secured.Group("/roles")
	roles.Get("", perm(m.az, "system:role:list"), m.role.List)
	roles.Post("", perm(m.az, "system:role:create"), m.role.Create)
	roles.Get("/:id", perm(m.az, "system:role:list"), m.role.Get)
	roles.Put("/:id", perm(m.az, "system:role:update"), m.role.Update)
	roles.Delete("/:id", perm(m.az, "system:role:delete"), m.role.Delete)
	roles.Put("/:id/menus", perm(m.az, "system:role:assign-menu"), m.role.AssignMenus)
	roles.Put("/:id/users", perm(m.az, "system:role:assign-user"), m.role.AssignUsers)
	roles.Get("/:id/users", perm(m.az, "system:role:list"), m.role.GetUserIDs)

	// 部门管理
	depts := secured.Group("/depts")
	depts.Get("/tree", perm(m.az, "system:dept:list"), m.dept.Tree)
	depts.Post("", perm(m.az, "system:dept:create"), m.dept.Create)
	depts.Put("/:id", perm(m.az, "system:dept:update"), m.dept.Update)
	depts.Delete("/:id", perm(m.az, "system:dept:delete"), m.dept.Delete)

	// 菜单管理
	menus := secured.Group("/menus")
	menus.Get("/tree", perm(m.az, "system:menu:list"), m.menu.Tree)
	menus.Post("", perm(m.az, "system:menu:create"), m.menu.Create)
	menus.Put("/:id", perm(m.az, "system:menu:update"), m.menu.Update)
	menus.Delete("/:id", perm(m.az, "system:menu:delete"), m.menu.Delete)
}
