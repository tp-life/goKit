package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"goKit/internal/application/dto"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	infraMarketData "goKit/internal/infrastructure/marketdata"
	infraPolymarket "goKit/internal/infrastructure/polymarket"

	"go.uber.org/fx"
)

var insufficientBalanceAllowancePattern = regexp.MustCompile(`balance:\s*(\d+),\s*order amount:\s*(\d+)`)

type autoTradeRejectCode string

const (
	autoTradeRejectNone             autoTradeRejectCode = ""
	autoTradeRejectNoMarket         autoTradeRejectCode = "no_market"
	autoTradeRejectMarketClosed     autoTradeRejectCode = "market_closed"
	autoTradeRejectTimeWindowMiss   autoTradeRejectCode = "time_window_miss"
	autoTradeRejectReferenceMiss    autoTradeRejectCode = "reference_missing"
	autoTradeRejectOutcomePriceMiss autoTradeRejectCode = "outcome_price_missing"
	autoTradeRejectDiffMiss         autoTradeRejectCode = "diff_miss"
	autoTradeRejectProbabilityMiss  autoTradeRejectCode = "probability_miss"
	autoTradeRejectDataLag          autoTradeRejectCode = "data_lag"
	autoTradeRejectBlockedByState   autoTradeRejectCode = "blocked_by_state"
	autoTradeRejectRetryLimit       autoTradeRejectCode = "retry_limit"
	autoTradeRejectSignalConfirm    autoTradeRejectCode = "signal_confirm"
	autoTradeRejectBinanceVeto      autoTradeRejectCode = "binance_veto"
	autoTradeRejectNetEdgeMiss      autoTradeRejectCode = "net_edge_miss"
)

const minPolymarketBuyShares = 5.0
const strategyModeTailSweep = "tail-sweep"

// autoTradeDecision 表示一次自动交易评估的结果与归因。
type autoTradeDecision struct {
	plan       autoBuyPlan
	ok         bool
	reasonCode autoTradeRejectCode
	reason     string
}

type PolymarketService struct {
	cfg    infraPolymarket.Config
	repo   repository.PolymarketStateRepository
	client infraPolymarket.SDK
	feeds  infraMarketData.FeedClient
	logger *slog.Logger

	mu           sync.RWMutex
	state        entity.PolymarketState
	dashboard    entity.DashboardState
	activeMarket *entity.ActiveMarket
	autoTrade    entity.AutoTradeDiagnostics

	price struct {
		btc        *float64
		binance    *float64
		ptb        *float64
		ptbDisplay *float64
		upPrice    *float64
		downPrice  *float64
		upBid      *float64
		upAsk      *float64
		downBid    *float64
		downAsk    *float64

		btcUpdateTS        time.Time
		binanceUpdateTS    time.Time
		ptbDisplayUpdateTS time.Time
		upUpdateTS         time.Time
		downUpdateTS       time.Time
	}

	marketFeeRateBps int

	subscribers map[int]chan entity.DashboardState
	nextSubID   int

	cancelRoot   context.CancelFunc
	cancelMarket context.CancelFunc

	autoTradeGuard autoTradeRiskGuard
	autoTradeSizer autoTradeSizeAllocator

	signalConfirmKey   string
	signalConfirmSince time.Time

	nextPTB struct {
		slug      string
		target    *float64
		display   *float64
		fetchedAt time.Time
	}
}

// NewPolymarketService 创建机器人服务，并让编排层只依赖基建设施接口。
func NewPolymarketService(
	cfg infraPolymarket.Config,
	repo repository.PolymarketStateRepository,
	client infraPolymarket.SDK,
	feeds infraMarketData.FeedClient,
	logger *slog.Logger,
) *PolymarketService {
	return &PolymarketService{
		cfg:         cfg,
		repo:        repo,
		client:      client,
		feeds:       feeds,
		logger:      logger,
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
	}
}

// RegisterSinglePolymarketLifecycle 把单市场 worker 挂到 Fx 生命周期中。
func RegisterSinglePolymarketLifecycle(lc fx.Lifecycle, svc *PolymarketService) {
	lc.Append(fx.Hook{
		OnStart: svc.Start,
		OnStop:  svc.Stop,
	})
}

// Start 恢复本地状态，并启动所有后台轮询与订阅任务。
func (s *PolymarketService) Start(ctx context.Context) error {
	state, err := s.repo.Load(ctx)
	if err != nil {
		return err
	}

	// 先把持久化状态恢复到 dashboard，确保页面一启动就有可展示的快照。
	s.mu.Lock()
	if state != nil {
		s.state = *state
	}
	s.dashboard.Position = clonePosition(s.state.Position)
	s.dashboard.LastOrder = cloneLastOrder(s.state.LastOrder)
	s.dashboard.PendingOrder = s.dashboardPendingOrderLocked()
	s.dashboard.TradeHistory = cloneHistory(s.state.TradeHistory)
	s.mu.Unlock()
	s.publish()

	if !s.cfg.Enabled {
		s.addLog("WARN", "polymarket 功能已禁用")
		return nil
	}

	if s.cfg.AutoTrade && !s.client.HasPrivateKey() {
		s.cfg.AutoTrade = false
		s.addLog("WARN", "未配置 PRIVATE_KEY，已自动降级为监控模式")
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	s.cancelRoot = cancel

	s.addLog("OK", fmt.Sprintf("Polymarket bot 已启动，自动下单: %t", s.cfg.AutoTrade))
	go s.runRTDS(rootCtx)
	go s.runBinanceFeed(rootCtx)
	if s.cfg.EnableBalancePolling {
		go s.runBalancePoller(rootCtx)
	}
	if s.cfg.EnableAccountPolling {
		go s.runAccountSnapshotPoller(rootCtx)
	}
	if s.cfg.EnableAutoRedeemer {
		go s.runAutoRedeemer(rootCtx)
	}
	go s.runEngine(rootCtx)
	return nil
}

// Stop 停止所有后台任务，并清理已注册的订阅者。
func (s *PolymarketService) Stop(_ context.Context) error {
	if s.cancelMarket != nil {
		s.cancelMarket()
	}
	if s.cancelRoot != nil {
		s.cancelRoot()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	return nil
}

// Snapshot 返回 dashboard 快照的深拷贝。
func (s *PolymarketService) Snapshot() entity.DashboardState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDashboard(s.dashboard)
}

// Logs 返回 dashboard 活动日志列表。
func (s *PolymarketService) Logs() []entity.ActivityLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneActivity(s.dashboard.Activity)
}

// History 返回 dashboard 历史区所需的数据，并保持与 Python 版相同的优先级。
func (s *PolymarketService) History() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildHistoryPayloadLocked()
}

// Subscribe 创建一个新的 dashboard 状态订阅。
func (s *PolymarketService) Subscribe() (int, <-chan entity.DashboardState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan entity.DashboardState, 1)
	ch <- cloneDashboard(s.dashboard)
	s.subscribers[id] = ch
	return id, ch
}

// Unsubscribe 注销一个既有的 dashboard 订阅。
func (s *PolymarketService) Unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subscribers[id]; ok {
		close(ch)
		delete(s.subscribers, id)
	}
}

// DefaultTradeAmount 返回当前机器人配置的默认下单金额，供 TUI 表单初始化使用。
func (s *PolymarketService) DefaultTradeAmount() float64 {
	return s.cfg.TradeAmount
}

// SubmitTUIQuickOrder 按当前盘口快照构造一个快捷手动单，供终端热键直接调用。
func (s *PolymarketService) SubmitTUIQuickOrder(ctx context.Context, action, outcome string) (*dto.ManualOrderResp, error) {
	s.mu.RLock()
	req, err := s.buildTUIQuickOrderReqLocked(action, outcome)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	resp, err := s.SubmitManualOrder(ctx, req)
	if err != nil {
		return nil, err
	}

	s.addLog(
		"TRADE",
		fmt.Sprintf("TUI 快捷下单已提交: %s %s @ %.2f%%", resp.Action, resp.Outcome, resp.Price*100),
	)
	return resp, nil
}

// CancelActiveOrder 撤销当前最值得用户关注的挂单，优先撤买单，其次撤止盈单。
func (s *PolymarketService) CancelActiveOrder(ctx context.Context) error {
	s.mu.RLock()
	pending := clonePendingOrder(s.state.PendingOrder)
	takeProfit := clonePendingOrder(s.state.TakeProfitOrder)
	s.mu.RUnlock()

	target := pending
	targetKind := "pending"
	if target == nil {
		target = takeProfit
		targetKind = "take_profit"
	}
	if target == nil || strings.TrimSpace(target.OrderID) == "" {
		return errors.New("当前没有可撤销挂单")
	}

	if err := s.client.CancelOrder(ctx, target.OrderID); err != nil {
		return err
	}

	s.mu.Lock()
	switch targetKind {
	case "pending":
		if s.state.PendingOrder == nil || s.state.PendingOrder.OrderID != target.OrderID {
			s.mu.Unlock()
			return nil
		}
		s.state.PendingOrder = nil
	case "take_profit":
		if s.state.TakeProfitOrder == nil || s.state.TakeProfitOrder.OrderID != target.OrderID {
			s.mu.Unlock()
			return nil
		}
		s.state.TakeProfitOrder = nil
	}
	s.appendHistoryLocked(entity.TradeHistoryItem{
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Slug:    target.Slug,
		Action:  target.Action,
		Side:    target.Side,
		Price:   target.Price,
		Amount:  target.Amount,
		Size:    target.Size,
		OrderID: target.OrderID,
		Status:  "cancelled",
		Reason:  "tui_cancel",
	})
	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()

	s.addLog("WARN", fmt.Sprintf("TUI 已撤单: %s", target.OrderID))
	return nil
}

// SubmitManualOrder 处理手动买卖请求，并把结果写回本地状态。
func (s *PolymarketService) SubmitManualOrder(ctx context.Context, req dto.ManualOrderReq) (*dto.ManualOrderResp, error) {
	action := strings.ToUpper(strings.TrimSpace(req.Action))
	outcome := strings.ToUpper(strings.TrimSpace(req.Outcome))
	intent := strings.TrimSpace(req.Intent)
	if intent == "" {
		intent = "manual_panel"
	}
	if action != "BUY" && action != "SELL" {
		return nil, errors.New("action 必须是 BUY 或 SELL")
	}
	if outcome != "UP" && outcome != "DOWN" {
		return nil, errors.New("outcome 必须是 UP 或 DOWN")
	}
	if req.Probability < 0.01 || req.Probability > 0.99 {
		return nil, errors.New("probability 必须在 0.01 到 0.99 之间")
	}
	if !s.client.HasPrivateKey() {
		return nil, errors.New("未配置 PRIVATE_KEY，无法手动下单")
	}

	s.mu.RLock()
	market := cloneActiveMarket(s.activeMarket)
	position := clonePosition(s.state.Position)
	s.mu.RUnlock()

	if market == nil || market.Slug == "" {
		return nil, errors.New("当前没有活跃市场")
	}

	var tokenID string
	if outcome == "UP" {
		tokenID = market.UpToken
	} else {
		tokenID = market.DownToken
	}
	if tokenID == "" {
		return nil, errors.New("当前市场 token 信息不完整")
	}

	if action == "BUY" && req.Amount <= 0 {
		return nil, errors.New("买入金额必须大于 0")
	}

	var size float64
	if action == "BUY" {
		size = req.Amount / req.Probability
	} else {
		if position == nil || strings.ToUpper(position.Side) != outcome {
			return nil, errors.New("当前没有可卖出的匹配持仓")
		}
		size = position.Size
	}
	if size <= 0 {
		return nil, errors.New("计算得到的下单份额无效")
	}
	if action == "BUY" {
		if err := validateMinimumBuyShares(req.Amount, req.Probability); err != nil {
			return nil, err
		}
	}

	// 服务层只负责业务校验与状态维护，实际下单仍统一走底层 SDK。
	orderID, normalizedSize, err := s.client.PlaceLimitOrder(ctx, tokenID, action, req.Probability, size)
	if err != nil {
		nowISO := time.Now().Format(time.RFC3339)
		nowText := time.Now().Format("2006-01-02 15:04:05")
		s.mu.Lock()
		if action == "BUY" {
			s.state.LastOrder = &entity.LastOrder{
				Key:        market.Slug + "|" + outcome + "|manual",
				Time:       nowISO,
				RetryCount: 1,
				LastPrice:  req.Probability,
				Error:      err.Error(),
			}
		}
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:    nowText,
			Slug:    market.Slug,
			Action:  action,
			Side:    outcome,
			Price:   req.Probability,
			Amount:  req.Amount,
			Size:    size,
			OrderID: "",
			Status:  "failed",
			Reason:  intent,
			Error:   err.Error(),
		})
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		s.addLog("ERR", fmt.Sprintf("手动下单失败: %v", err))
		return nil, err
	}

	nowISO := time.Now().Format(time.RFC3339)
	nowText := time.Now().Format("2006-01-02 15:04:05")
	resp := &dto.ManualOrderResp{
		OK:      true,
		Action:  action,
		Intent:  intent,
		Outcome: outcome,
		OrderID: orderID,
		Price:   req.Probability,
		Size:    normalizedSize,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch action {
	case "BUY":
		s.state.PendingOrder = &entity.PendingOrder{
			OrderID: orderID,
			Time:    nowISO,
			Slug:    market.Slug,
			Side:    outcome,
			Action:  "BUY",
			Reason:  intent,
			Price:   req.Probability,
			Size:    normalizedSize,
			Amount:  req.Amount,
		}
		s.state.LastOrder = &entity.LastOrder{
			Key:        market.Slug + "|" + outcome + "|manual",
			Time:       nowISO,
			RetryCount: 1,
			LastPrice:  req.Probability,
			Error:      "",
		}
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:    nowText,
			Slug:    market.Slug,
			Action:  "BUY",
			Side:    outcome,
			Price:   req.Probability,
			Amount:  req.Amount,
			Size:    normalizedSize,
			OrderID: orderID,
			Status:  "submitted",
			Reason:  intent,
		})
	case "SELL":
		s.state.TakeProfitOrder = &entity.PendingOrder{
			OrderID: orderID,
			Time:    nowISO,
			Slug:    market.Slug,
			Side:    outcome,
			Action:  "SELL",
			Reason:  intent,
			Price:   req.Probability,
			Size:    normalizedSize,
			Amount:  position.Amount,
		}
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:    nowText,
			Slug:    market.Slug,
			Action:  "SELL",
			Side:    outcome,
			Price:   req.Probability,
			Amount:  position.Amount,
			Size:    normalizedSize,
			OrderID: orderID,
			Status:  "submitted",
			Reason:  intent,
		})
	}

	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	return resp, nil
}

