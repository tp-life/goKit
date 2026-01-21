package main

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"go.uber.org/fx"

	"goKit/internal/application/service"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/persistence"
	"goKit/internal/infrastructure/storage"
	httpInterface "goKit/internal/interface/http"

	"goKit/pkg/kit"
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/log"
	"goKit/pkg/kit/web"
)

type AppConfig struct {
	Web      web.Config     `mapstructure:"web"`
	Database db.Config      `mapstructure:"database"`
	Storage  storage.Config `mapstructure:"storage"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Log      log.Config     `mapstructure:"log"`
}

type AuthConfig struct {
	JWTSecret        string `mapstructure:"jwt_secret"`
	JWTExpiryStr     string `mapstructure:"jwt_expiry"`     // 字符串格式，如 "15m"
	RefreshExpiryStr string `mapstructure:"refresh_expiry"` // 字符串格式，如 "720h"
}

func (c *AuthConfig) ToServiceConfig() service.AuthConfig {
	// 默认 JWT token 过期时间：7天（更长的有效期，减少频繁登录）
	jwtExpiry := 7 * 24 * time.Hour
	if c.JWTExpiryStr != "" {
		if d, err := time.ParseDuration(c.JWTExpiryStr); err == nil {
			jwtExpiry = d
		}
	}

	// 默认 Refresh Token 过期时间：30天
	refreshExpiry := 30 * 24 * time.Hour
	if c.RefreshExpiryStr != "" {
		if d, err := time.ParseDuration(c.RefreshExpiryStr); err == nil {
			refreshExpiry = d
		}
	}

	return service.AuthConfig{
		JWTSecret:     c.JWTSecret,
		JWTExpiry:     jwtExpiry,
		RefreshExpiry: refreshExpiry,
	}
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

	// 设置 log 配置默认值（如果未配置）
	if cfg.Log.Level == "" {
		cfg.Log = log.DefaultConfig()
	}

	return &cfg, nil
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
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),
		fx.Provide(func(cfg *AppConfig) log.Config { return cfg.Log }),

		kit.Module,

		// Repositories
		fx.Provide(persistence.NewUserRepo),
		fx.Provide(persistence.NewSessionRepo),
		fx.Provide(persistence.NewMemoRepo),
		fx.Provide(persistence.NewPageRepo),
		fx.Provide(persistence.NewBlockRepo),
		fx.Provide(persistence.NewTrashRepo),
		fx.Provide(persistence.NewSearchRepo),
		fx.Provide(persistence.NewTagRepo),

		// Storage
		fx.Provide(func(cfg *AppConfig, logger *slog.Logger) (repository.StorageService, error) {
			return storage.NewQiniuStorage(cfg.Storage, logger)
		}),

		// Services
		fx.Provide(func(cfg *AppConfig) service.AuthConfig {
			return cfg.Auth.ToServiceConfig()
		}),
		fx.Provide(service.NewAuthService),
		fx.Provide(service.NewTagService),
		fx.Provide(service.NewMemoService),
		fx.Provide(service.NewPageService),
		fx.Provide(func(
			memoRepo repository.MemoRepository,
			pageRepo repository.PageRepository,
			blockRepo repository.BlockRepository,
		) *service.TimelineService {
			return service.NewTimelineService(memoRepo, pageRepo, blockRepo)
		}),
		fx.Provide(service.NewTrashService),
		fx.Provide(service.NewSearchService),

		// Handlers
		fx.Provide(httpInterface.NewAuthHandler),
		fx.Provide(httpInterface.NewUploadHandler),
		fx.Provide(httpInterface.NewMemoHandler),
		fx.Provide(httpInterface.NewPageHandler),
		fx.Provide(httpInterface.NewTimelineHandler),
		fx.Provide(httpInterface.NewTrashHandler),
		fx.Provide(httpInterface.NewSearchHandler),
		fx.Provide(httpInterface.NewTagHandler),

		// Register routes (集中管理)
		fx.Invoke(httpInterface.RegisterRoutes),
	).Run()
}
