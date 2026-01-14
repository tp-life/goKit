package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

// ExchangeRepository 交易所数据仓库接口
type ExchangeRepository interface {
	// 市场数据
	GetMarketData(ctx context.Context, symbol, exchange string) (*entity.MarketData, error)
	SaveMarketData(ctx context.Context, data *entity.MarketData) error
	SaveMarketDataSnapshot(ctx context.Context, snapshot *entity.MarketDataSnapshot) error
	
	// 资金费率
	GetFundingRate(ctx context.Context, symbol, exchange string) (*entity.FundingRate, error)
	SaveFundingRate(ctx context.Context, rate *entity.FundingRate) error
	SaveFundingRateHistory(ctx context.Context, history *entity.FundingRateHistory) error
	
	// 套利机会
	SaveArbitrageOpportunity(ctx context.Context, opp *entity.ArbitrageOpportunity) error
	GetArbitrageOpportunities(ctx context.Context, symbol string, limit int) ([]*entity.ArbitrageOpportunity, error)
	
	// 配置
	GetArbitrageThreshold(ctx context.Context, symbol, exchangeA, exchangeB string) (*entity.ArbitrageThreshold, error)
	SaveArbitrageThreshold(ctx context.Context, threshold *entity.ArbitrageThreshold) error
	GetExchangeConfig(ctx context.Context, name string) (*entity.ExchangeConfig, error)
	
	// 清理历史数据（保留1天）
	CleanupOldData(ctx context.Context) error
}