// buildTUIQuickOrderReqLocked 从当前内存快照推导出适合终端热键的手动下单参数。
func (s *PolymarketService) buildTUIQuickOrderReqLocked(action, outcome string) (dto.ManualOrderReq, error) {
	action = strings.ToUpper(strings.TrimSpace(action))
	outcome = strings.ToUpper(strings.TrimSpace(outcome))
	position := clonePosition(s.state.Position)

	if action != "BUY" && action != "SELL" {
		return dto.ManualOrderReq{}, errors.New("快捷下单仅支持 BUY 或 SELL")
	}

	// 快捷卖出默认卖当前持仓方向，避免热键还要额外输入方向。
	if action == "SELL" && outcome == "" && position != nil {
		outcome = strings.ToUpper(strings.TrimSpace(position.Side))
	}
	if outcome != "UP" && outcome != "DOWN" {
		return dto.ManualOrderReq{}, errors.New("当前无法确定下单方向")
	}
	if action == "SELL" {
		if position == nil {
			return dto.ManualOrderReq{}, errors.New("当前没有可卖出的持仓")
		}
		if !strings.EqualFold(strings.TrimSpace(position.Side), outcome) {
			return dto.ManualOrderReq{}, errors.New("当前持仓方向与卖出方向不一致")
		}
	}

	price := s.quickOrderPriceLocked(action, outcome)
	if price <= 0 {
		return dto.ManualOrderReq{}, errors.New("当前盘口价格不足，暂时无法快捷下单")
	}

	req := dto.ManualOrderReq{
		Action:      action,
		Outcome:     outcome,
		Probability: clampProbability(price),
		Intent:      "tui_hotkey",
	}
	if action == "BUY" {
		req.Amount = s.cfg.TradeAmount
	}
	return req, nil
}

