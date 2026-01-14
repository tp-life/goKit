package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"
)

type CollectorService struct {
	exchangeFactory *exchange.ExchangeFactory
	exchangeRepo    repository.ExchangeRepository
	logger          *slog.Logger
	symbols         []string
	priceInterval   time.Duration
	rateInterval    time.Duration
	stopChan        chan struct{}
	wg              sync.WaitGroup
}

// GetLogger 返回 logger（用于外部访问）
func (s *CollectorService) GetLogger() *slog.Logger {
	return s.logger
}

func NewCollectorService(
	factory *exchange.ExchangeFactory,
	repo repository.ExchangeRepository,
	logger *slog.Logger,
	symbols []string,
	priceInterval, rateInterval time.Duration,
) *CollectorService {
	return &CollectorService{
		exchangeFactory: factory,
		exchangeRepo:    repo,
		logger:          logger,
		symbols:         symbols,
		priceInterval:   priceInterval,
		rateInterval:    rateInterval,
		stopChan:        make(chan struct{}),
	}
}

func (s *CollectorService) Start(ctx context.Context) error {
	s.logger.Info("collector_service_start_begin")
	
	if s.exchangeFactory == nil {
		s.logger.Error("exchange_factory_is_nil")
		return errors.New("exchange factory is nil")
	}

	clients := s.exchangeFactory.GetAllClients()
	if clients == nil {
		s.logger.Error("no_exchange_clients_available")
		return errors.New("no exchange clients available")
	}

	s.logger.Info("found_exchange_clients", slog.Int("count", len(clients)))

	// 为 WebSocket 连接创建独立的 context（不受启动超时限制）
	// 使用 context.Background() 因为连接是长期运行的
	connectCtx := context.Background()

	// 连接所有交易所（异步进行，不阻塞启动）
	for name, client := range clients {
		if client == nil {
			s.logger.Warn("exchange_client_is_nil", slog.String("exchange", name))
			continue
		}
		
		// 异步连接，避免阻塞
		s.wg.Add(1)
		go func(exchangeName string, exchangeClient exchange.ExchangeClient) {
			defer s.wg.Done()
			
			s.logger.Info("connecting_exchange", slog.String("exchange", exchangeName))
			if err := exchangeClient.Connect(connectCtx); err != nil {
				s.logger.Error("connect_exchange_failed",
					slog.String("exchange", exchangeName),
					slog.Any("err", err),
				)
				// 只记录 Lighter 的连接失败
				if exchangeName == "lighter" {
					// #region agent log
					func() {
						logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if logFile != nil {
							defer logFile.Close()
							logEntry := map[string]interface{}{
								"sessionId":    "debug-session",
								"runId":        "run1",
								"hypothesisId": "G",
								"location":     "collector_service.go:Connect",
								"message":      "lighter_connect_failed",
								"data":         map[string]interface{}{"error": err.Error()},
								"timestamp":    time.Now().UnixMilli(),
							}
							json.NewEncoder(logFile).Encode(logEntry)
						}
					}()
					// #endregion
				}
				return
			}
			s.logger.Info("exchange_connected", slog.String("exchange", exchangeName))

			// 订阅市场数据（也使用独立的 context）
			s.logger.Info("subscribing_market_data", 
				slog.String("exchange", exchangeName),
				slog.Int("symbols_count", len(s.symbols)),
			)
			marketDataCh, err := exchangeClient.SubscribeMarketData(connectCtx, s.symbols)
			if err != nil {
				s.logger.Error("subscribe_market_data_failed",
					slog.String("exchange", exchangeName),
					slog.Any("err", err),
				)
				// 只记录 Lighter 的订阅失败
				if exchangeName == "lighter" {
					// #region agent log
					func() {
						logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if logFile != nil {
							defer logFile.Close()
							logEntry := map[string]interface{}{
								"sessionId":    "debug-session",
								"runId":        "run1",
								"hypothesisId": "G",
								"location":     "collector_service.go:SubscribeMarketData",
								"message":      "lighter_subscribe_failed",
								"data":         map[string]interface{}{"error": err.Error()},
								"timestamp":    time.Now().UnixMilli(),
							}
							json.NewEncoder(logFile).Encode(logEntry)
						}
					}()
					// #endregion
				}
				return
			}
			s.logger.Info("market_data_subscribed", slog.String("exchange", exchangeName))

			// 启动数据收集协程
			s.wg.Add(1)
			go s.collectMarketData(exchangeName, marketDataCh)

			// 对于 Binance 和 Lighter，还需要收集资金费率数据（从 WebSocket 实时获取）
			if exchangeName == "binance" || exchangeName == "lighter" {
				if fundingRateClient, ok := client.(interface {
					GetFundingRateChannel() <-chan *entity.FundingRate
				}); ok {
					s.wg.Add(1)
					go s.collectFundingRates(exchangeName, fundingRateClient.GetFundingRateChannel())
				}
			}
		}(name, client)
	}

	// 启动定时更新资金费率
	s.wg.Add(1)
	go s.updateFundingRates(ctx)

	// 启动定时清理历史数据
	s.wg.Add(1)
	go s.cleanupOldData(ctx)

	s.logger.Info("collector_service_started", 
		slog.Int("exchanges", len(clients)),
		slog.Int("symbols", len(s.symbols)),
	)
	return nil
}

