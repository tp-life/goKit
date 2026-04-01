package main

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
	"go.uber.org/fx"

	"goKit/internal/application/service"
	infraPolymarket "goKit/internal/infrastructure/polymarket"
	httphandler "goKit/internal/interface/http/handler"
	httpInterface "goKit/internal/interface/http/router"
	tuiInterface "goKit/internal/interface/tui"

	"goKit/pkg/kit"
	"goKit/pkg/kit/db"
	kitlog "goKit/pkg/kit/log"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"
)

type AppConfig struct {
	Web      web.Config          `mapstructure:"web"`
	RPC      rpc.Config          `mapstructure:"rpc"`
	Database db.Config           `mapstructure:"database"`
	Log      kitlog.Config       `mapstructure:"log"`
	UI       tuiInterface.Config `mapstructure:"ui"`
}

func LoadConfig() (*AppConfig, error) {
	// 优先读取 configs/polymarket.env；如果不存在，再把根目录 .env 当作补充来源。
	// 不能把两个路径一次性传给 godotenv.Load：
	// 当前依赖会在第一个文件不存在时直接返回，后面的回退文件不会继续加载。
	_ = loadEnvIfExists(".env")

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./configs")

	v.SetDefault("web.port", ":5080")
	v.SetDefault("web.app_name", "PolymarketBot")
	v.SetDefault("web.enabled", true)
	v.SetDefault("web.prefork", false)
	v.SetDefault("rpc.port", ":9090")
	v.SetDefault("rpc.max_connection_idle", "300s")
	v.SetDefault("rpc.timeout", "5s")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.source", false)
	v.SetDefault("ui.mode", "web")
	v.SetDefault("ui.refresh_interval_ms", 1000)
	v.SetDefault("database.driver", "mysql")
	v.SetDefault("database.replicas", []string{})
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.max_open_conns", 100)
	v.SetDefault("database.conn_max_lifetime", "1h")
	v.SetDefault("database.log_mode", "error")
	v.SetDefault("database.slow_threshold", "200ms")

	if err := v.ReadInConfig(); err != nil {
		var configErr viper.ConfigFileNotFoundError
		if !errors.As(err, &configErr) {
			return nil, err
		}
	}

	var cfg AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// 允许把 UI 模式直接写在 polymarket.env 里，远程部署时切换更方便。
	if rawMode := strings.TrimSpace(os.Getenv("POLYMARKET_UI_MODE")); rawMode != "" {
		cfg.UI.Mode = rawMode
	}
	if rawRefresh := strings.TrimSpace(os.Getenv("POLYMARKET_TUI_REFRESH_INTERVAL_MS")); rawRefresh != "" {
		if parsed, err := strconv.Atoi(rawRefresh); err == nil && parsed > 0 {
			cfg.UI.RefreshIntervalMS = parsed
		}
	}

	cfg.UI.Normalize()
	cfg.Web.Enabled = cfg.UI.WebEnabled()
	if strings.TrimSpace(cfg.Log.Output) == "" {
		if cfg.UI.TUIEnabled() {
			cfg.Log.Output = "discard"
		} else {
			cfg.Log.Output = "stdout"
		}
	}
	return &cfg, nil
}

// loadEnvIfExists 在文件存在时才加载环境变量，避免缺失文件阻断回退链路。
func loadEnvIfExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return godotenv.Load(path)
}

func main() {
	fx.New(
		fx.Provide(LoadConfig),
		fx.Provide(
			web.AsMiddlewares(func() fiber.Handler {
				return cors.New() // 使用 fiber/middleware/cors
			}),
		),
		fx.Provide(func(cfg *AppConfig) kitlog.Config { return cfg.Log }),
		fx.Provide(func(cfg *AppConfig) web.Config { return cfg.Web }),
		fx.Provide(func(cfg *AppConfig) rpc.Config { return cfg.RPC }),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),
		fx.Provide(func(cfg *AppConfig) tuiInterface.Config { return cfg.UI }),
		fx.Provide(infraPolymarket.LoadConfig),

		kit.Module,

		fx.Provide(service.NewPolymarketManager),
		fx.Provide(func(manager *service.PolymarketManager) service.PolymarketApp { return manager }),
		fx.Invoke(service.RegisterPolymarketLifecycle),
		fx.Provide(httphandler.NewPolymarketHandler),
		fx.Provide(tuiInterface.NewProgram),
		fx.Invoke(tuiInterface.RegisterLifecycle),

		fx.Provide(httpInterface.NewRouter),
		fx.Invoke(func(app *fiber.App, router *httpInterface.Router) {
			router.Register(app)
		}),
	).Run()
}
