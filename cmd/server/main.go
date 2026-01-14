package main

import (
	"context"
	"embed"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"go.uber.org/fx"

	"goKit/internal/application/service"
	"goKit/internal/domain/repository"
	exchangeConfig "goKit/internal/infrastructure/config"
	"goKit/internal/infrastructure/exchange"
	"goKit/internal/infrastructure/exchange/binance"
	"goKit/internal/infrastructure/exchange/hyperliquid"
	"goKit/internal/infrastructure/exchange/lighter"
	"goKit/internal/infrastructure/persistence"
	httpInterface "goKit/internal/interface/http"

	"github.com/gofiber/websocket/v2"

	"goKit/pkg/kit"
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/log"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"
)

//go:embed web/dist
var webDistFS embed.FS

type AppConfig struct {
	Web       web.Config                     `mapstructure:"web"`
	RPC       rpc.Config                     `mapstructure:"rpc"`
	Database  db.Config                      `mapstructure:"database"`
	Log       log.Config                     `mapstructure:"log"`
	Exchanges exchangeConfig.ExchangeConfig  `mapstructure:"exchanges"`
	Arbitrage exchangeConfig.ArbitrageConfig `mapstructure:"arbitrage"`
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
		// ============================================================================
		// 配置层 (Configuration Layer)
		// ============================================================================
		fx.Provide(LoadConfig),
		fx.Provide(func(cfg *AppConfig) web.Config {
			if cfg == nil {
				return web.Config{}
			}
			return cfg.Web
		}),
		fx.Provide(func(cfg *AppConfig) rpc.Config {
			if cfg == nil {
				return rpc.Config{}
			}
			return cfg.RPC
		}),
		fx.Provide(func(cfg *AppConfig) db.Config {
			if cfg == nil {
				return db.DefaultConfig()
			}
			return cfg.Database
		}),
		fx.Provide(func(cfg *AppConfig) log.Config {
			if cfg == nil {
				return log.DefaultConfig()
			}
			// 如果配置中没有 log 配置，使用默认值
			if cfg.Log.Level == "" && cfg.Log.Format == "" {
				return log.DefaultConfig()
			}
			return cfg.Log
		}),

		// ============================================================================
		// 基础设施层 (Infrastructure Layer)
		// ============================================================================
		// 核心基础设施模块（日志、数据库、Web、RPC）
		// 注意：依赖顺序很重要，logger 必须在 db 之前创建
		kit.Module,

		// HTTP 中间件
		fx.Provide(
			web.AsMiddlewares(func() fiber.Handler {
				return cors.New()
			}),
		),

		// 数据库迁移（启动时执行）
		fx.Invoke(func(db *db.Client, logger *slog.Logger) {
			logger.Info("database_migration_start")
			if err := persistence.Migrate(db, logger); err != nil {
				logger.Error("migration_failed", slog.Any("err", err))
			} else {
				logger.Info("database_migration_complete")
			}
		}),

		// ============================================================================
		// 领域层 (Domain Layer)
		// ============================================================================
		// Repository 实现
		fx.Provide(func(db *db.Client, logger *slog.Logger) repository.ExchangeRepository {
			return persistence.NewExchangeRepo(db, logger)
		}),

		// ============================================================================
		// 交易所客户端层 (Exchange Client Layer)
		// ============================================================================
		// 符号映射器（统一处理不同交易所的币对差异）
		fx.Provide(func(cfg *AppConfig) *exchange.SymbolMapper {
			mapper := exchange.NewSymbolMapper()
			if cfg != nil && cfg.Arbitrage.Symbols != nil {
				for _, symbolCfg := range cfg.Arbitrage.Symbols {
					mapper.RegisterSymbol(
						symbolCfg.Symbol,
						symbolCfg.BinanceMarket,
						symbolCfg.LighterMarketID,
						symbolCfg.HyperliquidMarket,
					)
				}
			}
			return mapper
		}),

		// 交易所工厂
		fx.Provide(exchange.NewExchangeFactory),

		// 注册交易所客户端（在 Invoke 中创建，避免类型冲突）
		fx.Invoke(registerExchangeClients),

		// ============================================================================
		// 应用服务层 (Application Service Layer)
		// ============================================================================
		fx.Provide(service.NewComparisonService),
		fx.Provide(newCollectorService),

		// ============================================================================
		// 接口层 (Interface Layer)
		// ============================================================================
		fx.Provide(httpInterface.NewComparisonHandler),
		fx.Provide(httpInterface.NewWebSocketHandler),
		fx.Provide(func(logger *slog.Logger) *httpInterface.WebHandler {
			return httpInterface.NewWebHandler(logger, webDistFS)
		}),

		// ============================================================================
		// 启动和生命周期管理 (Lifecycle Management)
		// ============================================================================
		fx.Invoke(startup),
	).Run()
}

