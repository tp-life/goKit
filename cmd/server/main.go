package main

import (
	"goKit/internal"
	"goKit/internal/application/service"
	"goKit/internal/infrastructure/exchange"
	httpInterface "goKit/internal/interface/http/router"
	"goKit/pkg/kit"
	"goKit/pkg/kit/db"
	appLog "goKit/pkg/kit/log"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"
	frontend "goKit/web"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"go.uber.org/fx"
)

type AppConfig struct {
	Web       web.Config         `mapstructure:"web"`
	RPC       rpc.Config         `mapstructure:"rpc"`
	Database  db.Config          `mapstructure:"database"`
	Log       appLog.Config      `mapstructure:"log"`
	Strategy  service.Config     `mapstructure:"strategy"`
	Exchanges exchange.ConfigSet `mapstructure:"exchanges"`
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
		fx.Provide(LoadConfig),
		fx.Provide(func(cfg *AppConfig) web.Config { return cfg.Web }),
		fx.Provide(func(cfg *AppConfig) rpc.Config { return cfg.RPC }),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),
		fx.Provide(func(cfg *AppConfig) appLog.Config { return cfg.Log }),
		fx.Provide(func(cfg *AppConfig) service.Config { return cfg.Strategy }),
		fx.Provide(func(cfg *AppConfig) exchange.ConfigSet { return cfg.Exchanges }),
		fx.Provide(
			web.AsMiddlewares(func() fiber.Handler {
				return cors.New()
			}),
		),
		kit.Module,
		internal.Module,
		// === 4. 启动时执行路由注册 ===
		fx.Invoke(func(app *fiber.App, router *httpInterface.Router) {
			// 先注册后端 API 路由，再注册嵌入式前端页面。
			// 前端只占用 / 和 /assets/*，不会影响 /api/v1 的业务接口。
			router.Register(app)
			frontend.RegisterRoutes(app)
		}),
	).Run()
}
