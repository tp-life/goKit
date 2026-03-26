package router

import (
	"log/slog"

	"goKit/internal/interface/http/handler"
	"goKit/internal/interface/http/middleware"
	"goKit/pkg/kit/web"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/fx"
)

type Router struct {
	params RouterIn
}

type RouterIn struct {
	fx.In
	Logger               *slog.Logger
	WebConfig            web.Config
	HealthHandler        *handler.HealthHandler
	SymbolHandler        *handler.SymbolHandler
	OpportunityHandler   *handler.OpportunityHandler
	ExecutionPlanHandler *handler.ExecutionPlanHandler
	ExecutionHandler     *handler.ExecutionHandler
	MarketHandler        *handler.MarketHandler
	SystemHandler        *handler.SystemHandler
	SnapshotStatsHandler *handler.SnapshotStatsHandler
}

func NewRouter(par RouterIn) *Router {
	return &Router{params: par}
}

func RegisterRoutes(app *fiber.App, router *Router) {
	router.Register(app)
}

func (r *Router) Register(app *fiber.App) {
	v1 := app.Group("/api/v1")
	v1.Use(middleware.ErrorHandler(r.params.Logger))
	v1.Get("/health", r.params.HealthHandler.Get)
	v1.Get("/symbols", r.params.SymbolHandler.List)
	v1.Get("/opportunities/summary", r.params.OpportunityHandler.ListSummary)
	v1.Get("/opportunities/:id", r.params.OpportunityHandler.Detail)
	v1.Get("/opportunities", r.params.OpportunityHandler.List)
	v1.Get("/plans", r.params.ExecutionPlanHandler.List)
	v1.Get("/executions", r.params.ExecutionHandler.List)
	v1.Get("/executions/auto-close-candidates", r.params.ExecutionHandler.AutoCloseCandidates)
	v1.Get("/executions/live-positions", r.params.ExecutionHandler.LivePositions)
	v1.Get("/executions/:planKey/orders", r.params.ExecutionHandler.Orders)
	v1.Get("/market/:symbol", r.params.MarketHandler.Snapshot)
	v1.Get("/system/status", r.params.SystemHandler.Status)
	v1.Get("/snapshot-stats", r.params.SnapshotStatsHandler.Get)

	protected := v1.Group("", middleware.RequireExecutionAuth(r.params.WebConfig))
	protected.Post("/executions/:planKey/open", r.params.ExecutionHandler.Open)
	protected.Post("/executions/:planKey/close", r.params.ExecutionHandler.Close)
	protected.Post("/executions/auto-close-sweep", r.params.ExecutionHandler.SweepAutoClose)
	protected.Post("/executions/events/order", r.params.ExecutionHandler.InjectOrderEvent)
}