// registerExchangeClients 注册所有启用的交易所客户端
func registerExchangeClients(
	factory *exchange.ExchangeFactory,
	cfg *AppConfig,
	logger *slog.Logger,
	mapper *exchange.SymbolMapper,
) {
	logger.Info("registering_exchange_clients_begin")

	if cfg == nil {
		logger.Error("app_config_is_nil")
		return
	}

	if factory == nil {
		logger.Error("exchange_factory_is_nil")
		return
	}

	if logger == nil {
		return
	}

	// Binance 客户端
	if cfg.Exchanges.Binance.Enabled {
		logger.Info("registering_binance_client")
		binanceCfg := cfg.Exchanges.Binance.ToBinanceConfig(mapper)
		factory.Register("binance", binance.NewBinanceClient(binanceCfg, logger))
		logger.Info("binance_client_registered")
	} else {
		logger.Info("binance_client_disabled")
	}

	// Lighter 客户端
	if cfg.Exchanges.Lighter.Enabled {
		if mapper == nil {
			logger.Error("symbol_mapper_is_nil_for_lighter")
		} else {
			logger.Info("registering_lighter_client")
			lighterCfg := cfg.Exchanges.Lighter.ToLighterConfig(mapper)
			factory.Register("lighter", lighter.NewLighterClient(lighterCfg, logger))
			logger.Info("lighter_client_registered")
		}
	} else {
		logger.Info("lighter_client_disabled")
	}

	// Hyperliquid 客户端
	if cfg.Exchanges.Hyperliquid.Enabled {
		if mapper == nil {
			logger.Error("symbol_mapper_is_nil_for_hyperliquid")
		} else {
			logger.Info("registering_hyperliquid_client")
			hyperliquidCfg := cfg.Exchanges.Hyperliquid.ToHyperliquidConfig(mapper)
			factory.Register("hyperliquid", hyperliquid.NewHyperliquidClient(hyperliquidCfg, logger))
			logger.Info("hyperliquid_client_registered")
		}
	} else {
		logger.Info("hyperliquid_client_disabled")
	}

	logger.Info("registering_exchange_clients_complete")
}

// newCollectorService 创建数据收集服务
func newCollectorService(
	factory *exchange.ExchangeFactory,
	repo repository.ExchangeRepository,
	logger *slog.Logger,
	cfg *AppConfig,
) *service.CollectorService {
	if cfg == nil {
		logger.Error("app_config_is_nil_in_collector_service")
		return service.NewCollectorService(
			factory,
			repo,
			logger,
			[]string{},
			3*time.Second,
			60*time.Second,
		)
	}

	symbols := make([]string, 0)
	if cfg.Arbitrage.Symbols != nil {
		for _, s := range cfg.Arbitrage.Symbols {
			if s.Enabled {
				symbols = append(symbols, s.Symbol)
			}
		}
	}

	priceInterval := cfg.Arbitrage.PriceUpdateInterval
	if priceInterval == 0 {
		priceInterval = 3 * time.Second
	}

	rateInterval := cfg.Arbitrage.RateUpdateInterval
	if rateInterval == 0 {
		rateInterval = 60 * time.Second
	}

	return service.NewCollectorService(
		factory,
		repo,
		logger,
		symbols,
		priceInterval,
		rateInterval,
	)
}

