package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/polymarket"
)

type stubPolymarketSDK struct {
	placeLimitOrderFn       func(ctx context.Context, tokenID string, action string, price float64, sizeShares float64) (string, float64, error)
	cancelOrderFn           func(ctx context.Context, orderID string) error
	getOrderStatusFn        func(ctx context.Context, orderID string) (*polymarket.OrderStatus, error)
	getActiveMarketFn       func(ctx context.Context) (*polymarket.ActiveMarketView, error)
	getOrderBookFn          func(ctx context.Context, tokenID string) (*polymarket.OrderBookSummary, error)
	getOrderBookHashFn      func(orderbook *polymarket.OrderBookSummary) (string, error)
	validateOrderBookHashFn func(orderbook *polymarket.OrderBookSummary) (bool, string, error)
	subscribeMarketFn       func(ctx context.Context, upToken, downToken string, onUpdate func(assetID string, bid, ask, mid float64)) error
	getERC20BalanceFn       func(ctx context.Context, account string) (*float64, error)
	getWalletPositionsFn    func(ctx context.Context, user string) ([]polymarket.DataPositionResponse, error)
	getWalletClosedFn       func(ctx context.Context, user string) ([]polymarket.DataClosedPositionResponse, error)
	getTradeActivityFn      func(ctx context.Context, user string, limit int) ([]polymarket.DataActivityResponse, error)
	getRedeemableFn         func(ctx context.Context, user string) ([]string, int, error)
	redeemConditionFn       func(ctx context.Context, conditionID string) (*polymarket.RedeemResult, error)
	addressHex              string
	funderHex               string
	hasPrivateKey           bool
	hasPrivateKeySet        bool
}

type noopStateRepo struct{}

// Load 为测试仓储返回空状态。
func (noopStateRepo) Load(_ context.Context) (*entity.PolymarketState, error) {
	return &entity.PolymarketState{}, nil
}

// Save 为测试仓储提供空实现。
func (noopStateRepo) Save(_ context.Context, _ *entity.PolymarketState) error {
	return nil
}

var _ repository.PolymarketStateRepository = noopStateRepo{}

// CreateOrDeriveAPIKey 为测试桩提供空实现。
func (s *stubPolymarketSDK) CreateOrDeriveAPIKey(_ context.Context, _ int) (*polymarket.APIKeyCreds, error) {
	return nil, nil
}

// PlaceLimitOrder 为测试桩提供可注入的下单行为。
func (s *stubPolymarketSDK) PlaceLimitOrder(ctx context.Context, tokenID string, action string, price float64, sizeShares float64) (string, float64, error) {
	if s.placeLimitOrderFn != nil {
		return s.placeLimitOrderFn(ctx, tokenID, action, price, sizeShares)
	}
	return "", 0, errors.New("not implemented")
}

// CancelOrder 为测试桩提供可注入的撤单行为。
func (s *stubPolymarketSDK) CancelOrder(ctx context.Context, orderID string) error {
	if s.cancelOrderFn != nil {
		return s.cancelOrderFn(ctx, orderID)
	}
	return nil
}

// GetOrderStatus 为测试桩提供可注入的订单查询行为。
func (s *stubPolymarketSDK) GetOrderStatus(ctx context.Context, orderID string) (*polymarket.OrderStatus, error) {
	if s.getOrderStatusFn != nil {
		return s.getOrderStatusFn(ctx, orderID)
	}
	return nil, nil
}

// GetTickSize 返回测试中足够使用的固定 tick 大小。
func (s *stubPolymarketSDK) GetTickSize(_ context.Context, _ string) (string, error) {
	return "0.001", nil
}

