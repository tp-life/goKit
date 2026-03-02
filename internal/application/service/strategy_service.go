package service

import (
	"context"
	"math"

	"goKit/internal/application/dto"
	"goKit/internal/domain/repository"
)

type StrategyService struct {
	repo repository.StrategyRepository
}

func NewStrategyService(repo repository.StrategyRepository) *StrategyService {
	return &StrategyService{repo: repo}
}

// FindGoldenPitStocks 找出国家队新进/增持，但最新价格跌破成本的股票
func (s *StrategyService) FindGoldenPitStocks(ctx context.Context, reportDate, quarterStart, quarterEnd string) ([]dto.GoldenPitResp, error) {
	holdings, err := s.repo.FindNationalTeamHoldings(ctx, reportDate)
	if err != nil {
		return nil, err
	}

	var results []dto.GoldenPitResp

	for _, h := range holdings {
		// 只关注建仓或加仓的动作
		if h.ChangeType == "新进" || h.ChangeType == "增持" {

			// 1. 估算当季度均价作为成本线
			costPrice, err := s.repo.GetQuarterlyAveragePrice(ctx, h.StockCode, quarterStart, quarterEnd)
			if err != nil || costPrice <= 0 {
				continue
			}

			// 2. 获取最新收盘价
			latestPrice, err := s.repo.GetLatestPrice(ctx, h.StockCode)
			if err != nil || latestPrice <= 0 {
				continue
			}

			// 3. 策略判定：如果最新价低于估算成本价
			if latestPrice < costPrice {
				dropRatio := math.Round(((costPrice-latestPrice)/costPrice)*10000) / 100 // 保留两位小数

				results = append(results, dto.GoldenPitResp{
					StockCode:   h.StockCode,
					StockName:   h.Stock.StockName,
					Institution: h.Institution.InstName,
					ChangeType:  h.ChangeType,
					CostPrice:   math.Round(costPrice*100) / 100,
					LatestPrice: latestPrice,
					DropRatio:   dropRatio,
				})
			}
		}
	}

	return results, nil
}