// runRTDS 持续监听链上参考价，并更新内存里的 BTC 现价。
func (s *PolymarketService) runRTDS(ctx context.Context) {
	err := s.feeds.SubscribeRTDS(ctx, func(price float64) {
		now := time.Now()
		s.mu.Lock()
		s.price.btc = cloneFloatPtr(&price)
		s.price.btcUpdateTS = now
		s.mu.Unlock()
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		s.addLog("ERR", fmt.Sprintf("Chainlink 价格监听停止: %v", err))
	}
}

// runBinanceFeed 以 websocket 为主、HTTP 为兜底地刷新 Binance 参考价。
func (s *PolymarketService) runBinanceFeed(ctx context.Context) {
	go s.runBinanceFallbackPoller(ctx)

	err := s.feeds.SubscribeBinanceBTC(ctx, func(price float64) {
		now := time.Now()
		s.mu.Lock()
		s.price.binance = cloneFloatPtr(&price)
		s.price.binanceUpdateTS = now
		s.mu.Unlock()
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		s.addLog("WARN", fmt.Sprintf("Binance WebSocket 停止，已退回 HTTP 兜底: %v", err))
	}
}

// runBinanceFallbackPoller 在 websocket 长时间未更新时，用 HTTP 补一份参考价。
func (s *PolymarketService) runBinanceFallbackPoller(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.cfg.PriceRefreshSec) * time.Second)
	defer ticker.Stop()
	for {
		s.mu.RLock()
		lastUpdate := s.price.binanceUpdateTS
		s.mu.RUnlock()

		// 只有 websocket 尚未建立或明显过期时才回退到 HTTP，避免覆盖主通道数据。
		if !lastUpdate.IsZero() && time.Since(lastUpdate) < time.Duration(s.cfg.PriceRefreshSec*2)*time.Second {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}

		if price, err := s.feeds.GetBinanceBTCPrice(ctx); err == nil {
			now := time.Now()
			s.mu.Lock()
			s.price.binance = cloneFloatPtr(&price)
			s.price.binanceUpdateTS = now
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runBalancePoller 周期性刷新钱包 USDC 余额。
func (s *PolymarketService) runBalancePoller(ctx context.Context) {
	account := s.client.FunderHex()
	if account == "" {
		account = s.client.AddressHex()
	}
	if account == "" {
		return
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		balance, err := s.client.GetERC20Balance(ctx, account)
		if err == nil {
			s.mu.Lock()
			s.dashboard.WalletBalance = cloneFloatPtr(balance)
			s.publishLocked()
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runEngine 驱动机器人主循环，统一调度市场刷新、挂单处理和仓位管理。
func (s *PolymarketService) runEngine(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(float64(time.Second) * s.cfg.LoopIntervalSec))
	defer ticker.Stop()

	var lastMarketFetch time.Time
	for {
		now := time.Now()
		if lastMarketFetch.IsZero() || now.Sub(lastMarketFetch) >= time.Duration(s.cfg.MarketMetaRefreshSec)*time.Second {
			market, err := s.client.GetActiveMarket(ctx)
			if err == nil {
				s.updateActiveMarket(ctx, market)
			}
			lastMarketFetch = now
		}

		s.prewarmNextPTBIfNeeded(ctx)
		s.refreshPTBIfNeeded(ctx)
		s.processPendingBuy(ctx)
		s.processTakeProfitOrder(ctx)
		s.evaluateAutoTrade(ctx)
		s.evaluateTailSweep(ctx)
		s.managePosition(ctx)

		s.mu.Lock()
		s.syncDashboardLocked()
		s.publishLocked()
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// updateActiveMarket 处理市场切换，并在切换后重建盘口订阅。
func (s *PolymarketService) updateActiveMarket(ctx context.Context, market *entity.ActiveMarket) {
	s.mu.Lock()
	currentSlug := ""
	if s.activeMarket != nil {
		currentSlug = s.activeMarket.Slug
	}
	newSlug := ""
	if market != nil {
		newSlug = market.Slug
	}
	changed := currentSlug != newSlug
	if !changed {
		if market != nil {
			s.activeMarket = cloneActiveMarket(market)
			s.autoTrade.MarketSlug = market.Slug
		}
		s.mu.Unlock()
		return
	}
	if s.cancelMarket != nil {
		s.cancelMarket()
		s.cancelMarket = nil
	}
	s.activeMarket = cloneActiveMarket(market)
	s.resetAutoTradeDiagnosticsLocked(newSlug)
	s.resetSignalConfirmationLocked()

	// 市场切换后，上一轮价格与仓位上下文都必须清空，避免跨市场污染。
	s.price.ptb = nil
	s.price.ptbDisplay = nil
	s.price.upPrice = nil
	s.price.downPrice = nil
	s.price.upBid = nil
	s.price.upAsk = nil
	s.price.downBid = nil
	s.price.downAsk = nil
	s.price.ptbDisplayUpdateTS = time.Time{}
	s.price.upUpdateTS = time.Time{}
	s.price.downUpdateTS = time.Time{}
	s.marketFeeRateBps = 0

	// 如果上一轮已经预热到下一轮 PTB，这里在切盘瞬间直接接管，
	// 避免新轮次开始后的前几十秒一直空着等待外部接口就绪。
	if market != nil && strings.EqualFold(strings.TrimSpace(s.nextPTB.slug), strings.TrimSpace(newSlug)) {
		s.price.ptb = cloneFloatPtr(s.nextPTB.target)
		s.price.ptbDisplay = cloneFloatPtr(s.nextPTB.display)
		if s.price.ptbDisplay != nil || s.price.ptb != nil {
			s.price.ptbDisplayUpdateTS = time.Now()
		}
	}
	if market == nil || !strings.EqualFold(strings.TrimSpace(s.nextPTB.slug), strings.TrimSpace(newSlug)) {
		s.nextPTB.slug = ""
		s.nextPTB.target = nil
		s.nextPTB.display = nil
		s.nextPTB.fetchedAt = time.Time{}
	}
	s.state.Position = nil
	s.state.LastOrder = nil
	s.state.TakeProfitOrder = nil
	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()

	if market == nil {
		s.addLog("WARN", "当前没有活跃市场")
		return
	}

	s.addLog("OK", fmt.Sprintf("切换到市场 %s", market.Slug))
	if feeRate, err := s.client.GetFeeRateBps(ctx, market.UpToken); err == nil {
		s.mu.Lock()
		s.marketFeeRateBps = feeRate
		s.mu.Unlock()
	}
	marketCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancelMarket = cancel
	s.mu.Unlock()
	go func(upToken, downToken string) {
		err := s.client.SubscribeMarket(marketCtx, upToken, downToken, func(assetID string, bid, ask, display float64) {
			now := time.Now()
			s.mu.Lock()
			if s.activeMarket == nil {
				s.mu.Unlock()
				return
			}
			// 市场切换后，旧 websocket 回调可能会晚到一步。
			// 这里强校验当前活跃市场的 token，避免上一轮盘口把新一轮状态覆盖掉。
			if !strings.EqualFold(strings.TrimSpace(s.activeMarket.UpToken), strings.TrimSpace(upToken)) ||
				!strings.EqualFold(strings.TrimSpace(s.activeMarket.DownToken), strings.TrimSpace(downToken)) {
				s.mu.Unlock()
				return
			}
			if assetID == upToken {
				s.price.upBid = cloneFloatPtr(&bid)
				s.price.upAsk = cloneFloatPtr(&ask)
				s.price.upPrice = cloneFloatPtr(&display)
				s.price.upUpdateTS = now
			}
			if assetID == downToken {
				s.price.downBid = cloneFloatPtr(&bid)
				s.price.downAsk = cloneFloatPtr(&ask)
				s.price.downPrice = cloneFloatPtr(&display)
				s.price.downUpdateTS = now
			}
			s.mu.Unlock()
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			s.addLog("ERR", fmt.Sprintf("市场行情监听停止: %v", err))
		}
	}(market.UpToken, market.DownToken)
}

// refreshPTBIfNeeded 刷新当前市场的两类参考价。
// 1. `ptb` 只保存该轮固定的策略目标价，对应 crypto-price 的 openPrice。
// 2. `ptbDisplay` 保存页面当前参考价，对应 crypto-price 的 closePrice，用于展示层对齐官网。
func (s *PolymarketService) refreshPTBIfNeeded(ctx context.Context) {
	s.mu.RLock()
	market := cloneActiveMarket(s.activeMarket)
	hasTarget := s.price.ptb != nil
	lastDisplayUpdate := s.price.ptbDisplayUpdateTS
	s.mu.RUnlock()
	if market == nil {
		return
	}
	displayFresh := !lastDisplayUpdate.IsZero() && time.Since(lastDisplayUpdate) < time.Duration(s.cfg.PriceRefreshSec)*time.Second
	if hasTarget && displayFresh {
		return
	}

	openPrice, closePrice, err := s.feeds.GetCryptoPrice(ctx, market.Start, market.End)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 外部请求返回时，市场可能已经切到下一轮；这里再次校验 slug，避免旧结果写回新市场。
	if s.activeMarket == nil || !strings.EqualFold(strings.TrimSpace(s.activeMarket.Slug), strings.TrimSpace(market.Slug)) {
		return
	}
	if s.price.ptb == nil && openPrice != nil {
		s.price.ptb = cloneFloatPtr(openPrice)
	}
	if closePrice != nil {
		s.price.ptbDisplay = cloneFloatPtr(closePrice)
		s.price.ptbDisplayUpdateTS = time.Now()
		return
	}
	// 某些轮次接口可能暂时拿不到 closePrice；此时展示层退回到 openPrice，避免空值。
	if s.price.ptbDisplay == nil && openPrice != nil {
		s.price.ptbDisplay = cloneFloatPtr(openPrice)
		s.price.ptbDisplayUpdateTS = time.Now()
	}
}

// prewarmNextPTBIfNeeded 在当前轮还未结束时提前预热下一轮 PTB，
// 避免切盘后还要等待 Gamma / crypto-price 同步完成才出现参考价。
func (s *PolymarketService) prewarmNextPTBIfNeeded(ctx context.Context) {
	s.mu.RLock()
	market := cloneActiveMarket(s.activeMarket)
	cachedSlug := strings.TrimSpace(s.nextPTB.slug)
	lastFetch := s.nextPTB.fetchedAt
	s.mu.RUnlock()
	if market == nil {
		return
	}

	remaining := remainingSeconds(market.End)
	if remaining <= 0 || remaining > nextPTBPrewarmLeadSec(s.cfg.ResolvedMarketIntervalSec()) {
		return
	}

	endAt, ok := parseRFC3339Loose(market.End)
	if !ok {
		return
	}

	intervalSec := s.cfg.ResolvedMarketIntervalSec()
	nextStart := endAt.UTC()
	nextEnd := nextStart.Add(time.Duration(intervalSec) * time.Second)
	nextSlug := fmt.Sprintf("%s-%s-%d", s.cfg.ResolvedMarketSlugPrefix(), s.cfg.ResolvedMarketSlugInterval(), nextStart.Unix())
	if strings.EqualFold(cachedSlug, nextSlug) && !lastFetch.IsZero() && time.Since(lastFetch) < time.Duration(s.cfg.PriceRefreshSec)*time.Second {
		return
	}

	openPrice, closePrice, err := s.feeds.GetCryptoPrice(ctx, nextStart.Format(time.RFC3339), nextEnd.Format(time.RFC3339))
	if err != nil {
		return
	}

	displayPrice := closePrice
	if displayPrice == nil {
		displayPrice = openPrice
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// 只保留下一轮的预热值，避免旧轮次结果覆盖当前计划切换目标。
	s.nextPTB.slug = nextSlug
	s.nextPTB.target = cloneFloatPtr(openPrice)
	s.nextPTB.display = cloneFloatPtr(displayPrice)
	s.nextPTB.fetchedAt = time.Now()
}

// processPendingBuy 检查超时未完成的买单，并决定确认成交或撤单。
func (s *PolymarketService) processPendingBuy(ctx context.Context) {
	s.mu.RLock()
	pending := clonePendingOrder(s.state.PendingOrder)
	diff := s.currentDiffLocked()
	s.mu.RUnlock()

	if pending == nil || strings.ToUpper(pending.Action) != "BUY" {
		return
	}
	createdAt, err := time.Parse(time.RFC3339, pending.Time)
	if err != nil || time.Since(createdAt) < time.Duration(s.cfg.OrderTimeoutSec)*time.Second {
		return
	}

	// 只有在挂单超时后才去查询权威订单状态，避免频繁访问 Data API。
	status, err := s.client.GetOrderStatus(ctx, pending.OrderID)
	if err != nil {
		s.addLog("WARN", fmt.Sprintf("查询订单状态失败: %v", err))
		return
	}

	s.mu.Lock()
	if s.state.PendingOrder == nil || s.state.PendingOrder.OrderID != pending.OrderID {
		s.mu.Unlock()
		return
	}

	logLevel := ""
	logMessage := ""
	if status.Filled {
		filledAmount := pending.Price * status.SizeMatched
		s.state.Position = &entity.Position{
			Slug:        pending.Slug,
			Side:        pending.Side,
			EntryPrice:  pending.Price,
			EntryDiff:   absFloat(diff),
			Size:        status.SizeMatched,
			Amount:      filledAmount,
			WindowSec:   pending.WindowSec,
			StrategyKey: pending.StrategyKey,
			Execution:   pending.Execution,
		}
		s.state.PendingOrder = nil
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:        time.Now().Format("2006-01-02 15:04:05"),
			Slug:        pending.Slug,
			Action:      "BUY",
			Side:        pending.Side,
			Price:       pending.Price,
			Amount:      filledAmount,
			Size:        status.SizeMatched,
			OrderID:     pending.OrderID,
			Status:      "filled",
			Reason:      pending.Reason,
			Diff:        floatPtr(diff),
			WindowSec:   pending.WindowSec,
			StrategyKey: pending.StrategyKey,
			Execution:   pending.Execution,
			PostOnly:    pending.PostOnly,
		})
		logLevel = "TRADE"
		logMessage = fmt.Sprintf("买单已成交: %s @ %.2f%%", pending.Side, pending.Price*100)
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		if logMessage != "" {
			s.addLog(logLevel, logMessage)
		}
		return
	}
	s.mu.Unlock()

	// 部分成交的买单要先撤掉剩余部分，再把已成交仓位收口到本地状态。
	if status.SizeMatched > 0 {
		if err := s.client.CancelOrder(ctx, pending.OrderID); err != nil {
			s.addLog("WARN", fmt.Sprintf("买单部分成交后撤余单失败: %s: %v", pending.OrderID, err))
			return
		}

		s.mu.Lock()
		if s.state.PendingOrder == nil || s.state.PendingOrder.OrderID != pending.OrderID {
			s.mu.Unlock()
			return
		}

		filledAmount := pending.Price * status.SizeMatched
		s.state.Position = &entity.Position{
			Slug:        pending.Slug,
			Side:        pending.Side,
			EntryPrice:  pending.Price,
			EntryDiff:   absFloat(diff),
			Size:        status.SizeMatched,
			Amount:      filledAmount,
			WindowSec:   pending.WindowSec,
			StrategyKey: pending.StrategyKey,
			Execution:   pending.Execution,
		}
		s.state.PendingOrder = nil
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:        time.Now().Format("2006-01-02 15:04:05"),
			Slug:        pending.Slug,
			Action:      "BUY",
			Side:        pending.Side,
			Price:       pending.Price,
			Amount:      filledAmount,
			Size:        status.SizeMatched,
			OrderID:     pending.OrderID,
			Status:      "partial_filled",
			Reason:      pending.Reason,
			Diff:        floatPtr(diff),
			WindowSec:   pending.WindowSec,
			StrategyKey: pending.StrategyKey,
			Execution:   pending.Execution,
			PostOnly:    pending.PostOnly,
		})
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		s.addLog("TRADE", fmt.Sprintf("买单部分成交，剩余已撤单: %s @ %.2f%% | size=%.4f", pending.Side, pending.Price*100, status.SizeMatched))
		return
	}

	if err := s.client.CancelOrder(ctx, pending.OrderID); err != nil {
		s.addLog("WARN", fmt.Sprintf("买单超时后撤单失败: %s: %v", pending.OrderID, err))
		return
	}

	s.mu.Lock()
	if s.state.PendingOrder == nil || s.state.PendingOrder.OrderID != pending.OrderID {
		s.mu.Unlock()
		return
	}
	fallbackPlan, fallbackMarket, fallbackReason, canFallback := s.buildMakerTimeoutFallbackPlanLocked(pending)
	s.state.PendingOrder = nil
	if !canFallback {
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		if strings.TrimSpace(fallbackReason) != "" {
			s.addLog("WARN", fmt.Sprintf("买单超时未成交，已撤单: %s | %s", pending.OrderID, fallbackReason))
		} else {
			s.addLog("WARN", fmt.Sprintf("买单超时未成交，已撤单: %s", pending.OrderID))
		}
		return
	}

	s.mu.Unlock()
	s.addLog("WARN", fmt.Sprintf("maker 买单超时未成交，已自动回退为普通限价单: %s @ %.2f%%", fallbackPlan.side, fallbackPlan.price*100))
	s.executeBuyPlan(ctx, fallbackMarket, fallbackPlan, "maker 超时回退下单失败", "maker 超时回退买单已提交")
}

// processTakeProfitOrder 检查当前卖出挂单状态，并在成交后清理仓位。
func (s *PolymarketService) processTakeProfitOrder(ctx context.Context) {
	s.mu.RLock()
	tpOrder := clonePendingOrder(s.state.TakeProfitOrder)
	s.mu.RUnlock()
	if tpOrder == nil {
		return
	}

	status, err := s.client.GetOrderStatus(ctx, tpOrder.OrderID)
	if err != nil {
		return
	}

	normalizedStatus := strings.ToUpper(status.Status)
	s.mu.Lock()
	if s.state.TakeProfitOrder == nil || s.state.TakeProfitOrder.OrderID != tpOrder.OrderID {
		s.mu.Unlock()
		return
	}

	logLevel := ""
	logMessage := ""
	reason := strings.TrimSpace(tpOrder.Reason)
	if reason == "" {
		reason = "take_profit"
	}
	switch {
	case status.Filled:
		filledAmount := tpOrder.Price * status.SizeMatched
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:        time.Now().Format("2006-01-02 15:04:05"),
			Slug:        tpOrder.Slug,
			Action:      "SELL",
			Side:        tpOrder.Side,
			Price:       tpOrder.Price,
			Amount:      filledAmount,
			Size:        status.SizeMatched,
			OrderID:     tpOrder.OrderID,
			Status:      "filled",
			Reason:      reason,
			WindowSec:   tpOrder.WindowSec,
			StrategyKey: tpOrder.StrategyKey,
			Execution:   tpOrder.Execution,
			PostOnly:    tpOrder.PostOnly,
		})
		s.state.TakeProfitOrder = nil
		s.state.Position = nil
		logLevel = "TRADE"
		logMessage = fmt.Sprintf("卖单已成交: %s @ %.2f%%", tpOrder.Side, tpOrder.Price*100)
	case normalizedStatus == "CANCELED" || normalizedStatus == "CANCELLED" || normalizedStatus == "REJECTED" || normalizedStatus == "EXPIRED":
		s.state.TakeProfitOrder = nil
		logLevel = "WARN"
		logMessage = fmt.Sprintf("卖单已失效，等待再次触发: %s", reason)
	default:
		s.mu.Unlock()
		return
	}
	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()
	if logMessage != "" {
		s.addLog(logLevel, logMessage)
	}
}

// evaluateAutoTrade 根据当前行情条件评估是否需要自动入场。
func (s *PolymarketService) evaluateAutoTrade(ctx context.Context) {
	s.mu.Lock()
	market := cloneActiveMarket(s.activeMarket)
	decision := s.buildAutoBuyDecisionLocked()
	s.recordAutoTradeDecisionLocked(decision)
	if decision.ok {
		// 信号一旦真正放行，就重置确认窗口；后续若还想继续加仓，必须重新确认。
		s.resetSignalConfirmationLocked()
	}
	s.mu.Unlock()

	if !decision.ok || market == nil {
		return
	}
	plan := decision.plan

	if !s.cfg.AutoTrade {
		s.addLog("TRADE", fmt.Sprintf("提醒模式: 建议买入 %s @ %.2f%%", plan.side, plan.price*100))
		s.mu.Lock()
		s.state.LastOrder = &entity.LastOrder{
			Key:        plan.orderKey,
			Time:       time.Now().Format(time.RFC3339),
			RetryCount: plan.retryCount + 1,
			LastPrice:  plan.price,
			Error:      "",
		}
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		return
	}

	s.executeBuyPlan(ctx, market, plan, "自动下单失败", "自动买单已提交")
}

// evaluateTailSweep 在尾盘窗口内尝试执行独立的扫尾巴策略。
func (s *PolymarketService) evaluateTailSweep(ctx context.Context) {
	s.mu.Lock()
	market := cloneActiveMarket(s.activeMarket)
	decision := s.buildTailSweepDecisionLocked()
	s.mu.Unlock()

	if !decision.ok || market == nil {
		return
	}
	if !s.cfg.AutoTrade {
		plan := decision.plan
		s.addLog("TRADE", fmt.Sprintf("提醒模式: 建议尾盘扫尾买入 %s @ %.2f%%", plan.side, plan.price*100))
		s.mu.Lock()
		s.state.LastOrder = &entity.LastOrder{
			Key:        plan.orderKey,
			Time:       time.Now().Format(time.RFC3339),
			RetryCount: plan.retryCount + 1,
			LastPrice:  plan.price,
			Error:      "",
		}
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		return
	}
	s.executeBuyPlan(ctx, market, decision.plan, "尾盘扫尾下单失败", "尾盘扫尾买单已提交")
}

type autoBuyPlan struct {
	side         string
	tokenID      string
	price        float64
	reason       string
	orderKey     string
	retryCount   int
	tradeAmount  float64
	diff         float64
	windowSec    int
	strategyKey  string
	execution    string
	postOnly     bool
	orderType    string
	expirationTS int64
	netEdgeBps   float64
}

// executeBuyPlan 统一提交自动买入计划，并把结果回写到本地状态与 dashboard。
func (s *PolymarketService) executeBuyPlan(ctx context.Context, market *entity.ActiveMarket, plan autoBuyPlan, failPrefix, successPrefix string) {
	submittedPlan, orderID, normalizedSize, err := s.submitAutoBuyPlan(ctx, plan)
	s.mu.Lock()
	if err != nil {
		s.state.LastOrder = &entity.LastOrder{
			Key:        plan.orderKey,
			Time:       time.Now().Format(time.RFC3339),
			RetryCount: plan.retryCount + 1,
			LastPrice:  plan.price,
			Error:      err.Error(),
		}
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:        time.Now().Format("2006-01-02 15:04:05"),
			Slug:        market.Slug,
			Action:      "BUY",
			Side:        plan.side,
			Price:       plan.price,
			Amount:      plan.tradeAmount,
			OrderID:     "",
			Status:      "failed",
			Reason:      plan.reason,
			Error:       err.Error(),
			Diff:        floatPtr(plan.diff),
			WindowSec:   plan.windowSec,
			StrategyKey: plan.strategyKey,
			Execution:   plan.execution,
			PostOnly:    plan.postOnly,
			NetEdgeBps:  floatPtr(plan.netEdgeBps),
		})
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		s.addLog("ERR", fmt.Sprintf("%s: %v", failPrefix, err))
		return
	}

	s.state.PendingOrder = &entity.PendingOrder{
		OrderID:     orderID,
		Time:        time.Now().Format(time.RFC3339),
		Slug:        market.Slug,
		Side:        submittedPlan.side,
		Action:      "BUY",
		Reason:      submittedPlan.reason,
		Price:       submittedPlan.price,
		Size:        normalizedSize,
		Amount:      submittedPlan.tradeAmount,
		WindowSec:   submittedPlan.windowSec,
		StrategyKey: submittedPlan.strategyKey,
		Execution:   submittedPlan.execution,
		PostOnly:    submittedPlan.postOnly,
	}
	s.state.LastOrder = &entity.LastOrder{
		Key:        plan.orderKey,
		Time:       time.Now().Format(time.RFC3339),
		RetryCount: plan.retryCount + 1,
		LastPrice:  plan.price,
		Error:      "",
	}
	s.appendHistoryLocked(entity.TradeHistoryItem{
		Time:        time.Now().Format("2006-01-02 15:04:05"),
		Slug:        market.Slug,
		Action:      "BUY",
		Side:        submittedPlan.side,
		Price:       submittedPlan.price,
		Amount:      submittedPlan.tradeAmount,
		Size:        normalizedSize,
		OrderID:     orderID,
		Status:      "submitted",
		Reason:      submittedPlan.reason,
		Diff:        floatPtr(submittedPlan.diff),
		WindowSec:   submittedPlan.windowSec,
		StrategyKey: submittedPlan.strategyKey,
		Execution:   submittedPlan.execution,
		PostOnly:    submittedPlan.postOnly,
		NetEdgeBps:  floatPtr(submittedPlan.netEdgeBps),
	})
	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()
	if plan.postOnly && !submittedPlan.postOnly {
		s.addLog("WARN", fmt.Sprintf("maker 入场穿价，已自动回退为普通限价单: %s @ %.2f%%", submittedPlan.side, submittedPlan.price*100))
	}
	s.addLog("TRADE", fmt.Sprintf("%s: %s @ %.2f%% [%s]", successPrefix, submittedPlan.side, submittedPlan.price*100, submittedPlan.execution))
}

// submitAutoBuyPlan 负责提交自动开仓计划，并在 maker post-only 穿价时自动回退成普通限价单。
func (s *PolymarketService) submitAutoBuyPlan(ctx context.Context, plan autoBuyPlan) (autoBuyPlan, string, float64, error) {
	orderID, normalizedSize, err := s.client.PlaceLimitOrderWithOptions(
		ctx,
		plan.tokenID,
		"BUY",
		plan.price,
		plan.tradeAmount/plan.price,
		infraPolymarket.PlaceOrderOptions{
			OrderType:    plan.orderType,
			ExpirationTS: plan.expirationTS,
			PostOnly:     plan.postOnly,
		},
	)
	if err == nil {
		return plan, orderID, normalizedSize, nil
	}
	if !plan.postOnly || !isPostOnlyCrossBookError(err) {
		return plan, "", 0, err
	}

	// maker 入场只要因为“穿价”被拒，就立刻降级为普通限价单，避免错过同一轮信号。
	fallback := plan
	fallback.postOnly = false
	fallback.orderType = "GTC"
	fallback.expirationTS = 0
	fallback.execution = "taker_gtc_fallback"

	orderID, normalizedSize, fallbackErr := s.client.PlaceLimitOrderWithOptions(
		ctx,
		fallback.tokenID,
		"BUY",
		fallback.price,
		fallback.tradeAmount/fallback.price,
		infraPolymarket.PlaceOrderOptions{
			OrderType:    fallback.orderType,
			ExpirationTS: fallback.expirationTS,
			PostOnly:     fallback.postOnly,
		},
	)
	if fallbackErr != nil {
		return plan, "", 0, fmt.Errorf("maker 入场被拒后回退失败: 首次=%v; 回退=%w", err, fallbackErr)
	}
	return fallback, orderID, normalizedSize, nil
}

// buildMakerTimeoutFallbackPlanLocked 在 maker 挂单超时后，判断是否还能安全回退成普通限价单。
func (s *PolymarketService) buildMakerTimeoutFallbackPlanLocked(pending *entity.PendingOrder) (autoBuyPlan, *entity.ActiveMarket, string, bool) {
	if pending == nil {
		return autoBuyPlan{}, nil, "挂单上下文缺失", false
	}
	if !pending.PostOnly && !strings.EqualFold(strings.TrimSpace(pending.Execution), "maker_gtd") {
		return autoBuyPlan{}, nil, "", false
	}
	if s.activeMarket == nil {
		return autoBuyPlan{}, nil, "当前没有活跃市场，已放弃回退追单", false
	}
	if remainingSeconds(s.activeMarket.End) <= 0 {
		return autoBuyPlan{}, nil, "当前市场已进入结算，已放弃回退追单", false
	}

	referencePrice := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	if referencePrice <= 0 || ptb <= 0 {
		return autoBuyPlan{}, nil, "外部参考价或 PTB 未就绪，已放弃回退追单", false
	}
	diff := referencePrice - ptb
	side := strings.ToUpper(strings.TrimSpace(pending.Side))
	if resolveAutoTradeConditionSide(diff) != side {
		return autoBuyPlan{}, nil, "超时后外部信号方向已反转，已放弃回退追单", false
	}

	var (
		price        float64
		bestBid      float64
		bestAsk      float64
		displayPrice float64
		tokenID      string
		lastUpdate   time.Time
	)
	switch side {
	case "UP":
		price = firstPositive(s.price.upAsk, s.price.upPrice, s.activeMarket.UpPrice)
		bestBid = derefFloat(s.price.upBid)
		bestAsk = derefFloat(s.price.upAsk)
		displayPrice = firstPositive(s.price.upPrice, s.activeMarket.UpPrice)
		tokenID = s.activeMarket.UpToken
		lastUpdate = s.price.upUpdateTS
	case "DOWN":
		price = firstPositive(s.price.downAsk, s.price.downPrice, s.activeMarket.DownPrice)
		bestBid = derefFloat(s.price.downBid)
		bestAsk = derefFloat(s.price.downAsk)
		displayPrice = firstPositive(s.price.downPrice, s.activeMarket.DownPrice)
		tokenID = s.activeMarket.DownToken
		lastUpdate = s.price.downUpdateTS
	default:
		return autoBuyPlan{}, nil, "挂单方向无效，已放弃回退追单", false
	}
	if price <= 0 || tokenID == "" {
		return autoBuyPlan{}, nil, "当前盘口价格未就绪，已放弃回退追单", false
	}

	if lastUpdate.IsZero() {
		lastUpdate = time.Now()
	}
	referenceUpdate := s.price.btcUpdateTS
	if referenceUpdate.IsZero() {
		referenceUpdate = time.Now()
	}
	sideAge := time.Since(lastUpdate).Seconds()
	referenceAge := time.Since(referenceUpdate).Seconds()
	if sideAge > s.cfg.MarketDataMaxLagSec || referenceAge > s.cfg.MarketDataMaxLagSec {
		return autoBuyPlan{}, nil, fmt.Sprintf(
			"超时后盘口延迟 %.2fs / 参考价延迟 %.2fs，已放弃回退追单",
			sideAge,
			referenceAge,
		), false
	}

	diffBps := calcRelativeDiffBps(referencePrice, ptb)
	condition, ok := s.resolveFallbackConditionLocked(pending.WindowSec, price, diffBps)
	if !ok {
		return autoBuyPlan{}, nil, "超时后当前信号已不再满足原窗口条件，已放弃回退追单", false
	}
	if slippage := entryPriceSlippage(price, displayPrice); slippage > s.cfg.SlippageThreshold {
		return autoBuyPlan{}, nil, fmt.Sprintf(
			"超时后候选价格滑点 %.4f 超过阈值 %.4f，已放弃回退追单",
			slippage,
			s.cfg.SlippageThreshold,
		), false
	}
	if ok, reason := s.validateBinanceEntryLocked(side, referencePrice, derefFloat(s.price.binance), ptb); !ok {
		return autoBuyPlan{}, nil, reason, false
	}

	thresholdBps := autoTradeThresholdBps(condition, referencePrice, ptb)
	netEdgeBps := estimateNetEdgeBps(
		math.Abs(diffBps),
		thresholdBps,
		price,
		displayPrice,
		bestBid,
		bestAsk,
		s.marketFeeRateBps,
		false,
	)
	if netEdgeBps < s.cfg.MinNetEdgeBps {
		return autoBuyPlan{}, nil, fmt.Sprintf(
			"超时后净边际 %.2fbps 低于阈值 %.2fbps，已放弃回退追单",
			netEdgeBps,
			s.cfg.MinNetEdgeBps,
		), false
	}

	tradeAmount := pending.Amount
	if tradeAmount <= 0 && pending.Size > 0 && pending.Price > 0 {
		tradeAmount = pending.Size * pending.Price
	}
	if tradeAmount <= 0 {
		tradeAmount = s.cfg.TradeAmount
	}
	if err := validateMinimumBuyShares(tradeAmount, price); err != nil {
		return autoBuyPlan{}, nil, err.Error(), false
	}

	orderKey := ""
	retryCount := 0
	if s.state.LastOrder != nil {
		orderKey = strings.TrimSpace(s.state.LastOrder.Key)
		retryCount = s.state.LastOrder.RetryCount
	}
	if orderKey == "" {
		orderKey = fmt.Sprintf("%s|timeout-fallback|%s", strings.TrimSpace(pending.Slug), side)
	}

	plan := autoBuyPlan{
		side:         side,
		tokenID:      tokenID,
		price:        price,
		reason:       fmt.Sprintf("maker 超时回退: 剩余≤%ds 且价差满足阈值(%s)", pending.WindowSec, formatConditionDiffThreshold(condition)),
		orderKey:     orderKey,
		retryCount:   retryCount,
		tradeAmount:  tradeAmount,
		diff:         diff,
		windowSec:    pending.WindowSec,
		strategyKey:  pending.StrategyKey,
		execution:    "maker_timeout_fallback",
		postOnly:     false,
		orderType:    "GTC",
		expirationTS: 0,
		netEdgeBps:   netEdgeBps,
	}
	return plan, cloneActiveMarket(s.activeMarket), "", true
}

// buildAutoBuyDecisionLocked 在持锁状态下评估本轮是否满足自动买入条件。
func (s *PolymarketService) buildAutoBuyDecisionLocked() autoTradeDecision {
	decision := autoTradeDecision{
		reasonCode: autoTradeRejectNoMarket,
		reason:     "当前没有活跃市场",
	}
	if s.activeMarket == nil {
		s.resetSignalConfirmationLocked()
		return decision
	}
	if s.cfg.MainStrategyConfigured && !s.cfg.MainStrategyEnabled {
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectBlockedByState
		decision.reason = "当前市场主策略已关闭，仅观察价格或等待尾盘策略"
		return decision
	}
	remaining := remainingSeconds(s.activeMarket.End)
	if remaining <= 0 {
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectMarketClosed
		decision.reason = "当前市场已结束，等待下一轮"
		return decision
	}
	referencePrice := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	binancePrice := derefFloat(s.price.binance)
	if referencePrice <= 0 || ptb <= 0 {
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectReferenceMiss
		decision.reason = "外部参考价或 PTB 参考价未就绪"
		return decision
	}
	diff := referencePrice - ptb
	diffBps := calcRelativeDiffBps(referencePrice, ptb)

	// 优先使用盘口 ask，其次退回展示价和市场快照价，尽量贴近真实可成交概率。
	upDisplay := firstPositive(s.price.upPrice, s.activeMarket.UpPrice)
	downDisplay := firstPositive(s.price.downPrice, s.activeMarket.DownPrice)
	upPrice := firstPositive(s.price.upAsk, s.price.upPrice, s.activeMarket.UpPrice)
	downPrice := firstPositive(s.price.downAsk, s.price.downPrice, s.activeMarket.DownPrice)
	upBid := derefFloat(s.price.upBid)
	downBid := derefFloat(s.price.downBid)
	if upPrice <= 0 || downPrice <= 0 {
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectOutcomePriceMiss
		decision.reason = "UP 或 DOWN 概率价格未就绪"
		return decision
	}

	windowMatched := false
	diffMatched := false
	probabilityMatched := false
	for _, condition := range orderedAutoTradeConditions(s.cfg.Conditions) {
		if condition.Time <= 0 {
			continue
		}
		if remaining > condition.Time {
			continue
		}
		windowMatched = true
		if !matchesAutoTradeDiff(condition, diffBps) {
			continue
		}
		diffMatched = true
		side := resolveAutoTradeConditionSide(diff)
		if side == "" {
			continue
		}

		var price float64
		var bestBid float64
		var bestAsk float64
		switch side {
		case "UP":
			price = upPrice
			bestBid = upBid
			bestAsk = derefFloat(s.price.upAsk)
		case "DOWN":
			price = downPrice
			bestBid = downBid
			bestAsk = derefFloat(s.price.downAsk)
		default:
			continue
		}
		if price < condition.MinProb || price > condition.MaxProb {
			continue
		}
		probabilityMatched = true

		var lastUpdate time.Time
		if side == "UP" {
			lastUpdate = s.price.upUpdateTS
		} else {
			lastUpdate = s.price.downUpdateTS
		}
		if lastUpdate.IsZero() {
			lastUpdate = time.Now()
		}
		referenceUpdate := s.price.btcUpdateTS
		if referenceUpdate.IsZero() {
			referenceUpdate = time.Now()
		}
		sideAge := time.Since(lastUpdate).Seconds()
		referenceAge := time.Since(referenceUpdate).Seconds()
		if sideAge > s.cfg.MarketDataMaxLagSec || referenceAge > s.cfg.MarketDataMaxLagSec {
			s.resetSignalConfirmationLocked()
			decision.reasonCode = autoTradeRejectDataLag
			decision.reason = fmt.Sprintf(
				"%s 盘口延迟 %.2fs / 参考价延迟 %.2fs，超过 %.2fs",
				side,
				sideAge,
				referenceAge,
				s.cfg.MarketDataMaxLagSec,
			)
			return decision
		}

		if s.state.PendingOrder != nil || s.state.Position != nil {
			s.resetSignalConfirmationLocked()
			decision.reasonCode = autoTradeRejectBlockedByState
			if s.state.PendingOrder != nil {
				decision.reason = "当前已有挂单，暂停新的自动开仓"
			} else {
				decision.reason = "当前已有持仓，暂停新的自动开仓"
			}
			return decision
		}
		orderKey := fmt.Sprintf("%s|slot-%d|%s", s.activeMarket.Slug, conditionSlot(condition), side)
		lastOrder := s.state.LastOrder
		retryCount := 0
		if lastOrder != nil && lastOrder.Key == orderKey {
			retryCount = lastOrder.RetryCount
			if retryCount >= s.cfg.MaxRetryPerMarket {
				s.resetSignalConfirmationLocked()
				decision.reasonCode = autoTradeRejectRetryLimit
				decision.reason = fmt.Sprintf("条件%d(%s) 已达到最大重试次数 %d", conditionSlot(condition), side, s.cfg.MaxRetryPerMarket)
				return decision
			}
			if lastOrder.LastPrice > 0 {
				// 同方向重试时只允许按最小步长追价，避免短时间内无约束抬价。
				capPrice := minFloat(0.995, lastOrder.LastPrice+s.cfg.BuyRetryStep)
				if price > capPrice {
					price = capPrice
				}
			}
		}
		referenceOutcomePrice := upDisplay
		if side == "DOWN" {
			referenceOutcomePrice = downDisplay
		}
		executionMode := "taker_gtc"
		orderType := "GTC"
		postOnly := false
		expirationTS := int64(0)
		if s.cfg.PreferPostOnly && retryCount == 0 && bestBid > 0 && bestBid < price {
			price = bestBid
			executionMode = "maker_gtd"
			orderType = "GTD"
			postOnly = true
			// GTD 订单至少多留几十秒缓冲，避免刚发出就接近到期。
			expirationTS = time.Now().Add(time.Duration(60+s.cfg.PostOnlyTTLSec) * time.Second).Unix()
		}
		slippage := entryPriceSlippage(price, referenceOutcomePrice)
		if slippage > s.cfg.SlippageThreshold {
			s.resetSignalConfirmationLocked()
			decision.reasonCode = autoTradeRejectProbabilityMiss
			decision.reason = fmt.Sprintf(
				"候选价格滑点 %.4f 超过阈值 %.4f (候选=%.4f 参考=%.4f)",
				slippage,
				s.cfg.SlippageThreshold,
				price,
				referenceOutcomePrice,
			)
			return decision
		}
		if ok, reason := s.validateBinanceEntryLocked(side, referencePrice, binancePrice, ptb); !ok {
			s.resetSignalConfirmationLocked()
			decision.reasonCode = autoTradeRejectBinanceVeto
			decision.reason = reason
			return decision
		}

		tokenID := s.activeMarket.UpToken
		if side == "DOWN" {
			tokenID = s.activeMarket.DownToken
		}
		thresholdBps := autoTradeThresholdBps(condition, referencePrice, ptb)
		netEdgeBps := estimateNetEdgeBps(
			math.Abs(diffBps),
			thresholdBps,
			price,
			referenceOutcomePrice,
			bestBid,
			bestAsk,
			s.marketFeeRateBps,
			postOnly,
		)
		if netEdgeBps < s.cfg.MinNetEdgeBps {
			s.resetSignalConfirmationLocked()
			decision.reasonCode = autoTradeRejectNetEdgeMiss
			decision.reason = fmt.Sprintf(
				"净边际 %.2fbps 低于阈值 %.2fbps (信号=%.2fbps, 阈值=%.2fbps)",
				netEdgeBps,
				s.cfg.MinNetEdgeBps,
				math.Abs(diffBps),
				thresholdBps,
			)
			return decision
		}
		strategyKey := buildStrategyKey(s.cfg.ResolvedMarketKey(), conditionSlot(condition), condition.Time, side)
		tradeAmount := s.cfg.TradeAmount
		if s.autoTradeSizer != nil {
			if sized := s.autoTradeSizer(autoTradeSizeRequest{
				MarketSlug:      s.activeMarket.Slug,
				MarketKey:       s.cfg.ResolvedMarketKey(),
				Side:            side,
				BaseTradeAmount: s.cfg.TradeAmount,
				WindowSec:       condition.Time,
				StrategyKey:     strategyKey,
			}); sized > 0 {
				tradeAmount = sized
			}
		}
		if err := validateMinimumBuyShares(tradeAmount, price); err != nil {
			s.resetSignalConfirmationLocked()
			decision.reasonCode = autoTradeRejectProbabilityMiss
			decision.reason = err.Error()
			return decision
		}
		decision.plan = autoBuyPlan{
			side:         side,
			tokenID:      tokenID,
			price:        price,
			reason:       fmt.Sprintf("条件%d: 剩余≤%ds 且价差满足阈值(%s)", conditionSlot(condition), condition.Time, formatConditionDiffThreshold(condition)),
			orderKey:     orderKey,
			retryCount:   retryCount,
			tradeAmount:  tradeAmount,
			diff:         diff,
			windowSec:    condition.Time,
			strategyKey:  strategyKey,
			execution:    executionMode,
			postOnly:     postOnly,
			orderType:    orderType,
			expirationTS: expirationTS,
			netEdgeBps:   netEdgeBps,
		}
		decision.ok = true
		decision.reasonCode = autoTradeRejectNone
		decision.reason = ""
		if ok, reason := s.confirmAutoTradeSignalLocked(orderKey); !ok {
			decision.ok = false
			decision.reasonCode = autoTradeRejectSignalConfirm
			decision.reason = reason
			return decision
		}
		if s.autoTradeGuard != nil {
			if err := s.autoTradeGuard(autoTradeRiskRequest{
				MarketSlug:   s.activeMarket.Slug,
				MarketKey:    s.cfg.ResolvedMarketKey(),
				Side:         side,
				TradeAmount:  tradeAmount,
				CurrentPrice: price,
				WindowSec:    condition.Time,
				StrategyKey:  strategyKey,
			}); err != nil {
				s.resetSignalConfirmationLocked()
				decision.ok = false
				decision.reasonCode = autoTradeRejectBlockedByState
				decision.reason = "全局风控拦截: " + err.Error()
			}
		}
		return decision
	}

	switch {
	case !windowMatched:
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectTimeWindowMiss
		decision.reason = fmt.Sprintf("剩余 %ds，尚未进入自动开仓窗口", remaining)
	case !diffMatched:
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectDiffMiss
		decision.reason = fmt.Sprintf(
			"当前价差 %.4f / %.2fbps 未达到配置阈值",
			diff,
			diffBps,
		)
	case !probabilityMatched:
		s.resetSignalConfirmationLocked()
		decision.reasonCode = autoTradeRejectProbabilityMiss
		decision.reason = fmt.Sprintf(
			"价格未落入区间: UP=%.4f DOWN=%.4f",
			upPrice,
			downPrice,
		)
	}
	return decision
}

// buildTailSweepDecisionLocked 在尾盘窗口内评估是否触发独立的扫尾巴策略。
func (s *PolymarketService) buildTailSweepDecisionLocked() autoTradeDecision {
	decision := autoTradeDecision{
		reasonCode: autoTradeRejectNoMarket,
		reason:     "尾盘扫尾策略未命中",
	}
	if !s.cfg.TailSweepEnabled || !s.cfg.TailSweepAllowed {
		return decision
	}
	if s.activeMarket == nil {
		return decision
	}

	remaining := remainingSeconds(s.activeMarket.End)
	if remaining <= 0 || s.cfg.TailSweepFinalSec <= 0 || remaining > s.cfg.TailSweepFinalSec {
		return decision
	}
	if s.state.PendingOrder != nil || s.state.Position != nil {
		return decision
	}

	referencePrice := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	binancePrice := derefFloat(s.price.binance)
	if referencePrice <= 0 || ptb <= 0 {
		return decision
	}

	diff := referencePrice - ptb
	diffBps := math.Abs(calcRelativeDiffBps(referencePrice, ptb))
	if diffBps < s.cfg.TailSweepMinDiffBps {
		return decision
	}
	side := resolveAutoTradeConditionSide(diff)
	if side == "" {
		return decision
	}

	var price float64
	var bestBid float64
	var bestAsk float64
	var lastUpdate time.Time
	tokenID := s.activeMarket.UpToken
	switch side {
	case "UP":
		price = firstPositive(s.price.upAsk, s.price.upPrice, s.activeMarket.UpPrice)
		bestBid = derefFloat(s.price.upBid)
		bestAsk = derefFloat(s.price.upAsk)
		lastUpdate = s.price.upUpdateTS
	case "DOWN":
		price = firstPositive(s.price.downAsk, s.price.downPrice, s.activeMarket.DownPrice)
		bestBid = derefFloat(s.price.downBid)
		bestAsk = derefFloat(s.price.downAsk)
		lastUpdate = s.price.downUpdateTS
		tokenID = s.activeMarket.DownToken
	}
	if price <= 0 || bestBid <= 0 || bestAsk <= 0 || tokenID == "" {
		return decision
	}

	maxProb := s.cfg.TailSweepMaxProb
	if s.cfg.TailSweepMaxPrice > 0 && (maxProb <= 0 || s.cfg.TailSweepMaxPrice < maxProb) {
		maxProb = s.cfg.TailSweepMaxPrice
	}
	if price < s.cfg.TailSweepMinProb || (maxProb > 0 && price > maxProb) {
		return decision
	}
	if bestAsk-bestBid > s.cfg.TailSweepMaxSpread {
		return decision
	}

	referenceUpdate := s.price.btcUpdateTS
	if referenceUpdate.IsZero() {
		referenceUpdate = time.Now()
	}
	if lastUpdate.IsZero() {
		lastUpdate = time.Now()
	}
	if time.Since(referenceUpdate).Seconds() > s.cfg.TailSweepMaxLagSec || time.Since(lastUpdate).Seconds() > s.cfg.TailSweepMaxLagSec {
		return decision
	}
	if ok, _ := validateBinanceEntry(
		side,
		referencePrice,
		binancePrice,
		ptb,
		s.cfg.TailSweepRequireBinance,
		s.cfg.BinanceConfirmMinBps,
		s.cfg.BinanceVetoMaxDevBps,
	); !ok {
		return decision
	}

	strategyKey := buildTailSweepStrategyKey(s.cfg.ResolvedMarketKey(), s.cfg.TailSweepFinalSec, side)
	orderKey := fmt.Sprintf("%s|tail-sweep|%ds|%s", s.activeMarket.Slug, s.cfg.TailSweepFinalSec, side)
	retryCount := 0
	if s.state.LastOrder != nil && s.state.LastOrder.Key == orderKey {
		retryCount = s.state.LastOrder.RetryCount
		if retryCount >= s.cfg.MaxRetryPerMarket {
			return decision
		}
	}
	if s.cfg.TailSweepMaxTradesPerHour > 0 && s.countRecentTailSweepEntriesLocked(time.Now().Add(-1*time.Hour)) >= s.cfg.TailSweepMaxTradesPerHour {
		return decision
	}

	tradeAmount := s.cfg.TradeAmount * s.cfg.TailSweepSizeRatio
	minRequiredAmount := price * minPolymarketBuyShares
	if tradeAmount < minRequiredAmount {
		tradeAmount = minRequiredAmount
	}
	if err := validateMinimumBuyShares(tradeAmount, price); err != nil {
		return decision
	}
	netEdgeBps := estimateNetEdgeBps(
		diffBps,
		s.cfg.TailSweepMinDiffBps,
		price,
		price,
		bestBid,
		bestAsk,
		s.marketFeeRateBps,
		false,
	)
	if netEdgeBps < s.cfg.MinNetEdgeBps {
		return decision
	}
	if s.autoTradeGuard != nil {
		if err := s.autoTradeGuard(autoTradeRiskRequest{
			MarketSlug:       s.activeMarket.Slug,
			MarketKey:        s.cfg.ResolvedMarketKey(),
			StrategyMode:     strategyModeTailSweep,
			Side:             side,
			TradeAmount:      tradeAmount,
			CurrentPrice:     price,
			WindowSec:        s.cfg.TailSweepFinalSec,
			StrategyKey:      strategyKey,
			LossStreakLimit:  s.cfg.TailSweepLossStreakLimit,
			DisableLookback:  s.cfg.TailSweepDisableLookback,
			DisableMinProfit: s.cfg.TailSweepDisableMinProfit,
		}); err != nil {
			return decision
		}
	}

	decision.ok = true
	decision.reasonCode = autoTradeRejectNone
	decision.reason = ""
	decision.plan = autoBuyPlan{
		side:         side,
		tokenID:      tokenID,
		price:        price,
		reason:       fmt.Sprintf("尾盘扫尾: 剩余≤%ds 且 价差>=%.2fbps", s.cfg.TailSweepFinalSec, s.cfg.TailSweepMinDiffBps),
		orderKey:     orderKey,
		retryCount:   retryCount,
		tradeAmount:  tradeAmount,
		diff:         diff,
		windowSec:    s.cfg.TailSweepFinalSec,
		strategyKey:  strategyKey,
		execution:    "tail_sweep_limit",
		postOnly:     false,
		orderType:    "GTC",
		expirationTS: 0,
		netEdgeBps:   netEdgeBps,
	}
	return decision
}

// managePosition 负责已开仓位的止盈与止损逻辑。
func (s *PolymarketService) managePosition(ctx context.Context) {
	s.mu.RLock()
	market := cloneActiveMarket(s.activeMarket)
	position := clonePosition(s.state.Position)
	tpOrder := clonePendingOrder(s.state.TakeProfitOrder)
	upPrice := firstPositive(s.price.upPrice, s.activeMarketPriceLocked("UP"))
	downPrice := firstPositive(s.price.downPrice, s.activeMarketPriceLocked("DOWN"))
	upBid := derefFloat(s.price.upBid)
	downBid := derefFloat(s.price.downBid)
	diff := s.currentDiffLocked()
	s.mu.RUnlock()

	if market == nil || position == nil || position.Slug != market.Slug {
		return
	}
	if s.cfg.TailSweepHoldToSettlement && isTailSweepStrategyKey(position.StrategyKey) {
		return
	}

	currentProb := upPrice
	bestBid := upBid
	tokenID := market.UpToken
	if strings.ToUpper(position.Side) == "DOWN" {
		currentProb = downPrice
		bestBid = downBid
		tokenID = market.DownToken
	}
	if currentProb <= 0 {
		return
	}

	stopProb := position.EntryPrice * (1 - s.cfg.StopLossProbPct)
	riskAbs := position.EntryPrice - stopProb
	tpTrigger := minFloat(s.cfg.TakeProfitCap, position.EntryPrice+riskAbs*s.cfg.TakeProfitRR)
	if tpTrigger > position.EntryPrice {
		balancedRisk := (tpTrigger - position.EntryPrice) / s.cfg.TakeProfitRR
		balancedStop := position.EntryPrice - balancedRisk
		if balancedStop > stopProb {
			stopProb = balancedStop
		}
	}

	if tpOrder == nil && tpTrigger > position.EntryPrice && currentProb >= tpTrigger {
		if !s.cfg.AutoTrade {
			s.addLog("TRADE", fmt.Sprintf("提醒模式: 建议止盈卖出 %s @ %.2f%%", position.Side, tpTrigger*100))
		} else {
			s.addLog(
				"TRADE",
				fmt.Sprintf(
					"止盈触发: 入场%.2f%%, 当前%.2f%%, 目标%.2f%% (RR≈%.2f)",
					position.EntryPrice*100,
					currentProb*100,
					tpTrigger*100,
					s.cfg.TakeProfitRR,
				),
			)
			submitPrice, orderID, normalizedSize, err := s.placeTakeProfitOrderWithRetry(ctx, tokenID, position.Size, tpTrigger)
			if err == nil {
				estimatedProceeds := submitPrice * normalizedSize
				s.mu.Lock()
				s.state.TakeProfitOrder = &entity.PendingOrder{
					OrderID:     orderID,
					Time:        time.Now().Format(time.RFC3339),
					Slug:        market.Slug,
					Side:        position.Side,
					Action:      "SELL",
					Reason:      "take_profit",
					Price:       submitPrice,
					Size:        normalizedSize,
					Amount:      estimatedProceeds,
					WindowSec:   position.WindowSec,
					StrategyKey: position.StrategyKey,
					Execution:   "maker_gtc",
				}
				s.appendHistoryLocked(entity.TradeHistoryItem{
					Time:        time.Now().Format("2006-01-02 15:04:05"),
					Slug:        market.Slug,
					Action:      "SELL",
					Side:        position.Side,
					Price:       submitPrice,
					Amount:      estimatedProceeds,
					Size:        normalizedSize,
					OrderID:     orderID,
					Status:      "submitted",
					Reason:      "take_profit",
					Diff:        floatPtr(diff),
					WindowSec:   position.WindowSec,
					StrategyKey: position.StrategyKey,
					Execution:   "maker_gtc",
				})
				s.syncDashboardLocked()
				s.persistLocked(context.Background())
				s.publishLocked()
				s.mu.Unlock()
				s.addLog("TRADE", fmt.Sprintf("止盈挂单已提交: %s @ %.2f%%", position.Side, submitPrice*100))
			} else {
				s.mu.Lock()
				s.appendHistoryLocked(entity.TradeHistoryItem{
					Time:        time.Now().Format("2006-01-02 15:04:05"),
					Slug:        market.Slug,
					Action:      "SELL",
					Side:        position.Side,
					Price:       tpTrigger,
					Amount:      position.Size * tpTrigger,
					Size:        position.Size,
					OrderID:     "",
					Status:      "failed",
					Reason:      "take_profit",
					Error:       err.Error(),
					Diff:        floatPtr(diff),
					WindowSec:   position.WindowSec,
					StrategyKey: position.StrategyKey,
					Execution:   "maker_gtc",
				})
				s.syncDashboardLocked()
				s.persistLocked(context.Background())
				s.publishLocked()
				s.mu.Unlock()
				s.addLog("ERR", fmt.Sprintf("止盈挂单失败: %v", err))
			}
		}
	}

	if currentProb > 0 && currentProb <= stopProb {
		remaining := remainingSeconds(market.End)
		if s.shouldHoldStopLoss(position.Side, remaining) {
			s.addLog(
				"RISK",
				fmt.Sprintf(
					"最后 %ds 强信号保护生效，暂缓止损: %s / 剩余 %ds",
					s.cfg.StopLossHoldFinalSec,
					position.Side,
					remaining,
				),
			)
			return
		}
		if tpOrder != nil && strings.EqualFold(strings.TrimSpace(tpOrder.Reason), "stop_loss") {
			return
		}
		if tpOrder != nil {
			if err := s.client.CancelOrder(ctx, tpOrder.OrderID); err != nil {
				s.addLog("WARN", fmt.Sprintf("撤销原卖单失败，暂不重复提交止损单: %v", err))
				return
			}
		}

		if !s.cfg.AutoTrade {
			s.addLog("TRADE", fmt.Sprintf("提醒模式: 建议止损卖出 %s", position.Side))
			return
		}

		sellPrice := currentProb
		if bestBid > 0 {
			sellPrice = bestBid
		}
		orderID, normalizedSize, err := s.client.PlaceLimitOrder(ctx, tokenID, "SELL", sellPrice, position.Size)
		if adjustedSize, ok := adjustedSellSizeFromError(err, position.Size); ok {
			s.addLog("WARN", fmt.Sprintf("止损卖出数量从 %.4f 调整到 %.4f 后重试", position.Size, adjustedSize))
			orderID, normalizedSize, err = s.client.PlaceLimitOrder(ctx, tokenID, "SELL", sellPrice, adjustedSize)
		}
		estimatedProceeds := sellPrice * normalizedSize

		s.mu.Lock()
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:        time.Now().Format("2006-01-02 15:04:05"),
			Slug:        market.Slug,
			Action:      "SELL",
			Side:        position.Side,
			Price:       sellPrice,
			Amount:      estimatedProceeds,
			Size:        normalizedSize,
			OrderID:     orderID,
			Status:      statusText(err == nil, "submitted", "failed"),
			Reason:      "stop_loss",
			Error:       errorText(err),
			Diff:        floatPtr(diff),
			WindowSec:   position.WindowSec,
			StrategyKey: position.StrategyKey,
			Execution:   "stop_loss_taker",
		})
		if err == nil {
			s.state.TakeProfitOrder = &entity.PendingOrder{
				OrderID:     orderID,
				Time:        time.Now().Format(time.RFC3339),
				Slug:        market.Slug,
				Side:        position.Side,
				Action:      "SELL",
				Reason:      "stop_loss",
				Price:       sellPrice,
				Size:        normalizedSize,
				Amount:      estimatedProceeds,
				WindowSec:   position.WindowSec,
				StrategyKey: position.StrategyKey,
				Execution:   "stop_loss_taker",
			}
		} else {
			s.state.TakeProfitOrder = nil
		}
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		if err != nil {
			s.addLog("ERR", fmt.Sprintf("止损卖出失败: %v", err))
		} else {
			s.addLog("TRADE", fmt.Sprintf("止损卖出已提交: %s @ %.2f%%", position.Side, sellPrice*100))
		}
	}
}