// GetNegRisk 返回测试中足够使用的固定 neg-risk 标记。
func (s *stubPolymarketSDK) GetNegRisk(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// GetFeeRateBps 返回测试中足够使用的固定费率。
func (s *stubPolymarketSDK) GetFeeRateBps(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// GetActiveMarket 为测试桩提供可注入的市场发现行为。
func (s *stubPolymarketSDK) GetActiveMarket(ctx context.Context) (*polymarket.ActiveMarketView, error) {
	if s.getActiveMarketFn != nil {
		return s.getActiveMarketFn(ctx)
	}
	return nil, nil
}

// GetOrderBook 为测试桩提供可注入的订单簿查询行为。
func (s *stubPolymarketSDK) GetOrderBook(ctx context.Context, tokenID string) (*polymarket.OrderBookSummary, error) {
	if s.getOrderBookFn != nil {
		return s.getOrderBookFn(ctx, tokenID)
	}
	return nil, nil
}

// GetOrderBookHash 为测试桩提供可注入的订单簿哈希行为。
func (s *stubPolymarketSDK) GetOrderBookHash(orderbook *polymarket.OrderBookSummary) (string, error) {
	if s.getOrderBookHashFn != nil {
		return s.getOrderBookHashFn(orderbook)
	}
	return "", nil
}

// ValidateOrderBookHash 为测试桩提供可注入的订单簿校验行为。
func (s *stubPolymarketSDK) ValidateOrderBookHash(orderbook *polymarket.OrderBookSummary) (bool, string, error) {
	if s.validateOrderBookHashFn != nil {
		return s.validateOrderBookHashFn(orderbook)
	}
	return true, "", nil
}

// SubscribeMarket 为测试桩提供可注入的盘口订阅行为。
func (s *stubPolymarketSDK) SubscribeMarket(ctx context.Context, upToken, downToken string, onUpdate func(assetID string, bid, ask, mid float64)) error {
	if s.subscribeMarketFn != nil {
		return s.subscribeMarketFn(ctx, upToken, downToken, onUpdate)
	}
	return nil
}

// AddressHex 返回空地址，供只读测试使用。
func (s *stubPolymarketSDK) AddressHex() string {
	return s.addressHex
}

// FunderHex 返回空地址，供只读测试使用。
func (s *stubPolymarketSDK) FunderHex() string {
	return s.funderHex
}

// HasPrivateKey 在测试中返回 true，避免被监控模式短路。
func (s *stubPolymarketSDK) HasPrivateKey() bool {
	if s.hasPrivateKeySet {
		return s.hasPrivateKey
	}
	return true
}

// GetERC20Balance 为测试桩提供可注入的余额查询行为。
func (s *stubPolymarketSDK) GetERC20Balance(ctx context.Context, account string) (*float64, error) {
	if s.getERC20BalanceFn != nil {
		return s.getERC20BalanceFn(ctx, account)
	}
	return nil, nil
}

// GetWalletPositions 为测试桩提供可注入的持仓查询行为。
func (s *stubPolymarketSDK) GetWalletPositions(ctx context.Context, user string) ([]polymarket.DataPositionResponse, error) {
	if s.getWalletPositionsFn != nil {
		return s.getWalletPositionsFn(ctx, user)
	}
	return nil, nil
}

// GetWalletClosedPositions 为测试桩提供可注入的平仓查询行为。
func (s *stubPolymarketSDK) GetWalletClosedPositions(ctx context.Context, user string) ([]polymarket.DataClosedPositionResponse, error) {
	if s.getWalletClosedFn != nil {
		return s.getWalletClosedFn(ctx, user)
	}
	return nil, nil
}

// GetTradeActivity 为测试桩提供可注入的活动流水查询行为。
func (s *stubPolymarketSDK) GetTradeActivity(ctx context.Context, user string, limit int) ([]polymarket.DataActivityResponse, error) {
	if s.getTradeActivityFn != nil {
		return s.getTradeActivityFn(ctx, user, limit)
	}
	return nil, nil
}

// GetRedeemableConditions 为测试桩提供可注入的兑奖扫描行为。
func (s *stubPolymarketSDK) GetRedeemableConditions(ctx context.Context, user string) ([]string, int, error) {
	if s.getRedeemableFn != nil {
		return s.getRedeemableFn(ctx, user)
	}
	return nil, 0, nil
}

// RedeemCondition 为测试桩提供可注入的兑奖执行行为。
func (s *stubPolymarketSDK) RedeemCondition(ctx context.Context, conditionID string) (*polymarket.RedeemResult, error) {
	if s.redeemConditionFn != nil {
		return s.redeemConditionFn(ctx, conditionID)
	}
	return nil, nil
}

// discardLogger 返回一个不会向测试输出任何文本的 logger。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHistoryPrefersLiveTrades(t *testing.T) {
	svc := &PolymarketService{
		dashboard: entity.NewDashboardState(),
	}
	svc.dashboard.LiveTrades = []entity.LiveTradeSummary{
		{ID: "agg-1", Slug: "btc-updown-1", Status: "AGG"},
	}
	svc.state.TradeHistory = []entity.TradeHistoryItem{
		{Time: "2026-03-31 10:00:00", Slug: "local-history"},
	}

	items, ok := svc.History().([]entity.LiveTradeSummary)
	if !ok {
		t.Fatalf("expected aggregated live trades payload, got %T", svc.History())
	}
	if len(items) != 1 || items[0].Slug != "btc-updown-1" {
		t.Fatalf("unexpected history payload: %+v", items)
	}
}

func TestHistoryFallsBackToLocalAndWalletHistory(t *testing.T) {
	svc := &PolymarketService{
		dashboard: entity.NewDashboardState(),
	}
	svc.state.TradeHistory = []entity.TradeHistoryItem{
		{Time: "2026-03-31 10:00:00", Slug: "local-history"},
	}
	svc.dashboard.WalletHistory = []entity.TradeHistoryItem{
		{Time: "2026-03-31 10:05:00", Slug: "wallet-history"},
	}

	items, ok := svc.History().([]entity.TradeHistoryItem)
	if !ok {
		t.Fatalf("expected combined trade history payload, got %T", svc.History())
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(items))
	}
	if items[0].Slug != "local-history" || items[1].Slug != "wallet-history" {
		t.Fatalf("unexpected history order: %+v", items)
	}
}

