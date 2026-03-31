package router

import (
	handler "goKit/internal/interface/http/handler"
	"goKit/internal/interface/http/middleware"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/fx"
)

// Router 统管所有 HTTP 路由
type Router struct {
	params RouterIn
}

type RouterIn struct {
	fx.In
	Logger     *slog.Logger
	Polymarket *handler.PolymarketHandler `optional:"true"`
}

// NewRouter 通过 Fx 依赖注入所有的 Handler
func NewRouter(par RouterIn) *Router {
	return &Router{
		params: par,
	}
}

// Register 统一注册路由树
func (r *Router) Register(app *fiber.App) {
	v1 := app.Group("/api/v1")
	v1.Use(middleware.ErrorHandler(r.params.Logger))
	app.Use(middleware.ErrorHandler(r.params.Logger))

	if r.params.Polymarket != nil {
		app.Get("/", r.params.Polymarket.Dashboard)
		app.Get("/api/status", r.params.Polymarket.Status)
		app.Get("/api/logs", r.params.Polymarket.Logs)
		app.Get("/api/history", r.params.Polymarket.History)
		app.Get("/api/stream", r.params.Polymarket.Stream)
		app.Post("/api/manual-order", r.params.Polymarket.ManualOrder)
	}
}
