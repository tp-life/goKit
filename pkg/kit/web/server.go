package web

import (
	"context"
	"log/slog"

	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"go.uber.org/fx"
)

// ServerParams 定义注入参数
type ServerParams struct {
	fx.In

	Config Config
	Logger *slog.Logger
	// 使用 group 标签，Fx 会自动收集所有标记为 "http_global_middleware" 的 handler
	Middlewares []fiber.Handler `group:"http_global_middleware"`
}

func NewServer(params ServerParams) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:       params.Config.AppName,
		Prefork:       params.Config.Prefork,
		JSONEncoder:   sonic.Marshal,
		JSONDecoder:   sonic.Unmarshal,
		StrictRouting: true,
		CaseSensitive: true,
	})

	// 1. 内置基础中间件
	app.Use(recover.New())
	app.Use(requestid.New(requestid.Config{ContextKey: "requestid"}))

	// 2. 挂载用户注入的全局中间件 (CORS, Limiter, Auth 等)
	for _, m := range params.Middlewares {
		app.Use(m)
	}

	return app
}

func StartLifecycle(lc fx.Lifecycle, app *fiber.App, cfg Config, l *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				l.Info("http_server_start", slog.String("addr", cfg.Port))
				if err := app.Listen(cfg.Port); err != nil {
					l.Error("http_server_error", slog.Any("err", err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			l.Info("http_server_stop")
			return app.Shutdown()
		},
	})
}

// AsMiddlewares 注册流式拦截器
func AsMiddlewares(f any) any {
	return fx.Annotate(f, fx.ResultTags(`group:"http_global_middleware"`))
}
