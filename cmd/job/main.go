package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/fx"

	"goKit/internal/application/service"
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

// getLatestReportDate 自动推算 A 股最新有效财报期
func getLatestReportDate() string {
	now := time.Now()
	year := now.Year()
	month := now.Month()

	// 财报披露规则推导：
	// 5月以后，一季报肯定披露完了 (3-31)
	// 9月以后，中报肯定披露完了 (6-30)
	// 11月以后，三季报肯定披露完了 (9-30)
	// 1-4月，使用去年的年报 (12-31)
	if month >= 11 {
		return fmt.Sprintf("%d-09-30", year)
	} else if month >= 9 {
		return fmt.Sprintf("%d-06-30", year)
	} else if month >= 5 {
		return fmt.Sprintf("%d-03-31", year)
	}
	return fmt.Sprintf("%d-12-31", year-1)
}

func main() {
	app := fx.New(
		fx.Provide(LoadConfig),
		fx.Provide(func(cfg *AppConfig) db.Config { return cfg.Database }),
		fx.Provide(func(cfg *AppConfig) log.Config { return cfg.Log }),
		fx.Provide(log.NewLogger),
		fx.Provide(db.NewClient),
		fx.Provide(service.NewCrawlerService),

		fx.Invoke(func(lc fx.Lifecycle, dbClient *db.Client, crawler *service.CrawlerService, logger *slog.Logger) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						logger.Info("==== 🚀 GoKit 量化爬虫引擎启动 ====")

						// 第二步：同步基础信息与行业 (新浪)
						if err := crawler.SyncStockBasics(context.Background()); err != nil {
							logger.Error("基础信息同步异常", slog.Any("err", err))
						}

						// 第三步：【动态计算】抓取最新一期财报 (东方财富)
						reportDate := getLatestReportDate()
						logger.Info("推算出最新财报期", slog.String("reportDate", reportDate))
						if err := crawler.SyncHoldings(context.Background(), reportDate); err != nil {
							logger.Error("持仓流水同步异常", slog.Any("err", err))
						}

						// 第四步：【全量同步】拉取全市场所有股票最近 250 天的 K 线
						if err := crawler.SyncAllDailyQuotes(context.Background(), 250); err != nil {
							logger.Error("全量 K 线同步异常", slog.Any("err", err))
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
