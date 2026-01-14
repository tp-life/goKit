package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"
	"goKit/pkg/kit/cache"
	"goKit/pkg/kit/math"
)

type ComparisonService struct {
	exchangeFactory *exchange.ExchangeFactory
	exchangeRepo    repository.ExchangeRepository
	cache           *cache.MemoryCache
	logger          *slog.Logger
	mu              sync.RWMutex
	comparisons     map[string]*entity.ComparisonResult
}

func NewComparisonService(
	factory *exchange.ExchangeFactory,
	repo repository.ExchangeRepository,
	logger *slog.Logger,
) *ComparisonService {
	return &ComparisonService{
		exchangeFactory: factory,
		exchangeRepo:    repo,
		cache:           cache.NewMemoryCache(),
		logger:          logger,
		comparisons:     make(map[string]*entity.ComparisonResult),
	}
}

// CompareExchanges 对比多个交易所的数据
func (s *ComparisonService) CompareExchanges(ctx context.Context, symbol string) (*entity.ComparisonResult, error) {
	if s.exchangeFactory == nil {
		return nil, errors.New("exchange factory is nil")
	}

	clients := s.exchangeFactory.GetAllClients()
	if len(clients) < 2 {
		return nil, ErrInsufficientExchanges
	}

	result := &entity.ComparisonResult{
		Symbol:      symbol,
		Exchanges:   make([]string, 0),
		Prices:      make(map[string]*entity.MarketData),
		Rates:       make(map[string]*entity.FundingRate),
		Comparisons: make([]entity.ExchangePairComparison, 0),
	}

	// 收集所有交易所的数据
	for name, client := range clients {
		if !client.IsConnected() {
			s.logger.Debug("exchange_not_connected",
				slog.String("exchange", name),
				slog.String("symbol", symbol),
			)
			continue
		}

		// 查询时使用统一符号（因为数据保存时已经转换为统一符号）
		// Binance 数据保存时已经通过 symbolMapper.FromBinance 转换为统一符号
		querySymbol := symbol

		s.logger.Debug("querying_market_data",
			slog.String("exchange", name),
			slog.String("symbol", symbol),
			slog.String("query_symbol", querySymbol),
		)

		marketData, err := s.exchangeRepo.GetMarketData(ctx, querySymbol, name)
		if err != nil {
			s.logger.Warn("get_market_data_failed",
				slog.String("exchange", name),
				slog.String("symbol", symbol),
				slog.String("query_symbol", querySymbol),
				slog.Any("err", err),
			)
			// 即使市场数据获取失败，也继续尝试获取资金费率
		} else {
			s.logger.Debug("market_data_found",
				slog.String("exchange", name),
				slog.String("symbol", symbol),
				slog.Any("mark_price", marketData.MarkPrice),
			)
		}

		fundingRate, err := s.exchangeRepo.GetFundingRate(ctx, querySymbol, name)
		if err != nil {
			s.logger.Warn("get_funding_rate_failed",
				slog.String("exchange", name),
				slog.String("symbol", symbol),
				slog.String("query_symbol", querySymbol),
				slog.Any("err", err),
			)
			// 资金费率获取失败时，创建一个空的 FundingRate 对象，避免前端显示 NULL
			// 但只在有市场数据时才添加到结果中
			if marketData != nil {
				fundingRate = &entity.FundingRate{
					Symbol:   symbol,
					Exchange: name,
					Rate:     0,
					Rate8H:   0,
				}
			}
		} else {
			s.logger.Debug("funding_rate_found",
				slog.String("exchange", name),
				slog.String("symbol", symbol),
				slog.Float64("rate8h", fundingRate.Rate8H),
			)
		}

		// 只有当市场数据存在时才添加到结果中
		if marketData != nil {
			result.Exchanges = append(result.Exchanges, name)
			result.Prices[name] = marketData
			// 资金费率可能为 nil（如果获取失败且没有市场数据），但如果有就添加
			if fundingRate != nil {
				result.Rates[name] = fundingRate
			} else {
				// 即使资金费率为空，也创建一个默认值
				result.Rates[name] = &entity.FundingRate{
					Symbol:   symbol,
					Exchange: name,
					Rate:     0,
					Rate8H:   0,
				}
			}
		}
	}

	// 计算所有交易所对的对比
	s.calculateComparisons(ctx, result)

	result.LastUpdated = time.Now()

	s.mu.Lock()
	s.comparisons[symbol] = result
	s.mu.Unlock()

	return result, nil
}

func (s *ComparisonService) calculateComparisons(ctx context.Context, result *entity.ComparisonResult) {
	exchanges := result.Exchanges
	for i := 0; i < len(exchanges); i++ {
		for j := i + 1; j < len(exchanges); j++ {
			exchangeA := exchanges[i]
			exchangeB := exchanges[j]

			comparison := s.comparePair(ctx, result.Symbol, exchangeA, exchangeB, result)
			if comparison != nil {
				result.Comparisons = append(result.Comparisons, *comparison)
			}
		}
	}
}