// shouldHoldStopLoss 判断是否满足“临近结算且强信号仍然成立”的止损豁免条件。
func (s *PolymarketService) shouldHoldStopLoss(side string, remaining int) bool {
	// 未开启、已过结算、或还没进入尾盘窗口时，一律不豁免止损。
	if s.cfg.StopLossHoldFinalSec <= 0 || remaining <= 0 || remaining > s.cfg.StopLossHoldFinalSec {
		return false
	}

	s.mu.RLock()
	referencePrice := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	binancePrice := derefFloat(s.price.binance)
	referenceUpdateTS := s.price.btcUpdateTS
	binanceUpdateTS := s.price.binanceUpdateTS
	s.mu.RUnlock()
	if referencePrice <= 0 || ptb <= 0 {
		return false
	}

	// 尾盘豁免比普通开仓更依赖实时性，参考价过旧时宁可执行止损。
	referenceAge := time.Since(referenceUpdateTS).Seconds()
	if referenceUpdateTS.IsZero() || referenceAge > s.cfg.StopLossHoldMaxLagSec {
		return false
	}

	diff := referencePrice - ptb
	diffBps := math.Abs(calcRelativeDiffBps(referencePrice, ptb))
	if diffBps < s.cfg.StopLossHoldMinDiffBps {
		return false
	}

	normalizedSide := strings.ToUpper(strings.TrimSpace(side))
	switch normalizedSide {
	case "UP":
		if diff <= 0 {
			return false
		}
	case "DOWN":
		if diff >= 0 {
			return false
		}
	default:
		return false
	}

	if !s.cfg.StopLossHoldRequireBinance {
		return true
	}

	if binancePrice <= 0 {
		return false
	}
	if binanceUpdateTS.IsZero() || time.Since(binanceUpdateTS).Seconds() > s.cfg.StopLossHoldMaxLagSec {
		return false
	}

	// 尾盘保护只要求 Binance 方向同向确认；若配置了双源偏差上限，也继续沿用。
	if s.cfg.BinanceVetoMaxDevBps > 0 {
		sourceDeviation := math.Abs(calcRelativeDiffBps(referencePrice, binancePrice))
		if sourceDeviation > s.cfg.BinanceVetoMaxDevBps {
			return false
		}
	}

	binanceDiff := binancePrice - ptb
	binanceDiffBps := math.Abs(calcRelativeDiffBps(binancePrice, ptb))
	switch normalizedSide {
	case "UP":
		if binanceDiff <= 0 {
			return false
		}
	case "DOWN":
		if binanceDiff >= 0 {
			return false
		}
	}
	if s.cfg.BinanceConfirmMinBps > 0 && binanceDiffBps < s.cfg.BinanceConfirmMinBps {
		return false
	}
	return true
}

