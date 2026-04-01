package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"goKit/internal/application/dto"
	"goKit/internal/domain/entity"
	infraMarketData "goKit/internal/infrastructure/marketdata"
	"goKit/internal/infrastructure/persistence"
	infraPolymarket "goKit/internal/infrastructure/polymarket"

	"go.uber.org/fx"
)

type managedWorker struct {
	target   infraPolymarket.MarketTargetConfig
	service  *PolymarketService
	subID    int
	snapshot entity.DashboardState
}

// PolymarketManager 负责编排多个单市场 worker，并向上层暴露聚合后的统一视图。
type PolymarketManager struct {
	cfg    infraPolymarket.Config
	logger *slog.Logger

	mu        sync.RWMutex
	workers   map[string]*managedWorker
	order     []string
	focusKey  string
	snapshot  entity.DashboardState
	nextSubID int
	subs      map[int]chan entity.DashboardState
	cancel    context.CancelFunc
}

// NewPolymarketManager 创建多市场管理器。
func NewPolymarketManager(cfg infraPolymarket.Config, logger *slog.Logger) *PolymarketManager {
	manager := &PolymarketManager{
		cfg:      cfg,
		logger:   logger,
		workers:  map[string]*managedWorker{},
		snapshot: entity.NewDashboardState(),
		subs:     map[int]chan entity.DashboardState{},
	}

	targets := cfg.ResolvedMarketTargets()
	manager.order = make([]string, 0, len(targets))
	for idx, target := range targets {
		enableSharedTasks := idx == 0
		childCfg := cfg.CloneForTarget(target, idx, enableSharedTasks)
		repo := persistence.NewPolymarketStateRepository(childCfg)
		client, err := infraPolymarket.NewClient(childCfg, logger)
		if err != nil {
			if logger != nil {
				logger.Error("polymarket_manager_init_client", slog.String("market", target.Label), slog.Any("err", err))
			}
			continue
		}
		feeds, err := infraMarketData.NewClient(childCfg, logger)
		if err != nil {
			if logger != nil {
				logger.Error("polymarket_manager_init_feeds", slog.String("market", target.Label), slog.Any("err", err))
			}
			continue
		}

		worker := NewPolymarketService(childCfg, repo, client, feeds, logger)
		manager.workers[target.Key] = &managedWorker{
			target:   target,
			service:  worker,
			snapshot: entity.NewDashboardState(),
		}
		manager.order = append(manager.order, target.Key)
	}
	if len(manager.order) > 0 {
		manager.focusKey = manager.order[0]
	}
	manager.rebuildSnapshotLocked()
	return manager
}

// RegisterPolymarketLifecycle 把多市场管理器接到 Fx 生命周期中。
func RegisterPolymarketLifecycle(lc fx.Lifecycle, svc *PolymarketManager) {
	lc.Append(fx.Hook{
		OnStart: svc.Start,
		OnStop:  svc.Stop,
	})
}

// Start 启动所有已配置的单市场 worker，并订阅它们的快照。
func (m *PolymarketManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return nil
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()

	started := make([]*managedWorker, 0, len(m.order))
	for _, key := range m.order {
		m.mu.RLock()
		worker := m.workers[key]
		m.mu.RUnlock()
		if worker == nil {
			continue
		}

		if err := worker.service.Start(ctx); err != nil {
			for _, startedWorker := range started {
				_ = startedWorker.service.Stop(ctx)
			}
			cancel()
			m.mu.Lock()
			m.cancel = nil
			m.mu.Unlock()
			return err
		}
		subID, ch := worker.service.Subscribe()
		worker.subID = subID
		worker.snapshot = worker.service.Snapshot()
		started = append(started, worker)

		go m.consumeWorkerSnapshots(rootCtx, key, ch)
	}

	m.mu.Lock()
	m.rebuildSnapshotLocked()
	m.publishLocked()
	m.mu.Unlock()
	return nil
}

