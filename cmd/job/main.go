package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/viper"
	"go.uber.org/fx"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"
	"goKit/pkg/kit/db"
	"goKit/pkg/kit/log"
)

// AppConfig 配置映射
type AppConfig struct {
	Database db.Config  `mapstructure:"database"`
	Log      log.Config `mapstructure:"log"`
}

func LoadConfig() (*AppConfig, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置失败: %v", err)
	}
	var cfg AppConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %v", err)
	}
	return &cfg, nil
}

func main() {
	app := fx.New(
		// 1. 基础配置提供
		fx.Provide(LoadConfig),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),
		fx.Provide(func(cfg *AppConfig) log.Config { return cfg.Log }),

		// 2. 核心组件库
		fx.Provide(log.NewLogger),
		fx.Provide(db.NewClient),

		// 3. 业务逻辑服务
		fx.Provide(service.NewCrawlerService),
		// fx.Provide(service.NewStrategyService), // 策略模块如需可开放

		// 4. 生命周期管理
		fx.Invoke(func(lc fx.Lifecycle, dbClient *db.Client, crawler *service.CrawlerService, logger *slog.Logger) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						logger.Info("==== 🚀 GoKit 量化爬虫引擎启动 ====")

						// 第一步：自动迁移数据库表结构，确保表存在
						gormDB := dbClient.GetDB(context.Background())
						gormDB.AutoMigrate(
							&entity.StockInfo{},
							&entity.InstitutionInfo{},
							&entity.StockHoldingRecord{},
							&entity.StockDailyQuote{},
						)
						logger.Info("数据库表结构校验完毕")

						// 第二步：同步基础信息与行业 (新浪 + 腾讯)
						if err := crawler.SyncStockBasics(context.Background()); err != nil {
							logger.Error("基础信息同步异常", slog.Any("err", err))
						}

						// 第三步：抓取最新一期财报 (东方财富)
						reportDate := "2024-09-30" // 示例为 2024 年三季报，可改为动态入参
						if err := crawler.SyncHoldings(context.Background(), reportDate); err != nil {
							logger.Error("持仓流水同步异常", slog.Any("err", err))
						}

						// 第四步：抓取核心标的 K 线 (以茅台为例，仅做演示，策略可全量遍历)
						if err := crawler.SyncDailyQuotes(context.Background(), "600519", 250); err != nil {
							logger.Error("K 线同步异常", slog.Any("err", err))
						}

						logger.Info("==== ✅ 所有爬虫任务执行完毕 ====")
						os.Exit(0)
					}()
					return nil
				},
			})
		}),
	)

	app.Run()
}