// currentDiffLocked 返回当前外部参考价与策略目标价的绝对价差。
func (s *PolymarketService) currentDiffLocked() float64 {
	btc := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	if btc <= 0 || ptb <= 0 {
		return 0
	}
	return btc - ptb
}

// currentDisplayDiffLocked 返回当前外部参考价与展示 PTB 的绝对价差。
func (s *PolymarketService) currentDisplayDiffLocked() float64 {
	btc := derefFloat(s.price.btc)
	ptb := derefFloat(firstFloatPtr(s.price.ptbDisplay, s.price.ptb))
	if btc <= 0 || ptb <= 0 {
		return 0
	}
	return btc - ptb
}

// calcRelativeDiffBps 把绝对价差换算成相对 bps，便于同一套配置兼容不同价格量级的市场。
func calcRelativeDiffBps(referencePrice, ptb float64) float64 {
	denominator := math.Max(math.Abs(referencePrice), math.Abs(ptb))
	if denominator <= 0 {
		return 0
	}
	return (referencePrice - ptb) / denominator * 10000
}

// autoTradeThresholdBps 把当前条件的阈值统一换算成 bps，便于和信号强度、交易成本放在同一单位里比较。
func autoTradeThresholdBps(condition infraPolymarket.ConditionConfig, _ float64, _ float64) float64 {
	if condition.DiffBps <= 0 {
		return 0
	}
	return condition.DiffBps
}

