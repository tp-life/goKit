package repository

import (
	"context"
	"goKit/internal/domain/entity"
)

type StrategyRepository interface {
	// FindNationalTeamHoldings 查找特定报告期内“国家队”的持仓记录
	FindNationalTeamHoldings(ctx context.Context, reportDate string) ([]*entity.StockHoldingRecord, error)

	// GetQuarterlyAveragePrice 获取某只股票在特定时间段内的收盘均价
	GetQuarterlyAveragePrice(ctx context.Context, stockCode string, startDate, endDate string) (float64, error)

	// GetLatestPrice 获取某只股票的最新收盘价
	GetLatestPrice(ctx context.Context, stockCode string) (float64, error)
}
