package main

import (
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"go.uber.org/fx"

	httpInterface "goKit/internal/interface/http/router"

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
			web.AsMiddlewares(func() fiber.Handler {
				return cors.New() // 使用 fiber/middleware/cors
			}),
		),
		fx.Provide(func(cfg *AppConfig) web.Config { return cfg.Web }),
		fx.Provide(func(cfg *AppConfig) rpc.Config { return cfg.RPC }),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),

		kit.Module,

		// === 3. 统一路由管理器 ===
		fx.Provide(httpInterface.NewRouter),

		// === 4. 启动时执行路由注册 ===
		fx.Invoke(func(app *fiber.App, router *httpInterface.Router) {
			// 一键注册所有路由，Main 函数不再关心具体有哪些业务 Handler
			router.Register(app)
		}),
	).Run()
}