// estimateNetEdgeBps 估算一次入场在扣除点差、滑点与 taker 费之后剩余的净边际。
func estimateNetEdgeBps(actualSignalBps, thresholdBps, entryPrice, displayPrice, bestBid, bestAsk float64, feeRateBps int, postOnly bool) float64 {
	signalMargin := math.Max(0, actualSignalBps-thresholdBps)
	costBps := entryPriceSlippage(entryPrice, displayPrice) * 10000
	if !postOnly && bestBid > 0 && bestAsk > 0 && entryPrice > 0 {
		costBps += math.Abs(bestAsk-bestBid) / entryPrice * 10000
		costBps += estimatedTakerFeeBps(entryPrice, feeRateBps)
	}
	return signalMargin - costBps
}

// estimatedTakerFeeBps 依据 Polymarket crypto fee 曲线，估算买入该概率时的 taker 成本。
func estimatedTakerFeeBps(price float64, feeRateBps int) float64 {
	if price <= 0 || price >= 1 || feeRateBps <= 0 {
		return 0
	}
	feeRate := float64(feeRateBps) / 100
	return feeRate * math.Pow(price*(1-price), 2) * 10000
}

// buildStrategyKey 为多市场/多档位运行生成稳定策略键，便于统计收益榜与自动停用。
func buildStrategyKey(marketKey string, slot int, windowSec int, side string) string {
	return fmt.Sprintf(
		"%s|slot-%d|%ds|%s",
		strings.TrimSpace(marketKey),
		slot,
		windowSec,
		strings.ToUpper(strings.TrimSpace(side)),
	)
}

// buildTailSweepStrategyKey 为尾盘扫尾巴策略生成独立的策略键。
func buildTailSweepStrategyKey(marketKey string, windowSec int, side string) string {
	return fmt.Sprintf(
		"%s|tail-sweep|%ds|%s",
		strings.TrimSpace(marketKey),
		windowSec,
		strings.ToUpper(strings.TrimSpace(side)),
	)
}

// isTailSweepStrategyKey 判断某条策略键是否属于尾盘扫尾巴策略。
func isTailSweepStrategyKey(strategyKey string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(strategyKey)), "|tail-sweep|")
}

// entryPriceSlippage 计算候选买价相对展示价/参考价的实际偏离，用于拦截宽价差下的坏成交。
func entryPriceSlippage(candidatePrice, referencePrice float64) float64 {
	if candidatePrice <= 0 || referencePrice <= 0 {
		return 0
	}
	if candidatePrice <= referencePrice {
		return 0
	}
	return (candidatePrice - referencePrice) / referencePrice
}

// matchesAutoTradeDiff 判断当前价差强度是否满足某条自动交易档位。
func matchesAutoTradeDiff(condition infraPolymarket.ConditionConfig, diffBps float64) bool {
	if condition.DiffBps <= 0 {
		return false
	}
	return math.Abs(diffBps) >= condition.DiffBps
}

// resolveFallbackConditionLocked 在 maker 超时回退时，确认当前价格仍然满足原始窗口档位。
func (s *PolymarketService) resolveFallbackConditionLocked(windowSec int, price, diffBps float64) (infraPolymarket.ConditionConfig, bool) {
	for _, condition := range orderedAutoTradeConditions(s.cfg.Conditions) {
		if condition.Time <= 0 || condition.Time != windowSec {
			continue
		}
		if !matchesAutoTradeDiff(condition, diffBps) {
			continue
		}
		if price < condition.MinProb || price > condition.MaxProb {
			continue
		}
		return condition, true
	}
	return infraPolymarket.ConditionConfig{}, false
}

// resolveAutoTradeConditionSide 根据当前差值正负，自动得出本次应买的方向。
func resolveAutoTradeConditionSide(diff float64) string {
	switch {
	case diff > 0:
		return "UP"
	case diff < 0:
		return "DOWN"
	default:
		return ""
	}
}

// validateBinanceEntryLocked 使用 Binance 作为第二数据源做方向确认和异常偏差否决。
func (s *PolymarketService) validateBinanceEntryLocked(side string, referencePrice, binancePrice, ptb float64) (bool, string) {
	return validateBinanceEntry(
		side,
		referencePrice,
		binancePrice,
		ptb,
		s.cfg.BinanceRequireAlign,
		s.cfg.BinanceConfirmMinBps,
		s.cfg.BinanceVetoMaxDevBps,
	)
}

// validateBinanceEntry 使用 Binance 作为第二数据源做方向确认和异常偏差否决。
func validateBinanceEntry(side string, referencePrice, binancePrice, ptb float64, requireAlign bool, confirmBps, maxDevBps float64) (bool, string) {
	needsBinance := requireAlign || confirmBps > 0 || maxDevBps > 0
	if !needsBinance {
		return true, ""
	}
	if binancePrice <= 0 {
		return false, "Binance 参考价未就绪，已开启双源确认"
	}
	if maxDevBps > 0 {
		sourceDeviation := math.Abs(calcRelativeDiffBps(referencePrice, binancePrice))
		if sourceDeviation > maxDevBps {
			return false, fmt.Sprintf(
				"Chainlink 与 Binance 偏差 %.2fbps，超过 %.2fbps",
				sourceDeviation,
				maxDevBps,
			)
		}
	}

	binanceDiff := binancePrice - ptb
	binanceDiffBps := math.Abs(calcRelativeDiffBps(binancePrice, ptb))
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "UP":
		if binanceDiff <= 0 || binanceDiffBps < confirmBps {
			return false, fmt.Sprintf(
				"Binance 未确认 UP 方向: diff=%.4f / %.2fbps，低于 %.2fbps",
				binanceDiff,
				binanceDiffBps,
				confirmBps,
			)
		}
	case "DOWN":
		if binanceDiff >= 0 || binanceDiffBps < confirmBps {
			return false, fmt.Sprintf(
				"Binance 未确认 DOWN 方向: diff=%.4f / %.2fbps，低于 %.2fbps",
				binanceDiff,
				binanceDiffBps,
				confirmBps,
			)
		}
	}
	return true, ""
}

// confirmAutoTradeSignalLocked 要求同一市场同一方向的信号持续一段时间后才能真正放行。
func (s *PolymarketService) confirmAutoTradeSignalLocked(orderKey string) (bool, string) {
	required := time.Duration(s.cfg.AutoTradeConfirmSec * float64(time.Second))
	if required <= 0 {
		return true, ""
	}
	now := time.Now()
	if s.signalConfirmKey != orderKey || s.signalConfirmSince.IsZero() {
		s.signalConfirmKey = orderKey
		s.signalConfirmSince = now
		return false, fmt.Sprintf("信号确认中 0.00s / %.2fs", s.cfg.AutoTradeConfirmSec)
	}
	elapsed := now.Sub(s.signalConfirmSince)
	if elapsed >= required {
		return true, ""
	}
	return false, fmt.Sprintf("信号确认中 %.2fs / %.2fs", elapsed.Seconds(), s.cfg.AutoTradeConfirmSec)
}

// resetSignalConfirmationLocked 在信号失效、市场切换或真正下单后清空确认状态。
func (s *PolymarketService) resetSignalConfirmationLocked() {
	s.signalConfirmKey = ""
	s.signalConfirmSince = time.Time{}
}

// formatConditionDiffThreshold 把条件阈值渲染成日志友好的文字，方便判断当前规则使用的是哪种模式。
func formatConditionDiffThreshold(condition infraPolymarket.ConditionConfig) string {
	return fmt.Sprintf("%.2f bps", condition.DiffBps)
}

// isPostOnlyCrossBookError 判断 post-only 挂单是否因为价格已穿过盘口而被交易所拒绝。
func isPostOnlyCrossBookError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid post-only order") && strings.Contains(message, "crosses book")
}

// validateMinimumBuyShares 校验买入金额在当前概率下是否至少能达到 Polymarket 的最小买入份额。
func validateMinimumBuyShares(amount, price float64) error {
	if amount <= 0 || price <= 0 {
		return nil
	}
	size := amount / price
	if size+1e-9 >= minPolymarketBuyShares {
		return nil
	}
	requiredAmount := minPolymarketBuyShares * price
	return fmt.Errorf(
		"当前下单金额 %.2f USDC 在概率 %.2f%% 下仅能买 %.2f 份，低于最小 %.0f 份；请至少提高到 %.2f USDC",
		amount,
		price*100,
		size,
		minPolymarketBuyShares,
		requiredAmount,
	)
}

// countRecentTailSweepEntriesLocked 统计最近窗口期内已经提交过多少次尾盘扫尾买单。
func (s *PolymarketService) countRecentTailSweepEntriesLocked(since time.Time) int {
	count := 0
	for _, item := range s.state.TradeHistory {
		if !strings.EqualFold(strings.TrimSpace(item.Action), "BUY") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Status), "submitted") {
			continue
		}
		if !isTailSweepStrategyKey(item.StrategyKey) {
			continue
		}
		parsed, err := time.Parse("2006-01-02 15:04:05", item.Time)
		if err == nil && parsed.Before(since) {
			continue
		}
		count++
	}
	return count
}

// orderedAutoTradeConditions 按时间窗口从近到远排序条件，避免编号顺序影响实际匹配优先级。
func orderedAutoTradeConditions(in []infraPolymarket.ConditionConfig) []infraPolymarket.ConditionConfig {
	out := append([]infraPolymarket.ConditionConfig(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		leftDisabled := out[i].Time <= 0
		rightDisabled := out[j].Time <= 0
		if leftDisabled != rightDisabled {
			return !leftDisabled
		}
		if out[i].Time != out[j].Time {
			return out[i].Time < out[j].Time
		}
		return conditionSlot(out[i]) < conditionSlot(out[j])
	})
	return out
}

// conditionSlot 返回条件的稳定档位编号；缺失时回退到 0。
func conditionSlot(condition infraPolymarket.ConditionConfig) int {
	if condition.Slot > 0 {
		return condition.Slot
	}
	return 0
}

// activeMarketPriceLocked 返回当前市场缓存中的指定方向概率。
func (s *PolymarketService) activeMarketPriceLocked(side string) *float64 {
	if s.activeMarket == nil {
		return nil
	}
	if side == "UP" {
		return s.activeMarket.UpPrice
	}
	return s.activeMarket.DownPrice
}

