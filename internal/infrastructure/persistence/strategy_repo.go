package persistence

import (
	"context"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type StrategyRepo struct {
	client *db.Client
}

func NewStrategyRepo(client *db.Client) repository.StrategyRepository {
	return &StrategyRepo{client: client}
}

func (r *StrategyRepo) FindNationalTeamHoldings(ctx context.Context, reportDate string) ([]*entity.StockHoldingRecord, error) {
	var records []*entity.StockHoldingRecord

	err := r.client.GetDB(ctx).
		Preload("Stock").
		Preload("Institution").
		Joins("JOIN institution_info ON institution_info.inst_id = stock_holding_records.inst_id").
		Where("stock_holding_records.report_date = ?", reportDate).
		Where("institution_info.inst_type = ?", "国家队").
		Order("stock_holding_records.hold_ratio DESC").
		Find(&records).Error

	return records, err
}

func (r *StrategyRepo) GetQuarterlyAveragePrice(ctx context.Context, stockCode string, startDate, endDate string) (float64, error) {
	var avgPrice float64
	err := r.client.GetDB(ctx).
		Model(&entity.StockDailyQuote{}).
		Select("COALESCE(AVG(close), 0)").
		Where("stock_code = ?", stockCode).
		Where("trade_date >= ? AND trade_date <= ?", startDate, endDate).
		Scan(&avgPrice).Error

	return avgPrice, err
}

func (r *StrategyRepo) GetLatestPrice(ctx context.Context, stockCode string) (float64, error) {
	var latestClose float64
	err := r.client.GetDB(ctx).
		Model(&entity.StockDailyQuote{}).
		Select("close").
		Where("stock_code = ?", stockCode).
		Order("trade_date DESC").
		Limit(1).
		Scan(&latestClose).Error

	return latestClose, err
}