func (s *ComparisonService) comparePair(
	ctx context.Context,
	symbol, exchangeA, exchangeB string,
	result *entity.ComparisonResult,
) *entity.ExchangePairComparison {
	priceA := result.Prices[exchangeA]
	priceB := result.Prices[exchangeB]
	rateA := result.Rates[exchangeA]
	rateB := result.Rates[exchangeB]

	// 价格必须存在，但资金费率可以为空（使用默认值 0）
	if priceA == nil || priceB == nil {
		return nil
	}

	// 如果资金费率为空，创建默认值
	if rateA == nil {
		rateA = &entity.FundingRate{
			Symbol:   symbol,
			Exchange: exchangeA,
			Rate:     0,
			Rate8H:   0,
		}
	}
	if rateB == nil {
		rateB = &entity.FundingRate{
			Symbol:   symbol,
			Exchange: exchangeB,
			Rate:     0,
			Rate8H:   0,
		}
	}

	// 使用 Mark Price 进行对比
	var priceDiff float64
	if priceA.MarkPrice != nil && priceB.MarkPrice != nil {
		priceDiff = math.PriceDiff(*priceA.MarkPrice, *priceB.MarkPrice)
	}

	// 计算费率差异
	rateDiff := math.RateDiff(rateA.Rate8H, rateB.Rate8H)

	// 获取阈值配置
	threshold, _ := s.exchangeRepo.GetArbitrageThreshold(ctx, symbol, exchangeA, exchangeB)
	if threshold == nil {
		// 使用默认阈值
		threshold = &entity.ArbitrageThreshold{
			HighThreshold:    0.0005,
			MediumThreshold:  0.0002,
			MinProfitability: 0.0001,
		}
	}

	// 计算套利级别
	arbitrageLevel := s.calculateArbitrageLevel(math.Abs(rateDiff), threshold)

	// 计算预期收益率（简化版，实际需要考虑手续费等）
	profitability := math.Profitability(math.Abs(rateDiff), 0.001, 0.001) // 假设手续费 0.1%

	comparison := &entity.ExchangePairComparison{
		ExchangeA:      exchangeA,
		ExchangeB:      exchangeB,
		PriceDiff:      priceDiff,
		RateDiff:       rateDiff,
		ArbitrageLevel: arbitrageLevel,
		Profitability:  profitability,
	}

	// 如果发现套利机会，保存到数据库
	if arbitrageLevel != "none" && profitability >= threshold.MinProfitability {
		s.saveArbitrageOpportunity(ctx, symbol, exchangeA, exchangeB, comparison)
	}

	return comparison
}

func (s *ComparisonService) calculateArbitrageLevel(rateDiffAbs float64, threshold *entity.ArbitrageThreshold) string {
	if rateDiffAbs >= threshold.HighThreshold {
		return "high"
	} else if rateDiffAbs >= threshold.MediumThreshold {
		return "medium"
	}
	return "none"
}

func (s *ComparisonService) saveArbitrageOpportunity(
	ctx context.Context,
	symbol, exchangeA, exchangeB string,
	comparison *entity.ExchangePairComparison,
) {
	opp := &entity.ArbitrageOpportunity{
		Symbol:         symbol,
		ExchangeA:      exchangeA,
		ExchangeB:      exchangeB,
		PriceDiff:      comparison.PriceDiff,
		RateDiff:       comparison.RateDiff,
		ArbitrageLevel: comparison.ArbitrageLevel,
		Profitability:  comparison.Profitability,
		DetectedAt:     time.Now(),
		ExpiredAt:      timePtr(time.Now().Add(5 * time.Minute)), // 5分钟后过期
		Executed:       false,
	}

	if err := s.exchangeRepo.SaveArbitrageOpportunity(ctx, opp); err != nil {
		s.logger.Warn("save_arbitrage_opportunity_failed", slog.Any("err", err))
	}
}

func (s *ComparisonService) GetAllComparisons() map[string]*entity.ComparisonResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*entity.ComparisonResult)
	for k, v := range s.comparisons {
		result[k] = v
	}

	s.logger.Debug("get_all_comparisons",
		slog.Int("count", len(result)),
		slog.Any("symbols", func() []string {
			symbols := make([]string, 0, len(result))
			for k := range result {
				symbols = append(symbols, k)
			}
			return symbols
		}()),
	)

	return result
}

func (s *ComparisonService) GetComparison(symbol string) (*entity.ComparisonResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result, ok := s.comparisons[symbol]
	return result, ok
}

var ErrInsufficientExchanges = errors.New("insufficient exchanges for comparison")

func timePtr(t time.Time) *time.Time {
	return &t
}
