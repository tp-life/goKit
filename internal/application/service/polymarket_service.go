package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	price struct {
		btc       *float64
		binance   *float64
		ptb       *float64
		upPrice   *float64
		downPrice *float64
		upBid     *float64
		upAsk     *float64
		downBid   *float64
		downAsk   *float64

		btcUpdateTS     time.Time
		binanceUpdateTS time.Time
		upUpdateTS      time.Time
		downUpdateTS    time.Time
	}

	subscribers map[int]chan entity.DashboardState
	nextSubID   int

	cancelRoot   context.CancelFunc
	cancelMarket context.CancelFunc
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

// RegisterPolymarketLifecycle 把 Polymarket 机器人挂到 Fx 生命周期中。
func RegisterPolymarketLifecycle(lc fx.Lifecycle, svc *PolymarketService) {
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
	go s.runBalancePoller(rootCtx)
	go s.runAccountSnapshotPoller(rootCtx)
	go s.runAutoRedeemer(rootCtx)
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

		s.refreshPTBIfNeeded(ctx)
		s.processPendingBuy(ctx)
		s.processTakeProfitOrder(ctx)
		s.evaluateAutoTrade(ctx)
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
		}
		s.mu.Unlock()
		return
	}
	if s.cancelMarket != nil {
		s.cancelMarket()
		s.cancelMarket = nil
	}
	s.activeMarket = cloneActiveMarket(market)

	// 市场切换后，上一轮价格与仓位上下文都必须清空，避免跨市场污染。
	s.price.ptb = nil
	s.price.upPrice = nil
	s.price.downPrice = nil
	s.price.upBid = nil
	s.price.upAsk = nil
	s.price.downBid = nil
	s.price.downAsk = nil
	s.price.upUpdateTS = time.Time{}
	s.price.downUpdateTS = time.Time{}
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

// refreshPTBIfNeeded 在当前轮首次拿到市场元数据后，补齐 PTB 价格。
func (s *PolymarketService) refreshPTBIfNeeded(ctx context.Context) {
	s.mu.RLock()
	market := cloneActiveMarket(s.activeMarket)
	hasPTB := s.price.ptb != nil
	s.mu.RUnlock()
	if market == nil || hasPTB {
		return
	}

	openPrice, closePrice, err := s.feeds.GetCryptoPrice(ctx, market.Start, market.End)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.price.ptb != nil {
		return
	}
	if openPrice != nil {
		s.price.ptb = cloneFloatPtr(openPrice)
	} else if closePrice != nil {
		s.price.ptb = cloneFloatPtr(closePrice)
	}
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
		s.state.Position = &entity.Position{
			Slug:       pending.Slug,
			Side:       pending.Side,
			EntryPrice: pending.Price,
			EntryDiff:  absFloat(diff),
			Size:       status.SizeMatched,
			Amount:     pending.Amount,
		}
		s.state.PendingOrder = nil
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:    time.Now().Format("2006-01-02 15:04:05"),
			Slug:    pending.Slug,
			Action:  "BUY",
			Side:    pending.Side,
			Price:   pending.Price,
			Amount:  pending.Amount,
			Size:    status.SizeMatched,
			OrderID: pending.OrderID,
			Status:  "filled",
			Reason:  "pending_filled",
			Diff:    floatPtr(diff),
		})
		logLevel = "TRADE"
		logMessage = fmt.Sprintf("买单已成交: %s @ %.2f%%", pending.Side, pending.Price*100)
	} else {
		_ = s.client.CancelOrder(ctx, pending.OrderID)
		s.state.PendingOrder = nil
		logLevel = "WARN"
		logMessage = fmt.Sprintf("买单超时未成交，已撤单: %s", pending.OrderID)
	}
	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()
	if logMessage != "" {
		s.addLog(logLevel, logMessage)
	}
}