func (s *CollectorService) collectMarketData(exchangeName string, dataCh <-chan *entity.MarketData) {
	defer s.wg.Done()

	// 使用节流机制，避免过于频繁的数据库写入
	// 每个交易所每 5 秒最多保存一次市场数据
	lastSaveTime := make(map[string]time.Time)
	saveInterval := 5 * time.Second

	for {
		select {
		case <-s.stopChan:
			return
		case data, ok := <-dataCh:
			if !ok {
				return
			}
			
			// 只记录 Lighter 的数据接收（用于调试资金费率问题）
			if exchangeName == "lighter" {
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "C",
							"location":     "collector_service.go:141",
							"message":      "lighter_data_received",
							"data":         map[string]interface{}{"symbol": data.Symbol, "has_spot": data.SpotPrice != nil, "has_mark": data.MarkPrice != nil},
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion
			}
			
			// 检查是否需要节流
			key := fmt.Sprintf("%s:%s", data.Exchange, data.Symbol)
			lastSave, exists := lastSaveTime[key]
			if exists && time.Since(lastSave) < saveInterval {
				// 跳过这次保存，但更新内存中的数据（用于实时显示）
				continue
			}
			
			// 更新最后保存时间
			lastSaveTime[key] = time.Now()
			
			// 保存数据
			s.saveMarketData(context.Background(), data)
		}
	}
}

func (s *CollectorService) collectFundingRates(exchangeName string, dataCh <-chan *entity.FundingRate) {
	defer s.wg.Done()

	// 使用节流机制，避免过于频繁的数据库写入
	// 每个交易所每 5 秒最多保存一次资金费率
	lastSaveTime := make(map[string]time.Time)
	saveInterval := 5 * time.Second

	// 创建 symbol 集合，用于快速查找
	symbolSet := make(map[string]bool)
	for _, symbol := range s.symbols {
		symbolSet[symbol] = true
	}

	for {
		select {
		case <-s.stopChan:
			return
		case data, ok := <-dataCh:
			if !ok {
				return
			}

			// 只处理配置中启用的 symbol
			if !symbolSet[data.Symbol] {
				continue
			}

			// 检查是否需要节流
			key := fmt.Sprintf("%s:%s", data.Exchange, data.Symbol)
			lastSave, exists := lastSaveTime[key]
			if exists && time.Since(lastSave) < saveInterval {
				// 跳过这次保存
				continue
			}

			// 更新最后保存时间
			lastSaveTime[key] = time.Now()

			// 保存资金费率
			ctx := context.Background()
			if err := s.exchangeRepo.SaveFundingRate(ctx, data); err != nil {
				s.logger.Warn("save_funding_rate_failed",
					slog.String("exchange", data.Exchange),
					slog.String("symbol", data.Symbol),
					slog.Any("err", err),
				)
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "F",
							"location":     "collector_service.go:collectFundingRates",
							"message":      "save_funding_rate_failed",
							"data":         map[string]interface{}{"exchange": data.Exchange, "symbol": data.Symbol, "error": err.Error()},
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion
			} else {
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "F",
							"location":     "collector_service.go:collectFundingRates",
							"message":      "save_funding_rate_success",
							"data":         map[string]interface{}{"exchange": data.Exchange, "symbol": data.Symbol},
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion

				// 保存历史记录
				history := &entity.FundingRateHistory{
					Symbol:      data.Symbol,
					Exchange:    data.Exchange,
					Rate:        data.Rate,
					Rate8H:      data.Rate8H,
					NextFunding: data.NextFunding,
					Timestamp:   data.UpdatedAt,
				}

				if err := s.exchangeRepo.SaveFundingRateHistory(ctx, history); err != nil {
					s.logger.Warn("save_funding_rate_history_failed",
						slog.String("exchange", data.Exchange),
						slog.String("symbol", data.Symbol),
						slog.Any("err", err),
					)
				}
			}
		}
	}
}

func (s *CollectorService) saveMarketData(ctx context.Context, data *entity.MarketData) {
	// 保存当前数据
	if err := s.exchangeRepo.SaveMarketData(ctx, data); err != nil {
		s.logger.Warn("save_market_data_failed",
			slog.String("exchange", data.Exchange),
			slog.String("symbol", data.Symbol),
			slog.Any("err", err),
		)
	}

	// 保存快照
	snapshot := &entity.MarketDataSnapshot{
		Symbol:         data.Symbol,
		Exchange:       data.Exchange,
		SpotPrice:      data.SpotPrice,
		MarkPrice:      data.MarkPrice,
		IndexPrice:     data.IndexPrice,
		LastTradePrice: data.LastTradePrice,
		Timestamp:      data.UpdatedAt,
	}

	if err := s.exchangeRepo.SaveMarketDataSnapshot(ctx, snapshot); err != nil {
		s.logger.Warn("save_market_data_snapshot_failed",
			slog.String("exchange", data.Exchange),
			slog.String("symbol", data.Symbol),
			slog.Any("err", err),
		)
	}
}