func TestBuildRoundResultsAddsResolvedAndActiveRows(t *testing.T) {
	now := time.Now()
	results := buildRoundResults(
		[]entity.LiveTradeSummary{
			{
				ID:          "agg-closed",
				Slug:        "btc-updown-closed",
				Direction:   "UP",
				OrderTime:   "2026-03-31T10:00:00Z",
				SettleTime:  "2026-03-31T10:15:00Z",
				RedeemCount: 1,
				Profit:      2.5,
				Result:      "CLOSED",
			},
		},
		&entity.ActiveMarket{
			Slug:  "btc-updown-active",
			Start: now.Add(-5 * time.Minute).Format(time.RFC3339),
			End:   now.Add(10 * time.Minute).Format(time.RFC3339),
		},
		&entity.Position{
			Slug: "btc-updown-active",
			Side: "DOWN",
		},
		nil,
		42.5,
	)

	if len(results) != 2 {
		t.Fatalf("expected 2 round results, got %d", len(results))
	}

	bySlug := map[string]entity.RoundResult{}
	for _, item := range results {
		bySlug[item.Slug] = item
	}

	resolved, ok := bySlug["btc-updown-closed"]
	if !ok {
		t.Fatalf("resolved round missing: %+v", results)
	}
	if resolved.Status != "resolved" || resolved.FinalOutcome != "UP" {
		t.Fatalf("unexpected resolved round: %+v", resolved)
	}
	if resolved.Profit == nil || math.Abs(*resolved.Profit-2.5) > 1e-9 {
		t.Fatalf("unexpected resolved profit: %+v", resolved.Profit)
	}

	active, ok := bySlug["btc-updown-active"]
	if !ok {
		t.Fatalf("active round missing: %+v", results)
	}
	if active.Status != "active" || active.EntrySide != "DOWN" {
		t.Fatalf("unexpected active round: %+v", active)
	}
	if active.FinalDiff == nil || math.Abs(*active.FinalDiff-42.5) > 1e-9 {
		t.Fatalf("unexpected active diff: %+v", active.FinalDiff)
	}
}