// processTakeProfitOrder 检查止盈挂单状态，并在成交后清理仓位。
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
	switch {
	case status.Filled:
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:    time.Now().Format("2006-01-02 15:04:05"),
			Slug:    tpOrder.Slug,
			Action:  "SELL",
			Side:    tpOrder.Side,
			Price:   tpOrder.Price,
			Amount:  tpOrder.Amount,
			Size:    status.SizeMatched,
			OrderID: tpOrder.OrderID,
			Status:  "filled",
			Reason:  tpOrder.Reason,
		})
		s.state.TakeProfitOrder = nil
		s.state.Position = nil
		logLevel = "TRADE"
		logMessage = fmt.Sprintf("卖单已成交: %s @ %.2f%%", tpOrder.Side, tpOrder.Price*100)
	case normalizedStatus == "CANCELED" || normalizedStatus == "CANCELLED" || normalizedStatus == "REJECTED" || normalizedStatus == "EXPIRED":
		s.state.TakeProfitOrder = nil
		logLevel = "WARN"
		logMessage = "卖单已失效，等待再次触发"
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
	s.mu.RLock()
	market := cloneActiveMarket(s.activeMarket)
	plan, ok := s.buildAutoBuyPlanLocked()
	s.mu.RUnlock()
	if !ok || market == nil {
		return
	}

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

	orderID, normalizedSize, err := s.client.PlaceLimitOrder(ctx, plan.tokenID, "BUY", plan.price, plan.tradeAmount/plan.price)
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
			Time:    time.Now().Format("2006-01-02 15:04:05"),
			Slug:    market.Slug,
			Action:  "BUY",
			Side:    plan.side,
			Price:   plan.price,
			Amount:  plan.tradeAmount,
			OrderID: "",
			Status:  "failed",
			Reason:  plan.reason,
			Error:   err.Error(),
			Diff:    floatPtr(plan.diff),
		})
		s.syncDashboardLocked()
		s.persistLocked(context.Background())
		s.publishLocked()
		s.mu.Unlock()
		s.addLog("ERR", fmt.Sprintf("自动下单失败: %v", err))
		return
	}

	s.state.PendingOrder = &entity.PendingOrder{
		OrderID: orderID,
		Time:    time.Now().Format(time.RFC3339),
		Slug:    market.Slug,
		Side:    plan.side,
		Action:  "BUY",
		Reason:  plan.reason,
		Price:   plan.price,
		Size:    normalizedSize,
		Amount:  plan.tradeAmount,
	}
	s.state.LastOrder = &entity.LastOrder{
		Key:        plan.orderKey,
		Time:       time.Now().Format(time.RFC3339),
		RetryCount: plan.retryCount + 1,
		LastPrice:  plan.price,
		Error:      "",
	}
	s.appendHistoryLocked(entity.TradeHistoryItem{
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Slug:    market.Slug,
		Action:  "BUY",
		Side:    plan.side,
		Price:   plan.price,
		Amount:  plan.tradeAmount,
		Size:    normalizedSize,
		OrderID: orderID,
		Status:  "submitted",
		Reason:  plan.reason,
		Diff:    floatPtr(plan.diff),
	})
	s.syncDashboardLocked()
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()
	s.addLog("TRADE", fmt.Sprintf("自动买单已提交: %s @ %.2f%%", plan.side, plan.price*100))
}

type autoBuyPlan struct {
	side        string
	tokenID     string
	price       float64
	reason      string
	orderKey    string
	retryCount  int
	tradeAmount float64
	diff        float64
}

