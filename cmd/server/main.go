package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"google.golang.org/grpc"

	authzv1 "goKit/api/gen/authz/v1"
	httpInterface "goKit/internal/interface/http/router"
	"goKit/internal/modules/system"
	appsvc "goKit/internal/modules/system/application/service"
	"goKit/internal/modules/system/infrastructure/persistence"
	"goKit/internal/modules/system/infrastructure/seed"
	sysgrpc "goKit/internal/modules/system/interface/grpc"

	"goKit/pkg/kit"
	"goKit/pkg/kit/auth"
	"goKit/pkg/kit/cache"
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"
)

type SeedConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type AppConfig struct {
	Web      web.Config       `mapstructure:"web"`
	RPC      rpc.Config       `mapstructure:"rpc"`
	Database db.Config        `mapstructure:"database"`
	JWT      auth.Config      `mapstructure:"jwt"`
	Authz    auth.AuthzConfig `mapstructure:"authz"`
	Seed     SeedConfig       `mapstructure:"seed"`
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

// ProvideAuthorizer 按配置选择授权器实现：
// local  → 单机模式，进程内查库+缓存
// remote → 微服务模式，gRPC 调用系统服务的 AuthzService
func ProvideAuthorizer(cfg *AppConfig, local *appsvc.LocalAuthorizer) (auth.Authorizer, error) {
	if cfg.Authz.Mode == "remote" {
		return auth.NewRemoteAuthorizer(cfg.Authz.Addr)
	}
	return local, nil
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
		fx.Provide(func(cfg *AppConfig) auth.Config { return cfg.JWT }),
		fx.Provide(auth.NewTokenManager),
		fx.Provide(cache.NewMemory),
		fx.Provide(ProvideAuthorizer),

		kit.Module,
		system.Module,

		// === 启动钩子：自动迁移 + 首次播种 ===
		fx.Invoke(func(lc fx.Lifecycle, cfg *AppConfig, client *db.Client, seeder *seed.Seeder, l *slog.Logger) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if cfg.Database.AutoMigrate {
						l.Info("auto_migrate_start")
						if err := persistence.AutoMigrate(ctx, client); err != nil {
							return err
						}
					}
					if cfg.Seed.Enabled {
						if err := seeder.Run(ctx); err != nil {
							return err
						}
					}
					return nil
				},
			})
		}),

		// === gRPC 服务注册（微服务模式下对外提供授权接口）===
		fx.Invoke(func(s *grpc.Server, authzServer *sysgrpc.AuthzServer) {
			authzv1.RegisterAuthzServiceServer(s, authzServer)
		}),

		// === 统一路由管理器 ===
		fx.Provide(httpInterface.NewRouter),

		// === 启动时执行路由注册 ===
		fx.Invoke(func(app *fiber.App, router *httpInterface.Router) {
			// 一键注册所有路由，Main 函数不再关心具体有哪些业务 Handler
			router.Register(app)
		}),
	).Run()
}
