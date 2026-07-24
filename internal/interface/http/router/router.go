package router

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/fx"

	"goKit/internal/interface/http"
	"goKit/internal/interface/http/middleware"
)

// Router 统管所有 HTTP 路由
type Router struct {
	params RouterIn
}

type RouterIn struct {
	fx.In
	Logger *slog.Logger
	System *http.HTTPModule
}

// NewRouter 通过 Fx 依赖注入所有的 Handler
func NewRouter(par RouterIn) *Router {
	return &Router{
		params: par,
	}
}

// Register 统一注册路由树
func (r *Router) Register(app *fiber.App) {
	// 全局 API 分组
	v1 := app.Group("/api/v1")
	v1.Use(middleware.ErrorHandler(r.params.Logger))

	// 健康检查（公开）
	v1.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// 挂载 system 模块路由（登录公开，其余 JWT + 权限点）
	r.params.System.RegisterRoutes(v1)
}