// startup 启动服务和注册路由
func startup(
	app *fiber.App,
	comparisonHandler *httpInterface.ComparisonHandler,
	wsHandler *httpInterface.WebSocketHandler,
	webHandler *httpInterface.WebHandler,
	collectorService *service.CollectorService,
	comparisonService *service.ComparisonService,
	cfg *AppConfig,
	lc fx.Lifecycle,
) {
	// 注册路由
	registerRoutes(app, comparisonHandler, wsHandler, webHandler)

	// 启动后台服务
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// 使用传入的 logger（通过依赖注入）
			logger := collectorService.GetLogger()
			if logger == nil {
				logger = slog.Default()
			}

			logger.Info("startup_begin", slog.String("phase", "lifecycle_start"))

			// 启动数据收集服务（异步，不阻塞启动流程）
			logger.Info("starting_collector_service_async")
			go func() {
				// 使用独立的 context，不受启动超时限制
				startCtx := context.Background()
				if err := collectorService.Start(startCtx); err != nil {
					logger.Error("collector_service_start_failed", slog.Any("err", err))
				} else {
					logger.Info("collector_service_started_successfully")
				}
			}()

			// 启动定时对比任务（异步）
			logger.Info("starting_comparison_task_async")
			startComparisonTask(ctx, comparisonService, cfg)

			logger.Info("startup_complete", slog.String("phase", "lifecycle_start"))
			return nil
		},
		OnStop: func(ctx context.Context) error {
			collectorService.Stop()
			return nil
		},
	})
}

// registerRoutes 注册所有 HTTP 路由
func registerRoutes(
	app *fiber.App,
	comparisonHandler *httpInterface.ComparisonHandler,
	wsHandler *httpInterface.WebSocketHandler,
	webHandler *httpInterface.WebHandler,
) {
	// REST API 路由
	comparisonHandler.RegisterRoutes(app)

	// WebSocket 路由
	app.Get("/ws/comparison", websocket.New(wsHandler.HandleComparison))

	// Web UI 静态文件（必须在最后，作为 fallback）
	webHandler.RegisterRoutes(app)
}

// startComparisonTask 启动定时对比任务
func startComparisonTask(
	ctx context.Context,
	comparisonService *service.ComparisonService,
	cfg *AppConfig,
) {
	if cfg == nil || comparisonService == nil {
		return
	}

	priceInterval := cfg.Arbitrage.PriceUpdateInterval
	if priceInterval == 0 {
		priceInterval = 3 * time.Second
	}

	// 使用独立的 context，不受启动超时限制
	taskCtx := context.Background()

	go func() {
		// 等待一小段时间，确保服务已启动
		time.Sleep(5 * time.Second)

		// 立即执行一次对比，填充初始数据
		if cfg.Arbitrage.Symbols != nil {
			for _, symbolCfg := range cfg.Arbitrage.Symbols {
				if symbolCfg.Enabled {
					if _, err := comparisonService.CompareExchanges(taskCtx, symbolCfg.Symbol); err != nil {
						// 使用默认 logger，因为这里没有注入 logger
						// 错误会被 comparisonService 内部记录
					}
				}
			}
		}

		ticker := time.NewTicker(priceInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if cfg.Arbitrage.Symbols != nil {
					for _, symbolCfg := range cfg.Arbitrage.Symbols {
						if symbolCfg.Enabled {
							_, _ = comparisonService.CompareExchanges(taskCtx, symbolCfg.Symbol)
						}
					}
				}
			}
		}
	}()
}