// buildAutoBuyPlanLocked 在持锁状态下构造一笔可执行的自动买入计划。
func (s *PolymarketService) buildAutoBuyPlanLocked() (autoBuyPlan, bool) {
	var plan autoBuyPlan
	if s.activeMarket == nil {
		return plan, false
	}
	remaining := remainingSeconds(s.activeMarket.End)
	if remaining <= 0 {
		return plan, false
	}
	btc := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	if btc <= 0 || ptb <= 0 {
		return plan, false
	}
	diff := btc - ptb

	// 优先使用盘口 ask，其次退回中间价和市场快照价，尽量贴近真实可成交概率。
	upPrice := firstPositive(s.price.upAsk, s.price.upPrice, s.activeMarket.UpPrice)
	downPrice := firstPositive(s.price.downAsk, s.price.downPrice, s.activeMarket.DownPrice)
	if upPrice <= 0 || downPrice <= 0 {
		return plan, false
	}

	for _, condition := range s.cfg.Conditions {
		if remaining > condition.Time {
			continue
		}
		var price float64
		switch condition.Side {
		case "UP":
			if diff < condition.Diff {
				continue
			}
			price = upPrice
		case "DOWN":
			if diff > -condition.Diff {
				continue
			}
			price = downPrice
		default:
			continue
		}
		if price < condition.MinProb || price > condition.MaxProb {
			continue
		}

		var lastUpdate time.Time
		if condition.Side == "UP" {
			lastUpdate = s.price.upUpdateTS
		} else {
			lastUpdate = s.price.downUpdateTS
		}
		if lastUpdate.IsZero() {
			lastUpdate = time.Now()
		}
		btcUpdate := s.price.btcUpdateTS
		if btcUpdate.IsZero() {
			btcUpdate = time.Now()
		}
		sideAge := time.Since(lastUpdate).Seconds()
		btcAge := time.Since(btcUpdate).Seconds()
		if sideAge > s.cfg.MarketDataMaxLagSec || btcAge > s.cfg.MarketDataMaxLagSec {
			return plan, false
		}

		if s.state.PendingOrder != nil || s.state.Position != nil {
			return plan, false
		}
		orderKey := s.activeMarket.Slug + "|" + condition.Side
		lastOrder := s.state.LastOrder
		retryCount := 0
		if lastOrder != nil && lastOrder.Key == orderKey {
			retryCount = lastOrder.RetryCount
			if retryCount >= s.cfg.MaxRetryPerMarket {
				return plan, false
			}
			if lastOrder.LastPrice > 0 {
				// 同方向重试时只允许按最小步长追价，避免短时间内无约束抬价。
				capPrice := minFloat(0.995, lastOrder.LastPrice+s.cfg.BuyRetryStep)
				if price > capPrice {
					price = capPrice
				}
			}
		}
		currentPrice := price
		slippage := 0.0
		if currentPrice > 0 {
			slippage = absFloat(currentPrice-price) / price
		}
		if slippage > s.cfg.SlippageThreshold {
			return plan, false
		}

		tokenID := s.activeMarket.UpToken
		if condition.Side == "DOWN" {
			tokenID = s.activeMarket.DownToken
		}
		plan = autoBuyPlan{
			side:        condition.Side,
			tokenID:     tokenID,
			price:       price,
			reason:      fmt.Sprintf("剩余≤%ds 且价差满足阈值", condition.Time),
			orderKey:    orderKey,
			retryCount:  retryCount,
			tradeAmount: s.cfg.TradeAmount,
			diff:        diff,
		}
		return plan, true
	}
	return plan, false
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
				s.mu.Lock()
				s.state.TakeProfitOrder = &entity.PendingOrder{
					OrderID: orderID,
					Time:    time.Now().Format(time.RFC3339),
					Slug:    market.Slug,
					Side:    position.Side,
					Action:  "SELL",
					Reason:  "take_profit",
					Price:   submitPrice,
					Size:    normalizedSize,
					Amount:  position.Amount,
				}
				s.appendHistoryLocked(entity.TradeHistoryItem{
					Time:    time.Now().Format("2006-01-02 15:04:05"),
					Slug:    market.Slug,
					Action:  "SELL",
					Side:    position.Side,
					Price:   submitPrice,
					Amount:  position.Amount,
					Size:    normalizedSize,
					OrderID: orderID,
					Status:  "submitted",
					Reason:  "take_profit",
					Diff:    floatPtr(diff),
				})
				s.syncDashboardLocked()
				s.persistLocked(context.Background())
				s.publishLocked()
				s.mu.Unlock()
				s.addLog("TRADE", fmt.Sprintf("止盈挂单已提交: %s @ %.2f%%", position.Side, submitPrice*100))
			} else {
				s.mu.Lock()
				s.appendHistoryLocked(entity.TradeHistoryItem{
					Time:    time.Now().Format("2006-01-02 15:04:05"),
					Slug:    market.Slug,
					Action:  "SELL",
					Side:    position.Side,
					Price:   tpTrigger,
					Amount:  position.Amount,
					Size:    position.Size,
					OrderID: "",
					Status:  "failed",
					Reason:  "take_profit",
					Error:   err.Error(),
					Diff:    floatPtr(diff),
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
		if tpOrder != nil {
			_ = s.client.CancelOrder(ctx, tpOrder.OrderID)
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

		s.mu.Lock()
		s.appendHistoryLocked(entity.TradeHistoryItem{
			Time:    time.Now().Format("2006-01-02 15:04:05"),
			Slug:    market.Slug,
			Action:  "SELL",
			Side:    position.Side,
			Price:   sellPrice,
			Amount:  position.Amount,
			Size:    normalizedSize,
			OrderID: orderID,
			Status:  statusText(err == nil, "submitted", "failed"),
			Reason:  "stop_loss",
			Error:   errorText(err),
			Diff:    floatPtr(diff),
		})
		s.state.Position = nil
		s.state.TakeProfitOrder = nil
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

// currentDiffLocked 返回当前 BTC 与 PTB 的价差。
func (s *PolymarketService) currentDiffLocked() float64 {
	btc := derefFloat(s.price.btc)
	ptb := derefFloat(s.price.ptb)
	if btc <= 0 || ptb <= 0 {
		return 0
	}
	return btc - ptb
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

	diff := s.currentDiffLocked()
	diffAbs := absFloat(diff)
	var diffPtr *float64
	var diffAbsPtr *float64
	if diff != 0 {
		diffPtr = floatPtr(diff)
		diffAbsPtr = floatPtr(diffAbs)
	}
	s.dashboard.Prices = entity.DashboardPrices{
		PTB:          cloneFloatPtr(s.price.ptb),
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
	s.dashboard.Position = clonePosition(s.state.Position)
	s.dashboard.PendingOrder = s.dashboardPendingOrderLocked()
	s.dashboard.LastOrder = cloneLastOrder(s.state.LastOrder)
	s.dashboard.TradeHistory = cloneHistory(s.state.TradeHistory)
	s.dashboard.RoundResults = buildRoundResults(
		s.dashboard.LiveTrades,
		s.activeMarket,
		s.state.Position,
		s.state.PendingOrder,
		diff,
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

// persistLocked 把当前交易状态持久化到本地仓储。
func (s *PolymarketService) persistLocked(ctx context.Context) {
	stateCopy := s.state
	stateCopy.PTB = cloneFloatPtr(s.price.ptb)
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
	out.Prices.PTB = cloneFloatPtr(in.Prices.PTB)
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

// cloneHistory 复制交易历史切片。
func cloneHistory(in []entity.TradeHistoryItem) []entity.TradeHistoryItem {
	out := make([]entity.TradeHistoryItem, 0, len(in))
	for _, item := range in {
		cloned := item
		cloned.Diff = cloneFloatPtr(item.Diff)
		cloned.PnL = cloneFloatPtr(item.PnL)
		out = append(out, cloned)
	}
	return out
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
	if strings.TrimSpace(endAt) == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, strings.Replace(endAt, "Z", "+00:00", 1))
	if err != nil {
		return 0
	}
	return int(time.Until(t).Seconds())
}

// statusText 根据成功与否返回两个候选状态文本之一。
func statusText(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}