func TestPlaceTakeProfitOrderWithRetry(t *testing.T) {
	attemptPrices := make([]float64, 0)
	sdk := &stubPolymarketSDK{
		placeLimitOrderFn: func(_ context.Context, _ string, _ string, price float64, sizeShares float64) (string, float64, error) {
			attemptPrices = append(attemptPrices, price)
			if len(attemptPrices) < 3 {
				return "", 0, errors.New("temporary failure")
			}
			return "tp-order-1", sizeShares, nil
		},
	}
	svc := &PolymarketService{
		cfg: polymarket.Config{
			TakeProfitCap:       0.99,
			TakeProfitRetryStep: 0.01,
			TakeProfitRetryMax:  3,
		},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
	}

	submitPrice, orderID, normalizedSize, err := svc.placeTakeProfitOrderWithRetry(context.Background(), "token-1", 8, 0.60)
	if err != nil {
		t.Fatalf("expected retry to succeed, got error %v", err)
	}
	if orderID != "tp-order-1" {
		t.Fatalf("unexpected order id %q", orderID)
	}
	if math.Abs(normalizedSize-8) > 1e-9 {
		t.Fatalf("unexpected normalized size %.4f", normalizedSize)
	}
	if math.Abs(submitPrice-0.62) > 1e-9 {
		t.Fatalf("expected final submit price 0.62, got %.4f", submitPrice)
	}

	wantAttempts := []float64{0.60, 0.61, 0.62}
	if len(attemptPrices) != len(wantAttempts) {
		t.Fatalf("unexpected retry count: %+v", attemptPrices)
	}
	for idx := range wantAttempts {
		if math.Abs(attemptPrices[idx]-wantAttempts[idx]) > 1e-9 {
			t.Fatalf("unexpected retry sequence: %+v", attemptPrices)
		}
	}
}

func TestPlaceTakeProfitOrderWithRetryAdjustsSizeWhenBalanceShort(t *testing.T) {
	attemptSizes := make([]float64, 0)
	sdk := &stubPolymarketSDK{
		placeLimitOrderFn: func(_ context.Context, _ string, _ string, _ float64, sizeShares float64) (string, float64, error) {
			attemptSizes = append(attemptSizes, sizeShares)
			if len(attemptSizes) == 1 {
				return "", 0, errors.New(`POST https://clob.polymarket.com/order failed with status 400: {"error": "not enough balance allowance: the balance is not enough -> balance: 7718750, order amount: 7930000"}`)
			}
			return "tp-order-adjusted", sizeShares, nil
		},
	}

	svc := &PolymarketService{
		cfg: polymarket.Config{
			TakeProfitCap:       0.99,
			TakeProfitRetryStep: 0.01,
			TakeProfitRetryMax:  2,
		},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
	}

	submitPrice, orderID, normalizedSize, err := svc.placeTakeProfitOrderWithRetry(context.Background(), "token-1", 7.93, 0.60)
	if err != nil {
		t.Fatalf("expected adjusted retry to succeed, got error %v", err)
	}
	if orderID != "tp-order-adjusted" {
		t.Fatalf("unexpected order id %q", orderID)
	}
	if math.Abs(submitPrice-0.60) > 1e-9 {
		t.Fatalf("expected submit price to stay at 0.60, got %.4f", submitPrice)
	}
	if normalizedSize >= 7.93 {
		t.Fatalf("expected normalized size to be reduced, got %.6f", normalizedSize)
	}
	if len(attemptSizes) != 2 {
		t.Fatalf("expected two sell attempts, got %d", len(attemptSizes))
	}
	if !(attemptSizes[1] < attemptSizes[0]) {
		t.Fatalf("expected second attempt to use smaller size, got %+v", attemptSizes)
	}
}

