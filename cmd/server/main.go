package main

import (
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"go.uber.org/fx"

	"goKit/internal/application/service"
	"goKit/internal/infrastructure/persistence"
	httpInterface "goKit/internal/interface/http"

	"goKit/pkg/kit"
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"
)

type AppConfig struct {
	Web      web.Config `mapstructure:"web"`
	RPC      rpc.Config `mapstructure:"rpc"`
	Database db.Config  `mapstructure:"database"`
}

func LoadConfig() (*AppConfig, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	var cfg AppConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	fx.New(
		fx.Provide(func() *slog.Logger {
			return slog.New(slog.NewJSONHandler(os.Stdout, nil))
		}),
		fx.Provide(LoadConfig),
		fx.Provide(
			fx.Annotate(
				func() fiber.Handler {
					return cors.New() // 使用 fiber/middleware/cors
				},
				// 关键：打上标签，使其被 NewServer 自动识别
				fx.ResultTags(`group:"http_global_middleware"`),
			),
		),
		fx.Provide(func(cfg *AppConfig) web.Config { return cfg.Web }),
		fx.Provide(func(cfg *AppConfig) rpc.Config { return cfg.RPC }),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),

		kit.Module,

		fx.Provide(persistence.NewUserRepo),
		fx.Provide(service.NewUserService),
		fx.Provide(httpInterface.NewUserHandler),

		fx.Invoke(func(app *fiber.App, h *httpInterface.UserHandler) {
			h.RegisterRoutes(app)
		}),
	).Run()
}
