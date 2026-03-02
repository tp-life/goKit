package router

import (
	"goKit/internal/interface/http/handler"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/fx"
)

// Router 统管所有 HTTP 路由
type Router struct {
	params RouterIn
}

type RouterIn struct {
	fx.In
	StrategyHandler *handler.StrategyHandler
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

	// ==========================================
	// 1. User 模块路由
	// ==========================================
	strategyGroup := v1.Group("/strategy")
	{
		strategyGroup.Get("/golden-pit", r.params.StrategyHandler.GetGoldenPit)
	}
}
