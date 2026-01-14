package persistence

import (
	"context"
	"log/slog"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type ExchangeRepo struct {
	db     *db.Client
	logger *slog.Logger
}

func NewExchangeRepo(db *db.Client, logger *slog.Logger) repository.ExchangeRepository {
	return &ExchangeRepo{
		db:     db,
		logger: logger,
	}
}

func (r *ExchangeRepo) GetMarketData(ctx context.Context, symbol, exchange string) (*entity.MarketData, error) {
	var data entity.MarketData
	err := r.db.GetDB(ctx).
		Where("symbol = ? AND exchange = ?", symbol, exchange).
		First(&data).Error
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (r *ExchangeRepo) SaveMarketData(ctx context.Context, data *entity.MarketData) error {
	return r.db.GetDB(ctx).
		Save(data).Error
}

func (r *ExchangeRepo) SaveMarketDataSnapshot(ctx context.Context, snapshot *entity.MarketDataSnapshot) error {
	return r.db.GetDB(ctx).
		Create(snapshot).Error
}

func (r *ExchangeRepo) GetFundingRate(ctx context.Context, symbol, exchange string) (*entity.FundingRate, error) {
	var rate entity.FundingRate
	err := r.db.GetDB(ctx).
		Where("symbol = ? AND exchange = ?", symbol, exchange).
		First(&rate).Error
	if err != nil {
		return nil, err
	}
	return &rate, nil
}

func (r *ExchangeRepo) SaveFundingRate(ctx context.Context, rate *entity.FundingRate) error {
	return r.db.GetDB(ctx).
		Save(rate).Error
}

func (r *ExchangeRepo) SaveFundingRateHistory(ctx context.Context, history *entity.FundingRateHistory) error {
	return r.db.GetDB(ctx).
		Create(history).Error
}

func (r *ExchangeRepo) SaveArbitrageOpportunity(ctx context.Context, opp *entity.ArbitrageOpportunity) error {
	return r.db.GetDB(ctx).
		Create(opp).Error
}

func (r *ExchangeRepo) GetArbitrageOpportunities(ctx context.Context, symbol string, limit int) ([]*entity.ArbitrageOpportunity, error) {
	var opps []*entity.ArbitrageOpportunity
	query := r.db.GetDB(ctx).
		Where("symbol = ?", symbol).
		Order("detected_at DESC")
	
	if limit > 0 {
		query = query.Limit(limit)
	}
	
	err := query.Find(&opps).Error
	return opps, err
}

func (r *ExchangeRepo) GetArbitrageThreshold(ctx context.Context, symbol, exchangeA, exchangeB string) (*entity.ArbitrageThreshold, error) {
	var threshold entity.ArbitrageThreshold
	err := r.db.GetDB(ctx).
		Where("symbol = ? AND exchange_a = ? AND exchange_b = ?", symbol, exchangeA, exchangeB).
		First(&threshold).Error
	if err != nil {
		return nil, err
	}
	return &threshold, nil
}

func (r *ExchangeRepo) SaveArbitrageThreshold(ctx context.Context, threshold *entity.ArbitrageThreshold) error {
	return r.db.GetDB(ctx).
		Save(threshold).Error
}

func (r *ExchangeRepo) GetExchangeConfig(ctx context.Context, name string) (*entity.ExchangeConfig, error) {
	var config entity.ExchangeConfig
	err := r.db.GetDB(ctx).
		Where("name = ?", name).
		First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *ExchangeRepo) CleanupOldData(ctx context.Context) error {
	oneDayAgo := time.Now().Add(-24 * time.Hour)
	
	// 清理市场数据快照
	if err := r.db.GetDB(ctx).
		Where("timestamp < ?", oneDayAgo).
		Delete(&entity.MarketDataSnapshot{}).Error; err != nil {
		r.logger.Warn("cleanup_market_data_failed", slog.Any("err", err))
	}
	
	// 清理资金费率历史
	if err := r.db.GetDB(ctx).
		Where("timestamp < ?", oneDayAgo).
		Delete(&entity.FundingRateHistory{}).Error; err != nil {
		r.logger.Warn("cleanup_funding_rate_failed", slog.Any("err", err))
	}
	
	// 清理过期套利机会
	if err := r.db.GetDB(ctx).
		Where("expired_at < ? AND executed = ?", time.Now(), false).
		Delete(&entity.ArbitrageOpportunity{}).Error; err != nil {
		r.logger.Warn("cleanup_arbitrage_failed", slog.Any("err", err))
	}
	
	return nil
}
