package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	grpcauth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"google.golang.org/grpc"

	authzv1 "goKit/api/gen/authz/v1"
	"goKit/internal/app"
	authapp "goKit/internal/application/auth"
	"goKit/internal/infrastructure/persistence"
	"goKit/internal/infrastructure/seed"
	sysgrpc "goKit/internal/interface/grpc"
	"goKit/internal/interface/http/middleware"
	httpInterface "goKit/internal/interface/http/router"
	"goKit/migrations"

	"goKit/pkg/kit"
	"goKit/pkg/kit/auth"
	"goKit/pkg/kit/cache"
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/log"
	"goKit/pkg/kit/obs"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"
)

type SeedConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	LoginMax    int           `mapstructure:"login_max"`    // 登录窗口内最大次数，默认 10
	LoginWindow time.Duration `mapstructure:"login_window"` // 登录限流窗口，默认 1m
}

type AppConfig struct {
	Web       web.Config       `mapstructure:"web"`
	RPC       rpc.Config       `mapstructure:"rpc"`
	Database  db.Config        `mapstructure:"database"`
	Log       log.Config       `mapstructure:"log"`
	JWT       auth.Config      `mapstructure:"jwt"`
	Authz     auth.AuthzConfig `mapstructure:"authz"`
	Trace     obs.TraceConfig  `mapstructure:"trace"`
	RateLimit RateLimitConfig  `mapstructure:"rate_limit"`
	Seed      SeedConfig       `mapstructure:"seed"`
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
func ProvideAuthorizer(cfg *AppConfig, local *authapp.LocalAuthorizer) (auth.Authorizer, error) {
	if cfg.Authz.Mode == "remote" {
		return auth.NewRemoteAuthorizer(cfg.Authz.Addr, cfg.Authz.Token)
	}
	return local, nil
}

// ProvideUserProvider 按配置选择用户信息提供者实现，规则同 ProvideAuthorizer。
// 其他模块需要用户信息时只依赖 auth.UserProvider 端口，无需 import 业务模块内部。
func ProvideUserProvider(cfg *AppConfig, local *authapp.LocalUserProvider) (auth.UserProvider, error) {
	if cfg.Authz.Mode == "remote" {
		return auth.NewRemoteUserProvider(cfg.Authz.Addr, cfg.Authz.Token)
	}
	return local, nil
}

func main() {
	fx.New(
		fx.Provide(LoadConfig),
		fx.Provide(
			web.AsMiddlewares(func() fiber.Handler {
				return cors.New() // 使用 fiber/middleware/cors
			}),
		),
		fx.Provide(func(cfg *AppConfig) web.Config { return cfg.Web }),
		fx.Provide(func(cfg *AppConfig) rpc.Config { return cfg.RPC }),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),
		fx.Provide(func(cfg *AppConfig) log.Config { return cfg.Log }),
		fx.Provide(func(cfg *AppConfig) auth.Config { return cfg.JWT }),
		fx.Provide(func(cfg *AppConfig) obs.TraceConfig { return cfg.Trace }),
		fx.Provide(auth.NewTokenManager),
		fx.Provide(cache.NewMemory),
		fx.Provide(ProvideAuthorizer),
		fx.Provide(ProvideUserProvider),
		// gRPC 服务间认证：authz.token 非空时启用 Bearer 校验（返回 nil 则 rpc.Server 跳过认证）
		fx.Provide(func(cfg *AppConfig) grpcauth.AuthFunc {
			return auth.NewServiceTokenAuthFunc(cfg.Authz.Token)
		}),
		// OTel 链路追踪：trace.endpoint 非空时启用 OTLP 上报
		fx.Invoke(obs.InitTracing),
		// 登录接口限流（按 IP，进程内计数）
		fx.Provide(func(cfg *AppConfig) fiber.Handler {
			return middleware.LoginRateLimiter(cfg.RateLimit.LoginMax, cfg.RateLimit.LoginWindow)
		}),

		// === 启动钩子：自动迁移 + 首次播种 ===
		// 注意：fx 按注册顺序执行 OnStart，本 Invoke 必须放在 kit.Module 之前，
		// 保证迁移/播种完成后 HTTP/gRPC 端口才开始监听，否则启动窗口内的请求会打到未建表的库上。
		fx.Invoke(func(lc fx.Lifecycle, cfg *AppConfig, client *db.Client, seeder *seed.Seeder, l *slog.Logger) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if cfg.Database.MigrationsEnabled {
						if cfg.Database.AutoMigrate {
							l.Warn("auto_migrate_and_migrations_both_enabled_prefer_versioned_migrations")
						}
						l.Info("migrations_start")
						if err := db.RunMigrations(client, migrations.FS); err != nil {
							return err
						}
					} else if cfg.Database.AutoMigrate {
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

		kit.Module,
		app.Module,

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