// Stop 停止所有 worker，并关闭管理器自己的订阅通道。
func (m *PolymarketManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	workers := make([]*managedWorker, 0, len(m.order))
	for _, key := range m.order {
		if worker := m.workers[key]; worker != nil {
			workers = append(workers, worker)
		}
	}
	for id, ch := range m.subs {
		close(ch)
		delete(m.subs, id)
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, worker := range workers {
		if worker.subID != 0 {
			worker.service.Unsubscribe(worker.subID)
		}
		if err := worker.service.Stop(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return nil
}

// Snapshot 返回聚合后的 dashboard 快照。
func (m *PolymarketManager) Snapshot() entity.DashboardState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneDashboard(m.snapshot)
}

// Logs 返回聚合后的活动日志。
func (m *PolymarketManager) Logs() []entity.ActivityLog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneActivity(m.snapshot.Activity)
}

// History 返回聚合后的历史视图，优先复用实时聚合交易。
func (m *PolymarketManager) History() any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if liveItems := trimLiveTrades(cloneLiveTrades(m.snapshot.LiveTrades), 300); len(liveItems) > 0 {
		return liveItems
	}
	merged := append(cloneHistory(m.snapshot.TradeHistory), cloneHistory(m.snapshot.WalletHistory)...)
	if len(merged) == 0 {
		return []entity.TradeHistoryItem{}
	}
	return trimTradeHistory(merged, 300)
}

// Subscribe 订阅聚合快照。
func (m *PolymarketManager) Subscribe() (int, <-chan entity.DashboardState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextSubID
	m.nextSubID++
	ch := make(chan entity.DashboardState, 1)
	ch <- cloneDashboard(m.snapshot)
	m.subs[id] = ch
	return id, ch
}

// Unsubscribe 注销既有的聚合订阅。
func (m *PolymarketManager) Unsubscribe(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.subs[id]; ok {
		close(ch)
		delete(m.subs, id)
	}
}

// DefaultTradeAmount 返回当前统一使用的默认下单金额。
func (m *PolymarketManager) DefaultTradeAmount() float64 {
	return m.cfg.TradeAmount
}

// SubmitTUIQuickOrder 把终端快捷单路由到当前焦点市场。
func (m *PolymarketManager) SubmitTUIQuickOrder(ctx context.Context, action, outcome string) (*dto.ManualOrderResp, error) {
	worker := m.focusedWorker()
	if worker == nil {
		return nil, errors.New("当前没有可操作的市场")
	}
	return worker.service.SubmitTUIQuickOrder(ctx, action, outcome)
}

// CancelActiveOrder 撤销当前焦点市场最值得关注的挂单。
func (m *PolymarketManager) CancelActiveOrder(ctx context.Context) error {
	worker := m.focusedWorker()
	if worker == nil {
		return errors.New("当前没有可操作的市场")
	}
	return worker.service.CancelActiveOrder(ctx)
}

// SubmitManualOrder 把手动单路由到指定或当前焦点市场。
func (m *PolymarketManager) SubmitManualOrder(ctx context.Context, req dto.ManualOrderReq) (*dto.ManualOrderResp, error) {
	worker := m.workerForRequest(req.MarketKey)
	if worker == nil {
		return nil, errors.New("当前没有可操作的市场")
	}
	return worker.service.SubmitManualOrder(ctx, req)
}

// SelectNextMarket 切换到 watchlist 中的下一个市场。
func (m *PolymarketManager) SelectNextMarket() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return ""
	}
	index := m.focusIndexLocked()
	index = (index + 1) % len(m.order)
	m.focusKey = m.order[index]
	m.rebuildSnapshotLocked()
	m.publishLocked()
	return m.focusKey
}

// SelectPrevMarket 切换到 watchlist 中的上一个市场。
func (m *PolymarketManager) SelectPrevMarket() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return ""
	}
	index := m.focusIndexLocked() - 1
	if index < 0 {
		index = len(m.order) - 1
	}
	m.focusKey = m.order[index]
	m.rebuildSnapshotLocked()
	m.publishLocked()
	return m.focusKey
}

