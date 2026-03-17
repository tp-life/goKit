package persistence

import (
	"goKit/internal/domain/entity"
	"goKit/pkg/kit/db"
)

func AutoMigrate(client *db.Client) error {
	return client.GetDB(nil).AutoMigrate(
		&entity.Symbol{},
		&entity.FundingSnapshot{},
		&entity.BookTopSnapshot{},
		&entity.Opportunity{},
		&entity.StrategyRun{},
		&entity.ExecutionPlan{},
		&entity.ExecutionRecord{},
		&entity.OrderRecord{},
	)
}