// syncDashboardLocked 把当前内存状态投影成前端需要的 dashboard 快照。
func (s *PolymarketService) syncDashboardLocked() {
	now := time.Now().Format(time.RFC3339)
	s.dashboard.UpdatedAt = now

	if s.activeMarket == nil {
		s.dashboard.Market = entity.DashboardMarket{
			Status: "waiting",
		}
	} else {
		remaining := remainingSeconds(s.activeMarket.End)
		if remaining < 0 {
			remaining = 0
		}
		s.dashboard.Market = entity.DashboardMarket{
			Slug:          s.activeMarket.Slug,
			Remaining:     remaining,
			RemainingText: fmt.Sprintf("%d分%d秒", remaining/60, remaining%60),
			Start:         s.activeMarket.Start,
			End:           s.activeMarket.End,
			Status:        "active",
		}
	}

	displayDiff := s.currentDisplayDiffLocked()
	displayDiffAbs := absFloat(displayDiff)
	var diffPtr *float64
	var diffAbsPtr *float64
	if displayDiff != 0 {
		diffPtr = floatPtr(displayDiff)
		diffAbsPtr = floatPtr(displayDiffAbs)
	}
	strategyTarget := cloneFloatPtr(s.price.ptb)
	s.dashboard.Prices = entity.DashboardPrices{
		PTB:          cloneFloatPtr(firstFloatPtr(s.price.ptbDisplay, s.price.ptb)),
		TargetPrice:  strategyTarget,
		ChainlinkBTC: cloneFloatPtr(s.price.btc),
		BinanceBTC:   cloneFloatPtr(s.price.binance),
		UpPrice:      cloneFloatPtr(firstFloatPtr(s.price.upPrice, s.activeMarketPriceLocked("UP"))),
		DownPrice:    cloneFloatPtr(firstFloatPtr(s.price.downPrice, s.activeMarketPriceLocked("DOWN"))),
		UpBid:        cloneFloatPtr(s.price.upBid),
		UpAsk:        cloneFloatPtr(s.price.upAsk),
		DownBid:      cloneFloatPtr(s.price.downBid),
		DownAsk:      cloneFloatPtr(s.price.downAsk),
		Diff:         diffPtr,
		DiffAbs:      diffAbsPtr,
		UpdatedTS:    time.Now().Unix(),
	}
	s.dashboard.AutoTrade = cloneAutoTradeDiagnostics(s.autoTrade)
	s.dashboard.AutoTradeConfig = buildAutoTradeConfigView(s.cfg)
	s.dashboard.Position = clonePosition(s.state.Position)
	s.dashboard.PendingOrder = s.dashboardPendingOrderLocked()
	s.dashboard.LastOrder = cloneLastOrder(s.state.LastOrder)
	s.dashboard.TradeHistory = cloneHistory(s.state.TradeHistory)
	s.dashboard.RoundResults = buildRoundResults(
		s.dashboard.LiveTrades,
		s.activeMarket,
		s.state.Position,
		s.state.PendingOrder,
		s.currentDiffLocked(),
	)
}

// dashboardPendingOrderLocked 返回 dashboard 当前应展示的挂单。
func (s *PolymarketService) dashboardPendingOrderLocked() *entity.PendingOrder {
	if s.state.PendingOrder != nil {
		return clonePendingOrder(s.state.PendingOrder)
	}
	return clonePendingOrder(s.state.TakeProfitOrder)
}

// buildHistoryPayloadLocked 按 Python 版优先级组装历史区数据。
func (s *PolymarketService) buildHistoryPayloadLocked() any {
	// 优先展示按市场聚合后的实时交易摘要，便于 dashboard 直接复用聚合视图。
	if liveItems := trimLiveTrades(cloneLiveTrades(s.dashboard.LiveTrades), 300); len(liveItems) > 0 {
		return liveItems
	}

	// 当实时聚合为空时，退回本地交易历史与钱包历史的拼接结果。
	merged := append(cloneHistory(s.state.TradeHistory), cloneHistory(s.dashboard.WalletHistory)...)
	if len(merged) == 0 {
		return []entity.TradeHistoryItem{}
	}
	return trimTradeHistory(merged, 300)
}

// resetAutoTradeDiagnosticsLocked 在市场切换时重置自动交易诊断计数。
func (s *PolymarketService) resetAutoTradeDiagnosticsLocked(marketSlug string) {
	s.autoTrade = entity.AutoTradeDiagnostics{
		MarketSlug: strings.TrimSpace(marketSlug),
	}
}

// recordAutoTradeDecisionLocked 记录一次自动交易评估结果，供 TUI 与 dashboard 诊断展示。
func (s *PolymarketService) recordAutoTradeDecisionLocked(decision autoTradeDecision) {
	s.autoTrade.SampleCount++
	now := time.Now().Format(time.RFC3339)
	if decision.ok {
		s.autoTrade.TriggerCount++
		s.autoTrade.LastReason = ""
		s.autoTrade.LastReasonAt = ""
		s.autoTrade.LastTriggerAt = now
		return
	}

	s.autoTrade.LastReason = strings.TrimSpace(decision.reason)
	s.autoTrade.LastReasonAt = now
	switch decision.reasonCode {
	case autoTradeRejectNoMarket:
		s.autoTrade.NoMarketCount++
	case autoTradeRejectMarketClosed:
		s.autoTrade.MarketClosedCount++
	case autoTradeRejectTimeWindowMiss:
		s.autoTrade.TimeWindowMissCount++
	case autoTradeRejectReferenceMiss:
		s.autoTrade.ReferenceMissingCount++
	case autoTradeRejectOutcomePriceMiss:
		s.autoTrade.OutcomePriceMissing++
	case autoTradeRejectDiffMiss:
		s.autoTrade.DiffMissCount++
	case autoTradeRejectProbabilityMiss:
		s.autoTrade.ProbabilityMissCount++
	case autoTradeRejectDataLag:
		s.autoTrade.DataLagCount++
	case autoTradeRejectBlockedByState:
		s.autoTrade.BlockedByStateCount++
	case autoTradeRejectRetryLimit:
		s.autoTrade.RetryLimitCount++
	case autoTradeRejectSignalConfirm:
		s.autoTrade.SignalConfirmCount++
	case autoTradeRejectBinanceVeto:
		s.autoTrade.BinanceVetoCount++
	case autoTradeRejectNetEdgeMiss:
		s.autoTrade.NetEdgeMissCount++
	}
}

// persistLocked 把当前交易状态持久化到本地仓储。
func (s *PolymarketService) persistLocked(ctx context.Context) {
	stateCopy := s.state
	stateCopy.PTB = cloneFloatPtr(firstFloatPtr(s.price.ptbDisplay, s.price.ptb))
	stateCopy.PTBTarget = cloneFloatPtr(s.price.ptb)
	stateCopy.Chainlink = cloneFloatPtr(s.price.btc)
	stateCopy.Binance = cloneFloatPtr(s.price.binance)
	stateCopy.UpPrice = cloneFloatPtr(firstFloatPtr(s.price.upPrice, s.activeMarketPriceLocked("UP")))
	stateCopy.DownPrice = cloneFloatPtr(firstFloatPtr(s.price.downPrice, s.activeMarketPriceLocked("DOWN")))
	stateCopy.LastUpdate = time.Now().Format(time.RFC3339)
	_ = s.repo.Save(ctx, &stateCopy)
}

// appendHistoryLocked 追加一条交易历史，并限制历史长度上限。
func (s *PolymarketService) appendHistoryLocked(item entity.TradeHistoryItem) {
	s.state.TradeHistory = append(s.state.TradeHistory, item)
	if len(s.state.TradeHistory) > 400 {
		s.state.TradeHistory = append([]entity.TradeHistoryItem(nil), s.state.TradeHistory[len(s.state.TradeHistory)-400:]...)
	}
}

// addLog 同时写结构化日志和 dashboard 活动面板。
func (s *PolymarketService) addLog(level, message string) {
	ts := time.Now().Format("15:04:05")
	switch level {
	case "ERR":
		s.logger.Error("polymarket", slog.String("message", message))
	case "WARN":
		s.logger.Warn("polymarket", slog.String("message", message))
	default:
		s.logger.Info("polymarket", slog.String("level", level), slog.String("message", message))
	}

	s.mu.Lock()
	s.dashboard.Activity = append(s.dashboard.Activity, entity.ActivityLog{
		Time:    ts,
		Level:   level,
		Message: message,
	})
	if len(s.dashboard.Activity) > 400 {
		s.dashboard.Activity = append([]entity.ActivityLog(nil), s.dashboard.Activity[len(s.dashboard.Activity)-400:]...)
	}
	s.dashboard.UpdatedAt = time.Now().Format(time.RFC3339)
	s.publishLocked()
	s.mu.Unlock()
}

// publish 以线程安全方式向所有订阅者广播最新快照。
func (s *PolymarketService) publish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishLocked()
}

// publishLocked 向订阅者推送快照，并清理阻塞或失效的通道。
func (s *PolymarketService) publishLocked() {
	snapshot := cloneDashboard(s.dashboard)
	for id, ch := range s.subscribers {
		select {
		case ch <- snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snapshot:
			default:
				close(ch)
				delete(s.subscribers, id)
			}
		}
	}
}

// cloneDashboard 对 dashboard 快照做深拷贝，避免不同协程共享底层切片。
func cloneDashboard(in entity.DashboardState) entity.DashboardState {
	out := in
	out.Position = clonePosition(in.Position)
	out.PendingOrder = clonePendingOrder(in.PendingOrder)
	out.LastOrder = cloneLastOrder(in.LastOrder)
	out.TradeHistory = cloneHistory(in.TradeHistory)
	out.WalletHistory = cloneHistory(in.WalletHistory)
	out.Activity = cloneActivity(in.Activity)
	out.RoundResults = cloneRoundResults(in.RoundResults)
	out.WalletPositions = cloneWalletPositions(in.WalletPositions)
	out.LiveTrades = cloneLiveTrades(in.LiveTrades)
	out.AutoTrade = cloneAutoTradeDiagnostics(in.AutoTrade)
	out.AutoTradeConfig = cloneAutoTradeConfigView(in.AutoTradeConfig)
	out.AutoRedeem = cloneAutoRedeemStatus(in.AutoRedeem)
	out.GlobalRisk = cloneGlobalRiskStatus(in.GlobalRisk)
	out.Markets = cloneTrackedMarkets(in.Markets)
	out.StrategyPerformance = cloneStrategyPerformance(in.StrategyPerformance)
	out.Prices.PTB = cloneFloatPtr(in.Prices.PTB)
	out.Prices.TargetPrice = cloneFloatPtr(in.Prices.TargetPrice)
	out.Prices.ChainlinkBTC = cloneFloatPtr(in.Prices.ChainlinkBTC)
	out.Prices.BinanceBTC = cloneFloatPtr(in.Prices.BinanceBTC)
	out.Prices.UpPrice = cloneFloatPtr(in.Prices.UpPrice)
	out.Prices.DownPrice = cloneFloatPtr(in.Prices.DownPrice)
	out.Prices.UpBid = cloneFloatPtr(in.Prices.UpBid)
	out.Prices.UpAsk = cloneFloatPtr(in.Prices.UpAsk)
	out.Prices.DownBid = cloneFloatPtr(in.Prices.DownBid)
	out.Prices.DownAsk = cloneFloatPtr(in.Prices.DownAsk)
	out.Prices.Diff = cloneFloatPtr(in.Prices.Diff)
	out.Prices.DiffAbs = cloneFloatPtr(in.Prices.DiffAbs)
	out.WalletBalance = cloneFloatPtr(in.WalletBalance)
	return out
}

// cloneTrackedMarkets 复制 watchlist 市场摘要切片。
func cloneTrackedMarkets(in []entity.TrackedMarketView) []entity.TrackedMarketView {
	out := make([]entity.TrackedMarketView, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.Position = clonePosition(item.Position)
		cloned.PendingOrder = clonePendingOrder(item.PendingOrder)
		cloned.LastOrder = cloneLastOrder(item.LastOrder)
		cloned.AutoTrade = cloneAutoTradeDiagnostics(item.AutoTrade)
		cloned.AutoConfig = cloneAutoTradeConfigView(item.AutoConfig)
		cloned.Prices = clonePrices(item.Prices)
		out = append(out, cloned)
	}
	return out
}

// cloneAutoTradeDiagnostics 复制自动交易诊断结构。
func cloneAutoTradeDiagnostics(in entity.AutoTradeDiagnostics) entity.AutoTradeDiagnostics {
	return in
}

// cloneAutoTradeConfigView 复制自动交易配置视图。
func cloneAutoTradeConfigView(in entity.AutoTradeConfigView) entity.AutoTradeConfigView {
	out := in
	if len(in.Conditions) > 0 {
		out.Conditions = append([]entity.AutoTradeConditionView(nil), in.Conditions...)
	}
	return out
}

// buildAutoTradeConfigView 把当前 worker 的配置转换成前端更容易观察的只读摘要。
func buildAutoTradeConfigView(cfg infraPolymarket.Config) entity.AutoTradeConfigView {
	out := entity.AutoTradeConfigView{
		MainStrategyEnabled:        cfg.MainStrategyEnabled,
		TradeAmount:                cfg.TradeAmount,
		ConfirmSec:                 cfg.AutoTradeConfirmSec,
		MarketDataMaxLagSec:        cfg.MarketDataMaxLagSec,
		MinNetEdgeBps:              cfg.MinNetEdgeBps,
		PreferPostOnly:             cfg.PreferPostOnly,
		StopLossProbPct:            cfg.StopLossProbPct,
		StopLossHoldFinalSec:       cfg.StopLossHoldFinalSec,
		StopLossHoldMinDiffBps:     cfg.StopLossHoldMinDiffBps,
		StopLossHoldRequireBinance: cfg.StopLossHoldRequireBinance,
		StopLossHoldMaxLagSec:      cfg.StopLossHoldMaxLagSec,
		TakeProfitRR:               cfg.TakeProfitRR,
		BinanceRequireAlign:        cfg.BinanceRequireAlign,
		BinanceConfirmMinBps:       cfg.BinanceConfirmMinBps,
		BinanceVetoMaxDevBps:       cfg.BinanceVetoMaxDevBps,
		TailSweepEnabled:           cfg.TailSweepEnabled,
		TailSweepAllowed:           cfg.TailSweepAllowed,
		TailSweepFinalSec:          cfg.TailSweepFinalSec,
		TailSweepMinDiffBps:        cfg.TailSweepMinDiffBps,
		TailSweepMinProb:           cfg.TailSweepMinProb,
		TailSweepMaxProb:           cfg.TailSweepMaxProb,
		TailSweepMaxPrice:          cfg.TailSweepMaxPrice,
		TailSweepRequireBinance:    cfg.TailSweepRequireBinance,
		TailSweepMaxLagSec:         cfg.TailSweepMaxLagSec,
		TailSweepMaxSpread:         cfg.TailSweepMaxSpread,
		TailSweepSizeRatio:         cfg.TailSweepSizeRatio,
		TailSweepMaxTradesPerHour:  cfg.TailSweepMaxTradesPerHour,
		TailSweepLossStreakLimit:   cfg.TailSweepLossStreakLimit,
		TailSweepHoldToSettlement:  cfg.TailSweepHoldToSettlement,
		Conditions:                 make([]entity.AutoTradeConditionView, 0, len(cfg.Conditions)),
	}
	for _, condition := range orderedAutoTradeConditions(cfg.Conditions) {
		out.Conditions = append(out.Conditions, entity.AutoTradeConditionView{
			Index:   conditionSlot(condition),
			Enabled: condition.Time > 0,
			Time:    condition.Time,
			DiffBps: condition.DiffBps,
			MinProb: condition.MinProb,
			MaxProb: condition.MaxProb,
		})
	}
	return out
}