func (s *CollectorService) updateFundingRates(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.rateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.fetchAllFundingRates(ctx)
		}
	}
}

func (s *CollectorService) fetchAllFundingRates(ctx context.Context) {
	clients := s.exchangeFactory.GetAllClients()

		for name, client := range clients {
		if !client.IsConnected() {
			// 只记录 Lighter 未连接的情况
			if name == "lighter" {
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "H",
							"location":     "collector_service.go:fetchAllFundingRates",
							"message":      "lighter_not_connected",
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion
			}
			continue
		}

		for _, symbol := range s.symbols {
			// 只记录 Lighter 的资金费率获取
			if name == "lighter" {
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "H",
							"location":     "collector_service.go:fetchAllFundingRates",
							"message":      "lighter_before_get_funding_rate",
							"data":         map[string]interface{}{"symbol": symbol},
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion
			}
			rate, err := client.GetFundingRate(ctx, symbol)
			if err != nil {
				s.logger.Warn("fetch_funding_rate_failed",
					slog.String("exchange", name),
					slog.String("symbol", symbol),
					slog.Any("err", err),
				)
				// 只记录 Lighter 的错误
				if name == "lighter" {
					// #region agent log
					func() {
						logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if logFile != nil {
							defer logFile.Close()
							logEntry := map[string]interface{}{
								"sessionId":    "debug-session",
								"runId":        "run1",
								"hypothesisId": "H",
								"location":     "collector_service.go:fetchAllFundingRates",
								"message":      "lighter_get_funding_rate_failed",
								"data":         map[string]interface{}{"symbol": symbol, "error": err.Error()},
								"timestamp":    time.Now().UnixMilli(),
							}
							json.NewEncoder(logFile).Encode(logEntry)
						}
					}()
					// #endregion
				}
				continue
			}

			// 只记录 Lighter 的成功情况
			if name == "lighter" {
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "H",
							"location":     "collector_service.go:fetchAllFundingRates",
							"message":      "lighter_get_funding_rate_success",
							"data":         map[string]interface{}{"symbol": symbol, "rate": rate.Rate, "rate8h": rate.Rate8H},
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion
			}

			// 保存资金费率
			if err := s.exchangeRepo.SaveFundingRate(ctx, rate); err != nil {
				s.logger.Warn("save_funding_rate_failed",
					slog.String("exchange", name),
					slog.String("symbol", symbol),
					slog.Any("err", err),
				)
				// 只记录 Lighter 的保存失败
				if name == "lighter" {
					// #region agent log
					func() {
						logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if logFile != nil {
							defer logFile.Close()
							logEntry := map[string]interface{}{
								"sessionId":    "debug-session",
								"runId":        "run1",
								"hypothesisId": "H",
								"location":     "collector_service.go:fetchAllFundingRates",
								"message":      "lighter_save_funding_rate_failed",
								"data":         map[string]interface{}{"symbol": symbol, "error": err.Error()},
								"timestamp":    time.Now().UnixMilli(),
							}
							json.NewEncoder(logFile).Encode(logEntry)
						}
					}()
					// #endregion
				}
			} else if name == "lighter" {
				// 只记录 Lighter 的保存成功
				// #region agent log
				func() {
					logFile, _ := os.OpenFile("/Users/tp/work/person/goKit/.cursor/debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if logFile != nil {
						defer logFile.Close()
						logEntry := map[string]interface{}{
							"sessionId":    "debug-session",
							"runId":        "run1",
							"hypothesisId": "H",
							"location":     "collector_service.go:fetchAllFundingRates",
							"message":      "lighter_save_funding_rate_success",
							"data":         map[string]interface{}{"symbol": symbol},
							"timestamp":    time.Now().UnixMilli(),
						}
						json.NewEncoder(logFile).Encode(logEntry)
					}
				}()
				// #endregion
			}

			// 保存历史记录
			history := &entity.FundingRateHistory{
				Symbol:      rate.Symbol,
				Exchange:    rate.Exchange,
				Rate:        rate.Rate,
				Rate8H:      rate.Rate8H,
				NextFunding: rate.NextFunding,
				Timestamp:   rate.UpdatedAt,
			}

			if err := s.exchangeRepo.SaveFundingRateHistory(ctx, history); err != nil {
				s.logger.Warn("save_funding_rate_history_failed",
					slog.String("exchange", name),
					slog.String("symbol", symbol),
					slog.Any("err", err),
				)
			}
		}
	}
}

func (s *CollectorService) cleanupOldData(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Hour) // 每小时清理一次
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			if err := s.exchangeRepo.CleanupOldData(ctx); err != nil {
				s.logger.Warn("cleanup_old_data_failed", slog.Any("err", err))
			} else {
				s.logger.Info("cleanup_old_data_success")
			}
		}
	}
}

func (s *CollectorService) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	s.logger.Info("collector_service_stopped")
}
