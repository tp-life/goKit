package persistence

import (
	"context"
	"fmt"
	"log/slog"

	"goKit/internal/domain/entity"
	"goKit/pkg/kit/db"
)

func Migrate(dbClient *db.Client, logger *slog.Logger) error {
	if dbClient == nil {
		if logger != nil {
			logger.Error("database_client_is_nil")
		}
		return fmt.Errorf("database client is nil")
	}

	// 自动迁移所有实体
	ctx := context.Background()
	db := dbClient.GetDB(ctx)

	entities := []interface{}{
		&entity.MarketData{},
		&entity.MarketDataSnapshot{},
		&entity.FundingRate{},
		&entity.FundingRateHistory{},
		&entity.ArbitrageOpportunity{},
		&entity.ArbitrageThreshold{},
		&entity.ExchangeConfig{},
	}

	for _, e := range entities {
		if err := db.AutoMigrate(e); err != nil {
			logger.Error("database_migration_failed",
				slog.String("entity", fmt.Sprintf("%T", e)),
				slog.Any("err", err),
			)
			return fmt.Errorf("migrate %T: %w", e, err)
		}
		logger.Debug("entity_migrated", slog.String("entity", fmt.Sprintf("%T", e)))
	}

	logger.Info("database_migration_success", slog.Int("entities_count", len(entities)))
	return nil
}