// CurrentMarketKey 返回当前焦点市场键名。
func (m *PolymarketManager) CurrentMarketKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.focusKey
}

// consumeWorkerSnapshots 持续接收单市场 worker 的快照并刷新聚合视图。
func (m *PolymarketManager) consumeWorkerSnapshots(ctx context.Context, key string, ch <-chan entity.DashboardState) {
	for {
		select {
		case <-ctx.Done():
			return
		case snapshot, ok := <-ch:
			if !ok {
				return
			}
			m.mu.Lock()
			worker := m.workers[key]
			if worker != nil {
				worker.snapshot = cloneDashboard(snapshot)
			}
			m.rebuildSnapshotLocked()
			m.publishLocked()
			m.mu.Unlock()
		}
	}
}

// focusedWorker 返回当前焦点市场对应的 worker。
func (m *PolymarketManager) focusedWorker() *managedWorker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workers[m.focusKey]
}

// workerForRequest 根据请求里的 market key 或当前焦点选出目标 worker。
func (m *PolymarketManager) workerForRequest(requestedKey string) *managedWorker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.TrimSpace(requestedKey)
	if key == "" {
		key = m.focusKey
	}
	return m.workers[key]
}

// focusIndexLocked 返回当前焦点在 watchlist 中的序号。
func (m *PolymarketManager) focusIndexLocked() int {
	for idx, key := range m.order {
		if key == m.focusKey {
			return idx
		}
	}
	return 0
}

// rebuildSnapshotLocked 把所有子 worker 快照聚合成一个统一视图。
func (m *PolymarketManager) rebuildSnapshotLocked() {
	merged := entity.NewDashboardState()
	if len(m.order) == 0 {
		m.snapshot = merged
		return
	}

	if _, ok := m.workers[m.focusKey]; !ok {
		m.focusKey = m.order[0]
	}

	if focused := m.workers[m.focusKey]; focused != nil {
		merged = cloneDashboard(focused.snapshot)
	}
	merged.SelectedMarketKey = m.focusKey
	merged.Markets = m.buildTrackedMarketsLocked()
	merged.TradeHistory = m.mergeTradeHistoryLocked()
	merged.Activity = m.mergeActivityLocked()
	merged.RoundResults = m.mergeRoundResultsLocked()

	if primary := m.workers[m.order[0]]; primary != nil {
		primarySnapshot := primary.snapshot
		merged.WalletBalance = cloneFloatPtr(primarySnapshot.WalletBalance)
		merged.WalletPositions = cloneWalletPositions(primarySnapshot.WalletPositions)
		merged.WalletHistory = cloneHistory(primarySnapshot.WalletHistory)
		merged.LiveTrades = cloneLiveTrades(primarySnapshot.LiveTrades)
		merged.LivePositionsCount = primarySnapshot.LivePositionsCount
		merged.LiveRealizedPnL = primarySnapshot.LiveRealizedPnL
		merged.LiveUnrealizedPnL = primarySnapshot.LiveUnrealizedPnL
		merged.LiveTotalPnL = primarySnapshot.LiveTotalPnL
		merged.AutoRedeem = cloneAutoRedeemStatus(primarySnapshot.AutoRedeem)
	}

	if strings.TrimSpace(merged.UpdatedAt) == "" {
		merged.UpdatedAt = mergedTimestamp(merged.Markets)
	}
	m.snapshot = merged
}