// cloneAutoRedeemStatus 复制自动兑奖状态，并深拷贝最近结果 map。
func cloneAutoRedeemStatus(in entity.AutoRedeemStatus) entity.AutoRedeemStatus {
	out := in
	if in.LastResult != nil {
		out.LastResult = make(map[string]any, len(in.LastResult))
		for key, value := range in.LastResult {
			out.LastResult[key] = value
		}
	}
	return out
}

// cloneGlobalRiskStatus 复制全局风控状态。
func cloneGlobalRiskStatus(in entity.GlobalRiskStatus) entity.GlobalRiskStatus {
	return in
}

// cloneHistory 复制交易历史切片。
func cloneHistory(in []entity.TradeHistoryItem) []entity.TradeHistoryItem {
	out := make([]entity.TradeHistoryItem, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.Diff = cloneFloatPtr(item.Diff)
		cloned.PnL = cloneFloatPtr(item.PnL)
		cloned.NetEdgeBps = cloneFloatPtr(item.NetEdgeBps)
		out = append(out, cloned)
	}
	return out
}

// cloneStrategyPerformance 复制策略收益榜切片。
func cloneStrategyPerformance(in []entity.StrategyPerformance) []entity.StrategyPerformance {
	return append([]entity.StrategyPerformance(nil), in...)
}

// cloneActivity 复制活动日志切片。
func cloneActivity(in []entity.ActivityLog) []entity.ActivityLog {
	return append([]entity.ActivityLog(nil), in...)
}

// cloneWalletPositions 复制钱包持仓切片。
func cloneWalletPositions(in []entity.WalletPosition) []entity.WalletPosition {
	out := make([]entity.WalletPosition, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.AvgPrice = cloneFloatPtr(item.AvgPrice)
		cloned.CurPrice = cloneFloatPtr(item.CurPrice)
		cloned.RealizedPnL = cloneFloatPtr(item.RealizedPnL)
		out = append(out, cloned)
	}
	return out
}

// cloneLiveTrades 复制聚合后的实时交易摘要。
func cloneLiveTrades(in []entity.LiveTradeSummary) []entity.LiveTradeSummary {
	out := make([]entity.LiveTradeSummary, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.EntryPriceQuote = cloneFloatPtr(item.EntryPriceQuote)
		cloned.ExitPriceQuote = cloneFloatPtr(item.ExitPriceQuote)
		out = append(out, cloned)
	}
	return out
}

// cloneRoundResults 复制轮次结果切片，并深拷贝内部的浮点指针。
func cloneRoundResults(in []entity.RoundResult) []entity.RoundResult {
	out := make([]entity.RoundResult, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.FinalDiff = cloneFloatPtr(item.FinalDiff)
		cloned.Profit = cloneFloatPtr(item.Profit)
		out = append(out, cloned)
	}
	return out
}

// clonePosition 复制持仓对象。
func clonePosition(in *entity.Position) *entity.Position {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// clonePendingOrder 复制挂单对象。
func clonePendingOrder(in *entity.PendingOrder) *entity.PendingOrder {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// cloneLastOrder 复制最近一次下单记录。
func cloneLastOrder(in *entity.LastOrder) *entity.LastOrder {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// cloneActiveMarket 复制当前活跃市场结构。
func cloneActiveMarket(in *entity.ActiveMarket) *entity.ActiveMarket {
	if in == nil {
		return nil
	}
	out := *in
	out.UpPrice = cloneFloatPtr(in.UpPrice)
	out.DownPrice = cloneFloatPtr(in.DownPrice)
	return &out
}

// cloneFloatPtr 复制一个 float64 指针值。
func cloneFloatPtr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// firstPositive 返回第一个大于 0 的价格值。
func firstPositive(values ...*float64) float64 {
	for _, value := range values {
		if value != nil && *value > 0 {
			return *value
		}
	}
	return 0
}

// firstFloatPtr 返回第一个非 nil 的浮点指针。
func firstFloatPtr(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// quickOrderPriceLocked 为终端快捷下单挑选一个尽量贴近可成交的价格。
func (s *PolymarketService) quickOrderPriceLocked(action, outcome string) float64 {
	switch {
	case strings.EqualFold(action, "BUY") && strings.EqualFold(outcome, "UP"):
		return firstPositive(s.price.upAsk, s.price.upPrice, s.activeMarketPriceLocked("UP"))
	case strings.EqualFold(action, "BUY") && strings.EqualFold(outcome, "DOWN"):
		return firstPositive(s.price.downAsk, s.price.downPrice, s.activeMarketPriceLocked("DOWN"))
	case strings.EqualFold(action, "SELL") && strings.EqualFold(outcome, "UP"):
		return firstPositive(s.price.upBid, s.price.upPrice, s.activeMarketPriceLocked("UP"))
	case strings.EqualFold(action, "SELL") && strings.EqualFold(outcome, "DOWN"):
		return firstPositive(s.price.downBid, s.price.downPrice, s.activeMarketPriceLocked("DOWN"))
	default:
		return 0
	}
}

// derefFloat 在 nil 安全前提下解引用浮点指针。
func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// floatPtr 把普通浮点值转成指针。
func floatPtr(v float64) *float64 {
	return &v
}

// errorText 把 error 安全转换成可落盘、可展示的字符串。
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// clampProbability 把概率限制在 CLOB 手动下单能接受的安全区间内。
func clampProbability(v float64) float64 {
	switch {
	case v < 0.01:
		return 0.01
	case v > 0.99:
		return 0.99
	default:
		return v
	}
}

// absFloat 返回浮点数绝对值。
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// minFloat 返回两个浮点数中的较小值。
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// buildRoundResults 把账户聚合结果与当前活跃市场上下文统一投影成轮次结果。
func buildRoundResults(
	liveTrades []entity.LiveTradeSummary,
	market *entity.ActiveMarket,
	position *entity.Position,
	pendingOrder *entity.PendingOrder,
	diff float64,
) []entity.RoundResult {
	results := buildRoundResultsFromLiveTrades(liveTrades)
	if market == nil {
		return results
	}

	activeStatus := "settling"
	if remainingSeconds(market.End) > 0 {
		activeStatus = "active"
	}
	activeSide := resolveActiveRoundSide(position, pendingOrder)
	diffPtr := (*float64)(nil)
	if diff != 0 {
		diffPtr = floatPtr(diff)
	}

	foundCurrentRound := false
	for idx := range results {
		if !strings.EqualFold(results[idx].Slug, market.Slug) || results[idx].Status == "resolved" {
			continue
		}
		foundCurrentRound = true
		results[idx].Status = activeStatus
		if strings.TrimSpace(results[idx].Start) == "" {
			results[idx].Start = market.Start
		}
		if strings.TrimSpace(results[idx].End) == "" {
			results[idx].End = market.End
		}
		if strings.TrimSpace(results[idx].Time) == "" {
			results[idx].Time = firstNonEmpty(market.End, market.Start)
		}
		if strings.TrimSpace(results[idx].EntrySide) == "" && activeSide != "" {
			results[idx].EntrySide = activeSide
		}
		if diffPtr != nil {
			results[idx].FinalDiff = cloneFloatPtr(diffPtr)
		}
	}

	// 本地已经有活跃仓位或待成交买单时，即使 Data API 尚未同步，也先展示一个进行中的轮次。
	if !foundCurrentRound && activeSide != "" {
		activeRound := entity.RoundResult{
			Kind:      "MARKET_ROUND",
			Slug:      market.Slug,
			Start:     market.Start,
			End:       market.End,
			Time:      firstNonEmpty(market.End, market.Start),
			Status:    activeStatus,
			EntrySide: activeSide,
			FinalDiff: cloneFloatPtr(diffPtr),
		}
		results = append(results, activeRound)
	}

	sort.Slice(results, func(i, j int) bool {
		return roundResultMillis(results[i]) < roundResultMillis(results[j])
	})
	return results
}

// buildRoundResultsFromLiveTrades 把聚合交易摘要转换成前端直接消费的轮次结果。
func buildRoundResultsFromLiveTrades(rows []entity.LiveTradeSummary) []entity.RoundResult {
	out := make([]entity.RoundResult, 0, len(rows))
	for _, row := range rows {
		status := deriveRoundStatus(row)
		entrySide := normalizeOutcomeLabel(row.Direction)
		if entrySide == "-" {
			entrySide = ""
		}

		item := entity.RoundResult{
			Kind:      "MARKET_ROUND",
			Slug:      row.Slug,
			Start:     row.OrderTime,
			End:       row.SettleTime,
			Time:      firstNonEmpty(row.SettleTime, row.OrderTime),
			Status:    status,
			EntrySide: entrySide,
		}

		// 已结算轮次才写入盈亏和最终方向，避免把未结算头寸误判成亏损。
		if status == "resolved" {
			item.Profit = floatPtr(row.Profit)
			item.FinalOutcome = deriveRoundOutcome(row)
		}
		out = append(out, item)
	}
	return out
}

// deriveRoundStatus 根据聚合结果判断当前轮次处于进行中、待结算还是已结算。
func deriveRoundStatus(row entity.LiveTradeSummary) string {
	if strings.EqualFold(strings.TrimSpace(row.Result), "OPEN") {
		return "settling"
	}
	if row.SellCount > 0 || row.RedeemCount > 0 || strings.EqualFold(strings.TrimSpace(row.Result), "CLOSED") {
		return "resolved"
	}
	return "settling"
}

// deriveRoundOutcome 推导轮次最终方向；只有兑奖成功时才视为结果可确认。
func deriveRoundOutcome(row entity.LiveTradeSummary) string {
	if row.RedeemCount <= 0 {
		return ""
	}
	outcome := normalizeOutcomeLabel(row.Direction)
	if outcome == "-" {
		return ""
	}
	return outcome
}

// resolveActiveRoundSide 优先从本地持仓中推导当前活跃轮次的入场方向。
func resolveActiveRoundSide(position *entity.Position, pendingOrder *entity.PendingOrder) string {
	if position != nil {
		return strings.ToUpper(strings.TrimSpace(position.Side))
	}
	if pendingOrder != nil && strings.EqualFold(strings.TrimSpace(pendingOrder.Action), "BUY") {
		return strings.ToUpper(strings.TrimSpace(pendingOrder.Side))
	}
	return ""
}

// roundResultMillis 返回轮次结果用于排序的毫秒时间戳。
func roundResultMillis(row entity.RoundResult) int64 {
	return toMillis(firstNonEmpty(row.End, row.Time, row.Start))
}

// trimTradeHistory 只保留最新 limit 条普通历史记录。
func trimTradeHistory(items []entity.TradeHistoryItem, limit int) []entity.TradeHistoryItem {
	if len(items) <= limit {
		return items
	}
	return append([]entity.TradeHistoryItem(nil), items[len(items)-limit:]...)
}

// trimLiveTrades 只保留最新 limit 条聚合历史记录。
func trimLiveTrades(items []entity.LiveTradeSummary, limit int) []entity.LiveTradeSummary {
	if len(items) <= limit {
		return items
	}
	return append([]entity.LiveTradeSummary(nil), items[len(items)-limit:]...)
}

// placeTakeProfitOrderWithRetry 按配置的最小步长逐步抬价，直到止盈挂单成功或达到上限。
func (s *PolymarketService) placeTakeProfitOrderWithRetry(
	ctx context.Context,
	tokenID string,
	size float64,
	triggerPrice float64,
) (float64, string, float64, error) {
	maxRetry := s.cfg.TakeProfitRetryMax
	if maxRetry <= 0 {
		maxRetry = 1
	}

	attemptPrice := triggerPrice
	var lastErr error
	currentSize := size
	for attempt := 1; attempt <= maxRetry; attempt++ {
		orderID, normalizedSize, err := s.client.PlaceLimitOrder(ctx, tokenID, "SELL", attemptPrice, currentSize)
		if err == nil {
			return attemptPrice, orderID, normalizedSize, nil
		}
		if adjustedSize, ok := adjustedSellSizeFromError(err, currentSize); ok {
			s.addLog("WARN", fmt.Sprintf("检测到可卖仓位不足，止盈数量从 %.4f 调整到 %.4f 后重试", currentSize, adjustedSize))
			currentSize = adjustedSize
			orderID, normalizedSize, retryErr := s.client.PlaceLimitOrder(ctx, tokenID, "SELL", attemptPrice, currentSize)
			if retryErr == nil {
				return attemptPrice, orderID, normalizedSize, nil
			}
			err = retryErr
		}
		lastErr = err
		if attempt >= maxRetry {
			break
		}

		nextPrice := minFloat(s.cfg.TakeProfitCap, attemptPrice+s.cfg.TakeProfitRetryStep)
		if nextPrice <= attemptPrice+1e-9 {
			break
		}
		s.addLog("WARN", fmt.Sprintf("止盈挂单重试 %d/%d: 提高到 %.2f%%", attempt+1, maxRetry, nextPrice*100))
		attemptPrice = nextPrice
	}
	if lastErr == nil {
		lastErr = errors.New("止盈挂单失败")
	}
	return attemptPrice, "", 0, lastErr
}

// adjustedSellSizeFromError 尝试从余额不足错误中解析出当前可卖份额，并转换成 share 数量。
func adjustedSellSizeFromError(err error, requestedSize float64) (float64, bool) {
	if err == nil {
		return 0, false
	}
	matches := insufficientBalanceAllowancePattern.FindStringSubmatch(err.Error())
	if len(matches) != 3 {
		return 0, false
	}

	balanceUnits, parseErr := strconv.ParseFloat(matches[1], 64)
	if parseErr != nil || balanceUnits <= 0 {
		return 0, false
	}

	availableSize := balanceUnits / 1_000_000
	if availableSize <= 0 || availableSize >= requestedSize {
		return 0, false
	}

	// 给交易所舍入留一点余量，避免边界值再次因精度问题被拒绝。
	adjusted := availableSize - 0.000001
	if adjusted <= 0 {
		return 0, false
	}
	return adjusted, true
}

// remainingSeconds 计算距离市场结束还剩多少秒。
func remainingSeconds(endAt string) int {
	t, ok := parseRFC3339Loose(endAt)
	if !ok {
		return 0
	}
	return int(time.Until(t).Seconds())
}

// parseRFC3339Loose 兼容 `Z` 和显式偏移两种 RFC3339 时间格式。
func parseRFC3339Loose(raw string) (time.Time, bool) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.Replace(raw, "Z", "+00:00", 1))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// nextPTBPrewarmLeadSec 返回提前预热下一轮 PTB 的时间窗口。
// 这里固定在“切盘前最多 120 秒”内开始预热，避免 5m/15m 新轮次刚开始时长时间空白。
func nextPTBPrewarmLeadSec(intervalSec int) int {
	if intervalSec <= 0 {
		return 120
	}
	if intervalSec < 120 {
		return intervalSec
	}
	return 120
}

// statusText 根据成功与否返回两个候选状态文本之一。
func statusText(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}