func TestSubmitTUIQuickOrderUsesBestAskAndDefaultAmount(t *testing.T) {
	var gotTokenID string
	var gotAction string
	var gotPrice float64
	var gotSize float64

	sdk := &stubPolymarketSDK{
		placeLimitOrderFn: func(_ context.Context, tokenID string, action string, price float64, sizeShares float64) (string, float64, error) {
			gotTokenID = tokenID
			gotAction = action
			gotPrice = price
			gotSize = sizeShares
			return "order-up-1", sizeShares, nil
		},
	}

	upAsk := 0.62
	svc := &PolymarketService{
		cfg: polymarket.Config{
			TradeAmount: 12.4,
		},
		repo:        noopStateRepo{},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
		activeMarket: &entity.ActiveMarket{
			Slug:    "eth-updown-15m",
			UpToken: "up-token-1",
		},
	}
	svc.price.upAsk = &upAsk

	resp, err := svc.SubmitTUIQuickOrder(context.Background(), "BUY", "UP")
	if err != nil {
		t.Fatalf("expected quick order to succeed, got %v", err)
	}
	if resp.OrderID != "order-up-1" || resp.Action != "BUY" || resp.Outcome != "UP" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if gotTokenID != "up-token-1" || gotAction != "BUY" {
		t.Fatalf("unexpected place args: token=%s action=%s", gotTokenID, gotAction)
	}
	if math.Abs(gotPrice-0.62) > 1e-9 {
		t.Fatalf("expected quick order price 0.62, got %.4f", gotPrice)
	}
	if math.Abs(gotSize-20) > 1e-9 {
		t.Fatalf("expected quick order size 20, got %.4f", gotSize)
	}
}

func TestCancelActiveOrderClearsPendingOrder(t *testing.T) {
	var cancelledOrderID string
	sdk := &stubPolymarketSDK{
		cancelOrderFn: func(_ context.Context, orderID string) error {
			cancelledOrderID = orderID
			return nil
		},
	}

	svc := &PolymarketService{
		repo:        noopStateRepo{},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
		state: entity.PolymarketState{
			PendingOrder: &entity.PendingOrder{
				OrderID: "pending-1",
				Slug:    "btc-updown-15m",
				Action:  "BUY",
				Side:    "UP",
				Price:   0.61,
				Amount:  10,
				Size:    16.39,
			},
		},
	}

	if err := svc.CancelActiveOrder(context.Background()); err != nil {
		t.Fatalf("expected cancel to succeed, got %v", err)
	}
	if cancelledOrderID != "pending-1" {
		t.Fatalf("unexpected cancelled order id %q", cancelledOrderID)
	}
	if svc.state.PendingOrder != nil {
		t.Fatalf("expected pending order to be cleared, got %+v", svc.state.PendingOrder)
	}
	if len(svc.state.TradeHistory) == 0 || svc.state.TradeHistory[len(svc.state.TradeHistory)-1].Status != "cancelled" {
		t.Fatalf("expected cancellation history to be recorded, got %+v", svc.state.TradeHistory)
	}
}