// buildTrackedMarketsLocked 输出所有 watchlist 市场的摘要切片。
func (m *PolymarketManager) buildTrackedMarketsLocked() []entity.TrackedMarketView {
	out := make([]entity.TrackedMarketView, 0, len(m.order))
	for _, key := range m.order {
		worker := m.workers[key]
		if worker == nil {
			continue
		}
		snapshot := worker.snapshot
		out = append(out, entity.TrackedMarketView{
			Key:          worker.target.Key,
			Label:        worker.target.Label,
			Symbol:       worker.target.Symbol,
			IntervalSec:  worker.target.IntervalSec,
			UpdatedAt:    snapshot.UpdatedAt,
			Market:       snapshot.Market,
			Prices:       clonePrices(snapshot.Prices),
			Position:     clonePosition(snapshot.Position),
			PendingOrder: clonePendingOrder(snapshot.PendingOrder),
			LastOrder:    cloneLastOrder(snapshot.LastOrder),
			AutoTrade:    cloneAutoTradeDiagnostics(snapshot.AutoTrade),
		})
	}
	return out
}

// mergeTradeHistoryLocked 合并所有 worker 的本地交易历史。
func (m *PolymarketManager) mergeTradeHistoryLocked() []entity.TradeHistoryItem {
	items := make([]entity.TradeHistoryItem, 0, 64)
	for _, key := range m.order {
		worker := m.workers[key]
		if worker == nil {
			continue
		}
		items = append(items, cloneHistory(worker.snapshot.TradeHistory)...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Time < items[j].Time
	})
	return trimTradeHistory(items, 300)
}

// mergeActivityLocked 合并所有 worker 的活动日志，并为消息加上市场标签。
func (m *PolymarketManager) mergeActivityLocked() []entity.ActivityLog {
	items := make([]entity.ActivityLog, 0, 64)
	for _, key := range m.order {
		worker := m.workers[key]
		if worker == nil {
			continue
		}
		for _, item := range worker.snapshot.Activity {
			cloned := item
			cloned.Message = fmt.Sprintf("[%s] %s", worker.target.Label, item.Message)
			items = append(items, cloned)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Time < items[j].Time
	})
	if len(items) > 400 {
		items = items[len(items)-400:]
	}
	return items
}

// mergeRoundResultsLocked 合并所有 worker 的轮次结果。
func (m *PolymarketManager) mergeRoundResultsLocked() []entity.RoundResult {
	items := make([]entity.RoundResult, 0, 32)
	for _, key := range m.order {
		worker := m.workers[key]
		if worker == nil {
			continue
		}
		items = append(items, cloneRoundResults(worker.snapshot.RoundResults)...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := firstPresentString(items[i].Time, items[i].End, items[i].Start)
		right := firstPresentString(items[j].Time, items[j].End, items[j].Start)
		return left < right
	})
	return items
}

// publishLocked 向管理器自己的订阅者广播最新聚合快照。
func (m *PolymarketManager) publishLocked() {
	snapshot := cloneDashboard(m.snapshot)
	for id, ch := range m.subs {
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
				delete(m.subs, id)
			}
		}
	}
}

// clonePrices 深拷贝一组价格视图。
func clonePrices(in entity.DashboardPrices) entity.DashboardPrices {
	out := in
	out.PTB = cloneFloatPtr(in.PTB)
	out.ChainlinkBTC = cloneFloatPtr(in.ChainlinkBTC)
	out.BinanceBTC = cloneFloatPtr(in.BinanceBTC)
	out.UpPrice = cloneFloatPtr(in.UpPrice)
	out.DownPrice = cloneFloatPtr(in.DownPrice)
	out.UpBid = cloneFloatPtr(in.UpBid)
	out.UpAsk = cloneFloatPtr(in.UpAsk)
	out.DownBid = cloneFloatPtr(in.DownBid)
	out.DownAsk = cloneFloatPtr(in.DownAsk)
	out.Diff = cloneFloatPtr(in.Diff)
	out.DiffAbs = cloneFloatPtr(in.DiffAbs)
	return out
}

// mergedTimestamp 取 watchlist 中最近一次更新的时间，作为聚合快照时间戳。
func mergedTimestamp(markets []entity.TrackedMarketView) string {
	latest := ""
	for _, item := range markets {
		if item.UpdatedAt > latest {
			latest = item.UpdatedAt
		}
	}
	return latest
}

// firstPresentString 返回第一个非空字符串。
func firstPresentString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