func TestEvaluateAutoTradeRecordsConcreteOrderError(t *testing.T) {
	sdk := &stubPolymarketSDK{
		placeLimitOrderFn: func(_ context.Context, _ string, _ string, _ float64, _ float64) (string, float64, error) {
			return "", 0, errors.New("clob rejected order")
		},
	}
	btc := 100.0
	ptb := 50.0
	upAsk := 0.81
	downAsk := 0.19

	svc := &PolymarketService{
		cfg: polymarket.Config{
			AutoTrade:           true,
			TradeAmount:         5,
			MarketDataMaxLagSec: 5,
			Conditions: []polymarket.ConditionConfig{
				{Time: 120, Diff: 30, MinProb: 0.80, MaxProb: 0.92, Side: "UP"},
			},
		},
		repo:        noopStateRepo{},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
		activeMarket: &entity.ActiveMarket{
			Slug:    "btc-updown-15m",
			End:     time.Now().Add(30 * time.Second).Format(time.RFC3339),
			UpToken: "up-token-1",
		},
	}
	svc.price.btc = &btc
	svc.price.ptb = &ptb
	svc.price.upAsk = &upAsk
	svc.price.downAsk = &downAsk
	svc.price.btcUpdateTS = time.Now()
	svc.price.upUpdateTS = time.Now()
	svc.price.downUpdateTS = time.Now()

	svc.evaluateAutoTrade(context.Background())

	if svc.state.LastOrder == nil {
		t.Fatalf("expected last order to be recorded")
	}
	if svc.state.LastOrder.Error != "clob rejected order" {
		t.Fatalf("unexpected last order error: %+v", svc.state.LastOrder)
	}
	if len(svc.state.TradeHistory) == 0 {
		t.Fatalf("expected failed trade history to be recorded")
	}
	last := svc.state.TradeHistory[len(svc.state.TradeHistory)-1]
	if last.Status != "failed" || last.Error != "clob rejected order" {
		t.Fatalf("unexpected failed history row: %+v", last)
	}
}

func TestEvaluateAutoTradeRecordsDiffMissDiagnostics(t *testing.T) {
	btc := 100.0
	ptb := 80.0
	upAsk := 0.81
	downAsk := 0.19

	svc := &PolymarketService{
		cfg: polymarket.Config{
			AutoTrade:           true,
			TradeAmount:         5,
			MarketDataMaxLagSec: 5,
			Conditions: []polymarket.ConditionConfig{
				{Time: 120, Diff: 30, MinProb: 0.80, MaxProb: 0.92, Side: "UP"},
			},
		},
		repo:        noopStateRepo{},
		client:      &stubPolymarketSDK{},
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
		activeMarket: &entity.ActiveMarket{
			Slug:    "btc-updown-15m",
			End:     time.Now().Add(30 * time.Second).Format(time.RFC3339),
			UpToken: "up-token-1",
		},
	}
	svc.price.btc = &btc
	svc.price.ptb = &ptb
	svc.price.upAsk = &upAsk
	svc.price.downAsk = &downAsk
	svc.price.btcUpdateTS = time.Now()
	svc.price.upUpdateTS = time.Now()
	svc.price.downUpdateTS = time.Now()

	svc.evaluateAutoTrade(context.Background())

	if svc.autoTrade.SampleCount != 1 {
		t.Fatalf("expected one diagnostics sample, got %+v", svc.autoTrade)
	}
	if svc.autoTrade.DiffMissCount != 1 {
		t.Fatalf("expected diff miss diagnostics, got %+v", svc.autoTrade)
	}
	if svc.autoTrade.TriggerCount != 0 {
		t.Fatalf("expected no trigger when diff misses, got %+v", svc.autoTrade)
	}
	if svc.autoTrade.LastReason == "" {
		t.Fatalf("expected concrete diff miss reason, got %+v", svc.autoTrade)
	}
}

func TestEvaluateAutoTradeRecordsProbabilityMissDiagnostics(t *testing.T) {
	btc := 100.0
	ptb := 50.0
	upAsk := 0.95
	downAsk := 0.05

	svc := &PolymarketService{
		cfg: polymarket.Config{
			AutoTrade:           true,
			TradeAmount:         5,
			MarketDataMaxLagSec: 5,
			Conditions: []polymarket.ConditionConfig{
				{Time: 120, Diff: 30, MinProb: 0.80, MaxProb: 0.92, Side: "UP"},
			},
		},
		repo:        noopStateRepo{},
		client:      &stubPolymarketSDK{},
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
		activeMarket: &entity.ActiveMarket{
			Slug:    "btc-updown-15m",
			End:     time.Now().Add(30 * time.Second).Format(time.RFC3339),
			UpToken: "up-token-1",
		},
	}
	svc.price.btc = &btc
	svc.price.ptb = &ptb
	svc.price.upAsk = &upAsk
	svc.price.downAsk = &downAsk
	svc.price.btcUpdateTS = time.Now()
	svc.price.upUpdateTS = time.Now()
	svc.price.downUpdateTS = time.Now()

	svc.evaluateAutoTrade(context.Background())

	if svc.autoTrade.SampleCount != 1 {
		t.Fatalf("expected one diagnostics sample, got %+v", svc.autoTrade)
	}
	if svc.autoTrade.ProbabilityMissCount != 1 {
		t.Fatalf("expected probability miss diagnostics, got %+v", svc.autoTrade)
	}
	if svc.autoTrade.LastReason == "" {
		t.Fatalf("expected concrete probability miss reason, got %+v", svc.autoTrade)
	}
}

func TestEvaluateAutoTradeSupportsRelativeDiffBpsAcrossMarkets(t *testing.T) {
	referencePrice := 2000.0
	ptb := 1999.1
	upAsk := 0.81
	downAsk := 0.19

	sdk := &stubPolymarketSDK{
		placeLimitOrderFn: func(_ context.Context, tokenID, action string, price, size float64) (string, float64, error) {
			if tokenID != "eth-up-token" {
				t.Fatalf("unexpected token id: %s", tokenID)
			}
			if action != "BUY" {
				t.Fatalf("unexpected action: %s", action)
			}
			if price != upAsk {
				t.Fatalf("unexpected price %.4f", price)
			}
			return "order-eth-1", size, nil
		},
	}

	svc := &PolymarketService{
		cfg: polymarket.Config{
			AutoTrade:           true,
			TradeAmount:         5,
			MarketDataMaxLagSec: 5,
			Conditions: []polymarket.ConditionConfig{
				{Time: 120, Diff: 30, DiffBps: 4, MinProb: 0.80, MaxProb: 0.92, Side: "UP"},
			},
		},
		repo:        noopStateRepo{},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
		activeMarket: &entity.ActiveMarket{
			Slug:    "eth-updown-15m",
			End:     time.Now().Add(30 * time.Second).Format(time.RFC3339),
			UpToken: "eth-up-token",
		},
	}
	svc.price.btc = &referencePrice
	svc.price.ptb = &ptb
	svc.price.upAsk = &upAsk
	svc.price.downAsk = &downAsk
	svc.price.btcUpdateTS = time.Now()
	svc.price.upUpdateTS = time.Now()
	svc.price.downUpdateTS = time.Now()

	svc.evaluateAutoTrade(context.Background())

	if svc.state.PendingOrder == nil {
		t.Fatalf("expected relative diff bps condition to trigger an order")
	}
	if svc.state.PendingOrder.OrderID != "order-eth-1" {
		t.Fatalf("unexpected pending order: %+v", svc.state.PendingOrder)
	}
}

func TestRedeemConditionWithRetryDoesNotResubmitSubmittedRelayerTxn(t *testing.T) {
	attempts := 0
	sdk := &stubPolymarketSDK{
		redeemConditionFn: func(_ context.Context, _ string) (*polymarket.RedeemResult, error) {
			attempts++
			return &polymarket.RedeemResult{
				TransactionID: "txn-relayer-1",
				State:         "STATE_EXECUTED",
			}, context.DeadlineExceeded
		},
	}

	svc := &PolymarketService{
		cfg: polymarket.Config{
			AutoRedeemMaxRetry:   3,
			AutoRedeemReceiptSec: 1,
		},
		client:      sdk,
		logger:      discardLogger(),
		dashboard:   entity.NewDashboardState(),
		subscribers: map[int]chan entity.DashboardState{},
	}

	row, err := svc.redeemConditionWithRetry(context.Background(), "0x1111111111111111111111111111111111111111111111111111111111111111")
	if err == nil {
		t.Fatalf("expected timeout error when relayer transaction stays pending")
	}
	if attempts != 1 {
		t.Fatalf("expected submitted relayer transaction not to be retried, got %d attempts", attempts)
	}
	if row["transaction_id"] != "txn-relayer-1" {
		t.Fatalf("expected transaction id to be preserved, got %+v", row)
	}
}
