package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

type testOrderRepo struct {
	items []entity.OrderRecord
}

type testExecRepo struct {
	mu    sync.Mutex
	items []entity.ExecutionRecord
}

func (r *testExecRepo) Upsert(_ context.Context, item *entity.ExecutionRecord) error {
	if item == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.items {
		if r.items[i].PlanKey == item.PlanKey {
			if item.ID == 0 {
				item.ID = r.items[i].ID
			}
			r.items[i] = *item
			return nil
		}
	}
	if item.ID == 0 {
		item.ID = uint(len(r.items) + 1)
	}
	r.items = append(r.items, *item)
	return nil
}

func (r *testExecRepo) TryClaimAction(_ context.Context, item *entity.ExecutionRecord, allowedCurrentStatuses []string) (*entity.ExecutionRecord, bool, error) {
	if item == nil {
		return nil, false, fmt.Errorf("nil execution record")
	}
	allowed := make(map[string]struct{}, len(allowedCurrentStatuses))
	for _, status := range allowedCurrentStatuses {
		allowed[normalizeExecutionStatus(status)] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.items {
		if r.items[i].PlanKey != item.PlanKey {
			continue
		}
		current := r.items[i]
		if _, ok := allowed[normalizeExecutionStatus(current.Status)]; !ok {
			cp := current
			return &cp, false, nil
		}
		updated := *item
		if updated.ID == 0 {
			updated.ID = current.ID
		}
		if updated.ID == 0 {
			updated.ID = uint(i + 1)
		}
		r.items[i] = updated
		cp := r.items[i]
		return &cp, true, nil
	}

	if _, ok := allowed[""]; !ok {
		return nil, false, nil
	}
	inserted := *item
	if inserted.ID == 0 {
		inserted.ID = uint(len(r.items) + 1)
	}
	r.items = append(r.items, inserted)
	cp := inserted
	return &cp, true, nil
}

func (r *testExecRepo) FindByPlanKey(_ context.Context, planKey string) (*entity.ExecutionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.items {
		if r.items[i].PlanKey == planKey {
			cp := r.items[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *testExecRepo) ListLatest(_ context.Context, limit int) ([]entity.ExecutionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]entity.ExecutionRecord(nil), r.items...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *testExecRepo) ListActiveLive(_ context.Context) ([]entity.ExecutionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entity.ExecutionRecord, 0, len(r.items))
	for _, item := range r.items {
		if countsTowardLivePlanLimit(item) {
			out = append(out, item)
		}
	}
	return out, nil
}

type testPlanRepo struct {
	items []entity.ExecutionPlan
}

func (r *testPlanRepo) SaveBatch(_ context.Context, _, _ string, items []entity.ExecutionPlan) error {
	r.items = append([]entity.ExecutionPlan(nil), items...)
	return nil
}

func (r *testPlanRepo) ListLatest(_ context.Context, _ int) ([]entity.ExecutionPlan, error) {
	return append([]entity.ExecutionPlan(nil), r.items...), nil
}

func (r *testPlanRepo) ListByOpportunityBatch(_ context.Context, _ string, _ int) ([]entity.ExecutionPlan, error) {
	return append([]entity.ExecutionPlan(nil), r.items...), nil
}

func (r *testPlanRepo) FindByPlanKey(_ context.Context, planKey string) (*entity.ExecutionPlan, error) {
	for i := range r.items {
		if r.items[i].PlanKey == planKey {
			cp := r.items[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *testOrderRepo) Create(_ context.Context, item *entity.OrderRecord) error {
	r.items = append(r.items, *item)
	return nil
}

func (r *testOrderRepo) Update(_ context.Context, item *entity.OrderRecord) error {
	for i := range r.items {
		if r.items[i].ID == item.ID && item.ID != 0 {
			r.items[i] = *item
			return nil
		}
		if r.items[i].PlanKey == item.PlanKey &&
			r.items[i].Exchange == item.Exchange &&
			r.items[i].ClientOrderID == item.ClientOrderID &&
			r.items[i].Phase == item.Phase {
			r.items[i] = *item
			return nil
		}
	}
	return nil
}

func (r *testOrderRepo) FindByExternalOrderID(_ context.Context, exchangeName, clientOrderID, venueOrderID string) (*entity.OrderRecord, error) {
	for i := range r.items {
		if r.items[i].Exchange != exchangeName {
			continue
		}
		if clientOrderID != "" && r.items[i].ClientOrderID == clientOrderID {
			cp := r.items[i]
			return &cp, nil
		}
		if venueOrderID != "" && r.items[i].VenueOrderID == venueOrderID {
			cp := r.items[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *testOrderRepo) ListByPlanKey(_ context.Context, planKey string) ([]entity.OrderRecord, error) {
	out := make([]entity.OrderRecord, 0)
	for _, it := range r.items {
		if it.PlanKey == planKey {
			out = append(out, it)
		}
	}
	return out, nil
}

func (r *testOrderRepo) ListLatest(_ context.Context, _ int) ([]entity.OrderRecord, error) {
	return append([]entity.OrderRecord(nil), r.items...), nil
}

type testTradeAdapter struct {
	mu           sync.Mutex
	name         string
	enabled      bool
	caps         exchange.TradeCapabilities
	placeErr     error
	cancelErr    error
	accountErr   error
	positionErr  error
	placed       []exchange.TradeOrderRequest
	closed       []exchange.TradeOrderRequest
	canceled     []exchange.OrderLookupRequest
	closeResp    exchange.TradeOrderResult
	orderStatus  exchange.OrderStatus
	cancelStatus exchange.OrderStatus
	account      exchange.AccountSnapshot
	position     exchange.Position
	streamEvents []exchange.OrderEvent
	placeStarted chan string
	placeBlock   <-chan struct{}
	placeWaitCtx bool
}

func (a *testTradeAdapter) Name() string  { return a.name }
func (a *testTradeAdapter) Enabled() bool { return a.enabled }
func (a *testTradeAdapter) Capabilities() exchange.TradeCapabilities {
	if a.caps.MakerLimitTIF != "" || a.caps.TakerOrderType != "" || a.caps.TakerTimeInForce != "" || a.caps.TakerUsesAggressiveIOC {
		if len(a.streamEvents) > 0 {
			a.caps.SupportsOrderEventStream = true
		}
		return a.caps
	}
	return exchange.TradeCapabilities{
		MakerLimitTIF:            "GTX",
		TakerOrderType:           "MARKET",
		TakerUsesAggressiveIOC:   false,
		SupportsOrderEventStream: len(a.streamEvents) > 0,
	}
}
func (a *testTradeAdapter) PlaceOrder(ctx context.Context, req exchange.TradeOrderRequest) (exchange.TradeOrderResult, error) {
	a.mu.Lock()
	a.placed = append(a.placed, req)
	a.mu.Unlock()
	if a.placeStarted != nil {
		a.placeStarted <- req.ClientOrderID
	}
	if a.placeWaitCtx {
		<-ctx.Done()
		return exchange.TradeOrderResult{}, ctx.Err()
	}
	if a.placeBlock != nil {
		select {
		case <-a.placeBlock:
		case <-ctx.Done():
			return exchange.TradeOrderResult{}, ctx.Err()
		}
	}
	if a.placeErr != nil {
		return exchange.TradeOrderResult{}, a.placeErr
	}
	return exchange.TradeOrderResult{Status: "NEW", ClientOrderID: req.ClientOrderID}, nil
}
func (a *testTradeAdapter) ClosePosition(_ context.Context, req exchange.TradeOrderRequest) (exchange.TradeOrderResult, error) {
	a.mu.Lock()
	a.closed = append(a.closed, req)
	a.mu.Unlock()
	if a.closeResp.Status == "" {
		a.closeResp.Status = "FILLED"
	}
	if a.closeResp.ClientOrderID == "" {
		a.closeResp.ClientOrderID = req.ClientOrderID
	}
	return a.closeResp, nil
}
func (a *testTradeAdapter) GetPosition(_ context.Context, _, _, _ string) (exchange.Position, error) {
	if a.positionErr != nil {
		return exchange.Position{}, a.positionErr
	}
	return a.position, nil
}
func (a *testTradeAdapter) GetOrderStatus(_ context.Context, _ exchange.OrderLookupRequest) (exchange.OrderStatus, error) {
	if a.orderStatus.Status == "" {
		a.orderStatus.Status = "FILLED"
		a.orderStatus.Terminal = true
	}
	return a.orderStatus, nil
}
func (a *testTradeAdapter) CancelOrder(_ context.Context, req exchange.OrderLookupRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.canceled = append(a.canceled, req)
	if a.cancelErr != nil {
		return a.cancelErr
	}
	if a.cancelStatus.Status != "" {
		a.orderStatus = a.cancelStatus
	} else {
		a.orderStatus = exchange.OrderStatus{
			Exchange:      a.name,
			Status:        "CANCELED",
			VenueOrderID:  req.VenueOrderID,
			ClientOrderID: req.ClientOrderID,
			Terminal:      true,
			Canceled:      true,
		}
	}
	return nil
}
func (a *testTradeAdapter) GetAccountSnapshot(_ context.Context) (exchange.AccountSnapshot, error) {
	if a.accountErr != nil {
		return exchange.AccountSnapshot{}, a.accountErr
	}
	return a.account, nil
}

func newTestExecutionService(orderRepo *testOrderRepo, trades map[string]exchange.TradeAdapter) *ExecutionService {
	return &ExecutionService{
		cfg:             Config{Leverage: 2, Execution: ExecutionConfig{OrderStatusPollAttempts: 1, OrderStatusPollInterval: time.Millisecond, APIFailureThreshold: 2, APIFailureCooldown: time.Minute, MinAvailableBalanceRatio: 0.05}}.normalize(),
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:           NewMarketStore(),
		orderRepo:       orderRepo,
		trades:          trades,
		orderEventCh:    make(chan exchange.OrderEvent, 32),
		exchangeFailure: map[string]exchangeFailureState{},
	}
}

func seedAutoOpenMarket(store *MarketStore, now time.Time, symbol, longExchange, shortExchange string, price float64) {
	venueSymbol := symbol + "USDT"
	store.UpsertSymbol(entity.Symbol{Exchange: longExchange, Symbol: symbol, VenueSymbol: venueSymbol, StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	store.UpsertSymbol(entity.Symbol{Exchange: shortExchange, Symbol: symbol, VenueSymbol: venueSymbol, StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: longExchange, Symbol: symbol, VenueSymbol: venueSymbol, BidPrice: price, AskPrice: price, EventTimeMs: now.UnixMilli()})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: shortExchange, Symbol: symbol, VenueSymbol: venueSymbol, BidPrice: price, AskPrice: price, EventTimeMs: now.UnixMilli()})
	store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             longExchange,
		Symbol:               symbol,
		VenueSymbol:          venueSymbol,
		FundingRate:          -0.0020,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})
	store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             shortExchange,
		Symbol:               symbol,
		VenueSymbol:          venueSymbol,
		FundingRate:          0.0020,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})
}

func readyPlan(planKey, symbol, longExchange, shortExchange string, price, qty, notional float64) entity.ExecutionPlan {
	venueSymbol := symbol + "USDT"
	return entity.ExecutionPlan{
		PlanKey:              planKey,
		Symbol:               symbol,
		Status:               "ready",
		ReadyNow:             true,
		LongExchange:         longExchange,
		ShortExchange:        shortExchange,
		LongVenueSymbol:      venueSymbol,
		ShortVenueSymbol:     venueSymbol,
		LongEntryPrice:       price,
		ShortEntryPrice:      price,
		LongQty:              qty,
		ShortQty:             qty,
		LongMinQty:           0.001,
		ShortMinQty:          0.001,
		LongMinNotionalUSDT:  10,
		ShortMinNotionalUSDT: 10,
		TargetNotionalUSDT:   notional,
		RoundedNotionalUSDT:  notional,
		EntryMode:            "taker",
		ExitMode:             "taker",
	}
}

func (a *testTradeAdapter) StartOrderEventStream(ctx context.Context, sink exchange.OrderEventSink) error {
	for _, event := range a.streamEvents {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			sink.PublishOrderEvent(event)
		}
	}
	return nil
}

func TestPlacePlanOrders_OpenPartialFailureTriggersHedgeClose(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", TickSize: "0.01", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", TickSize: "0.01", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}, orderStatus: exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, placeErr: errors.New("short leg failed"), account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	orderRepo := &testOrderRepo{}

	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.store = store
	plan := &entity.ExecutionPlan{PlanKey: "aaaaaaaaaabbbbbbbbbbccccccccccdddddddddd", Symbol: "BTCUSDT", LongExchange: "longex", ShortExchange: "shortex", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT", LongQty: 1, ShortQty: 1, LongEntryPrice: 101, ShortEntryPrice: 102, EntryMode: "taker"}

	results, errMsg := svc.placePlanOrders(context.Background(), plan, "open", "manual")
	if errMsg == "" {
		t.Fatalf("expected aggregated error message")
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 records (2 open + 1 hedge), got %d", len(results))
	}
	if len(longAdapter.closed) != 1 {
		t.Fatalf("expected one hedge close on successful leg, got %d", len(longAdapter.closed))
	}
	hedgeReq := longAdapter.closed[0]
	if hedgeReq.Reason != "open_leg_failed_hedge" {
		t.Fatalf("expected hedge reason, got %s", hedgeReq.Reason)
	}
	if hedgeReq.Side != "SELL" {
		t.Fatalf("expected hedge close side SELL for previously opened BUY leg, got %s", hedgeReq.Side)
	}
	if !hedgeReq.ReduceOnly {
		t.Fatalf("expected hedge request reduce-only")
	}
	if hedgeReq.OrderType != "LIMIT" {
		t.Fatalf("expected hedge order type LIMIT, got %s", hedgeReq.OrderType)
	}
	if hedgeReq.TimeInForce != "IOC" {
		t.Fatalf("expected hedge tif IOC, got %s", hedgeReq.TimeInForce)
	}
	if hedgeReq.Price <= 0 {
		t.Fatalf("expected hedge limit price to be populated, got %f", hedgeReq.Price)
	}
	if hedgeReq.ClientOrderID == longAdapter.placed[0].ClientOrderID {
		t.Fatalf("expected hedge client order id to differ from open order id")
	}

	hedgeRecord := results[2]
	if hedgeRecord.Phase != "hedge_close" {
		t.Fatalf("expected hedge phase record, got %s", hedgeRecord.Phase)
	}
	if got := summarizeExecutionStatus(results, "open"); got != executionStateOpenHedging {
		t.Fatalf("expected hedging state, got %s", got)
	}
}

func TestPlacePlanOrders_ExecutesPrimaryLegsConcurrently(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	release := make(chan struct{})
	started := make(chan string, 2)
	longAdapter := &testTradeAdapter{
		name:         "longex",
		enabled:      true,
		account:      exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus:  exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
		placeStarted: started,
		placeBlock:   release,
	}
	shortAdapter := &testTradeAdapter{
		name:         "shortex",
		enabled:      true,
		account:      exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus:  exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
		placeStarted: started,
		placeBlock:   release,
	}

	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.store = store
	plan := &entity.ExecutionPlan{
		PlanKey:          "plan-concurrent-legs",
		Symbol:           "BTCUSDT",
		LongExchange:     "longex",
		ShortExchange:    "shortex",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
		LongQty:          1,
		ShortQty:         1,
		LongEntryPrice:   101,
		ShortEntryPrice:  102,
		EntryMode:        "taker",
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.placePlanOrders(context.Background(), plan, "open", "manual")
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected both primary legs to start before release, indicating concurrent execution")
		}
	}

	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected placePlanOrders to finish after releasing both primary legs")
	}
}

func TestPlacePlanOrders_TimeoutLegRecoveredByReconcile(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}, orderStatus: exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true}}
	shortAdapter := &testTradeAdapter{
		name:         "shortex",
		enabled:      true,
		account:      exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus:  exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
		placeWaitCtx: true,
	}

	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.store = store
	svc.cfg.Execution.PrimaryLegTimeout = 10 * time.Millisecond
	plan := &entity.ExecutionPlan{PlanKey: "plan-timeout-reconciled", Symbol: "BTCUSDT", LongExchange: "longex", ShortExchange: "shortex", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT", LongQty: 1, ShortQty: 1, LongEntryPrice: 101, ShortEntryPrice: 102, EntryMode: "taker"}

	results, errMsg := svc.placePlanOrders(context.Background(), plan, "open", "manual")
	if errMsg != "" {
		t.Fatalf("expected timeout leg to be recovered by reconcile, got errMsg=%s", errMsg)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 primary leg records, got %d", len(results))
	}
	if got := summarizeExecutionStatus(results, "open"); got != executionStateOpened {
		t.Fatalf("expected opened status after timeout reconcile, got %s", got)
	}
	for _, item := range results {
		if item.Status != "FILLED" {
			t.Fatalf("expected reconciled legs to end as FILLED, got %#v", item)
		}
		if item.ErrorMessage != "" {
			t.Fatalf("expected reconciled timeout leg to clear error message, got %#v", item)
		}
	}
}

func TestPlacePlanOrders_TimeoutLegWithoutReconcileTriggersHedge(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}, orderStatus: exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true}}
	shortAdapter := &testTradeAdapter{
		name:         "shortex",
		enabled:      true,
		account:      exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus:  exchange.OrderStatus{Status: "NEW", ExecutedQty: 0, Terminal: false},
		placeWaitCtx: true,
	}

	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.store = store
	svc.cfg.Execution.PrimaryLegTimeout = 10 * time.Millisecond
	plan := &entity.ExecutionPlan{PlanKey: "plan-timeout-unresolved", Symbol: "BTCUSDT", LongExchange: "longex", ShortExchange: "shortex", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT", LongQty: 1, ShortQty: 1, LongEntryPrice: 101, ShortEntryPrice: 102, EntryMode: "taker"}

	results, errMsg := svc.placePlanOrders(context.Background(), plan, "open", "manual")
	if errMsg == "" {
		t.Fatal("expected unresolved timeout leg to remain in aggregated errors")
	}
	if len(longAdapter.closed) != 1 {
		t.Fatalf("expected successful leg to enter hedge recovery, got %d hedge closes", len(longAdapter.closed))
	}
	if got := summarizeExecutionStatus(results, "open"); got != executionStateOpenHedging {
		t.Fatalf("expected open_hedging status after unresolved timeout, got %s", got)
	}
}

func TestPlacePlanOrders_PartialFillTriggersHedgeForAllExposedLegs(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	longAdapter := &testTradeAdapter{
		name:        "longex",
		enabled:     true,
		account:     exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus: exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
	}
	shortAdapter := &testTradeAdapter{
		name:        "shortex",
		enabled:     true,
		account:     exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus: exchange.OrderStatus{Status: "PARTIALLY_FILLED", ExecutedQty: 0.4, Terminal: false},
	}

	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.store = store
	plan := &entity.ExecutionPlan{
		PlanKey:          "plan-partial-fill-hedge",
		Symbol:           "BTCUSDT",
		LongExchange:     "longex",
		ShortExchange:    "shortex",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
		LongQty:          1,
		ShortQty:         1,
		LongEntryPrice:   101,
		ShortEntryPrice:  102,
		EntryMode:        "taker",
	}

	results, errMsg := svc.placePlanOrders(context.Background(), plan, "open", "manual")
	if errMsg == "" {
		t.Fatal("expected partial fill to surface as aggregated error")
	}
	if got := summarizeExecutionStatus(results, "open"); got != executionStateOpenHedging {
		t.Fatalf("expected open_hedging status, got %s", got)
	}
	if len(longAdapter.closed) != 1 {
		t.Fatalf("expected long leg exposure to be hedged, got %d hedge orders", len(longAdapter.closed))
	}
	if len(shortAdapter.closed) != 1 {
		t.Fatalf("expected partially filled short leg exposure to be hedged, got %d hedge orders", len(shortAdapter.closed))
	}
}

func TestPlacePlanOrders_UnfilledOpenLegIsCanceled(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", TickSize: "0.01", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", TickSize: "0.01", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	longAdapter := &testTradeAdapter{
		name:        "longex",
		enabled:     true,
		account:     exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus: exchange.OrderStatus{Status: "NEW", ExecutedQty: 0, Terminal: false},
	}
	shortAdapter := &testTradeAdapter{
		name:        "shortex",
		enabled:     true,
		account:     exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus: exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
	}

	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.store = store
	plan := &entity.ExecutionPlan{
		PlanKey:          "plan-unfilled-open-cancel",
		Symbol:           "BTCUSDT",
		LongExchange:     "longex",
		ShortExchange:    "shortex",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
		LongQty:          1,
		ShortQty:         1,
		LongEntryPrice:   101,
		ShortEntryPrice:  102,
		EntryMode:        "maker",
	}

	results, errMsg := svc.placePlanOrders(context.Background(), plan, "open", "manual")
	if errMsg == "" {
		t.Fatal("expected unresolved open leg to surface as aggregated error")
	}
	if len(longAdapter.canceled) != 1 {
		t.Fatalf("expected unresolved long leg to be canceled, got %d cancels", len(longAdapter.canceled))
	}
	if got := results[0].Status; got != "CANCELED" {
		t.Fatalf("expected unresolved long leg to reconcile as CANCELED after cancel, got %s", got)
	}
}

func TestEnforceRiskControls_BlocksLowBalance(t *testing.T) {
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 10}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 10}}
	orderRepo := &testOrderRepo{}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.cfg.Execution.MinAvailableBalanceRatio = 0.2
	plan := &entity.ExecutionPlan{Symbol: "BTCUSDT", LongExchange: "longex", ShortExchange: "shortex", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT", LongQty: 1, ShortQty: 1, LongEntryPrice: 100, ShortEntryPrice: 100}
	if err := svc.enforceRiskControls(context.Background(), plan); err == nil {
		t.Fatal("expected low balance risk block")
	}
}

func TestEnforceRiskControls_CircuitBreaker(t *testing.T) {
	adapter := &testTradeAdapter{name: "longex", enabled: true, accountErr: errors.New("boom")}
	other := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 500}}
	orderRepo := &testOrderRepo{}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": adapter, "shortex": other})
	plan := &entity.ExecutionPlan{Symbol: "BTCUSDT", LongExchange: "longex", ShortExchange: "shortex", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT", LongQty: 1, ShortQty: 1, LongEntryPrice: 100, ShortEntryPrice: 100}
	_ = svc.enforceRiskControls(context.Background(), plan)
	_ = svc.enforceRiskControls(context.Background(), plan)
	if err := svc.ensureExchangeAvailable("longex"); err == nil {
		t.Fatal("expected circuit breaker to open after repeated api failures")
	}
}

func TestReconcileOrderUsesOrderStatusFill(t *testing.T) {
	adapter := &testTradeAdapter{name: "longex", enabled: true, orderStatus: exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, AveragePrice: 100, Terminal: true}}
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": adapter})
	rec := entity.OrderRecord{Status: "NEW", ClientOrderID: "abc", RequestedQty: 1}
	out := svc.reconcileOrder(context.Background(), adapter, rec, exchange.TradeOrderRequest{CanonicalSymbol: "BTCUSDT", VenueSymbol: "BTCUSDT", Quantity: 1})
	if out.Status != "FILLED" || out.ExecutedQty != 1 {
		t.Fatalf("expected reconcile fill, got status=%s qty=%f", out.Status, out.ExecutedQty)
	}
}

func TestReverseSide(t *testing.T) {
	if got := reverseSide("BUY"); got != "SELL" {
		t.Fatalf("expected BUY -> SELL, got %s", got)
	}
	if got := reverseSide("sell"); got != "BUY" {
		t.Fatalf("expected sell -> BUY, got %s", got)
	}
	if got := reverseSide("UNKNOWN"); got != "UNKNOWN" {
		t.Fatalf("expected unknown side to remain unchanged, got %s", got)
	}
}

func TestShouldAutoCloseRecord_AllowsOpenedRecoveryAndRetryCloseStates(t *testing.T) {
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStateOpened}) {
		t.Fatal("expected opened record to be auto-close eligible")
	}
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: "dry_run_opened"}) {
		t.Fatal("expected dry_run_opened record to be auto-close eligible")
	}
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStateOpenPartial}) {
		t.Fatal("expected open_partial_failed to be routed into recovery auto-close")
	}
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStateOpenHedging}) {
		t.Fatal("expected open_hedging to be routed into recovery auto-close")
	}
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStateClosePartial}) {
		t.Fatal("expected close_partial_failed to be routed into retry auto-close")
	}
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStateCloseFailed}) {
		t.Fatal("expected close_failed to be routed into retry auto-close")
	}
	if !shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStateCloseHedging}) {
		t.Fatal("expected close_hedging to be routed into retry auto-close")
	}
	if shouldAutoCloseRecord(entity.ExecutionRecord{Status: executionStatePendingClose}) {
		t.Fatal("expected pending_close to stay out of duplicate auto-close scans")
	}
}

func TestInspectLiveAutoClose_ReportsDecisionAndRetryCloseCandidates(t *testing.T) {
	now := time.Now().UTC()
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:           "plan-retry-close",
				Symbol:            "BTC",
				Status:            executionStateCloseFailed,
				LiveTrading:       true,
				AutoClose:         true,
				TargetCloseTimeMs: now.Add(2 * time.Minute).UnixMilli(),
			},
			{
				PlanKey:           "plan-disabled-auto-close",
				Symbol:            "ETH",
				Status:            executionStateOpened,
				LiveTrading:       true,
				AutoClose:         false,
				TargetCloseTimeMs: now.Add(3 * time.Minute).UnixMilli(),
			},
			{
				PlanKey:           "plan-pending-close",
				Symbol:            "SOL",
				Status:            executionStatePendingClose,
				LiveTrading:       true,
				AutoClose:         true,
				TargetCloseTimeMs: now.Add(4 * time.Minute).UnixMilli(),
			},
		},
	}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			{
				PlanKey:          "plan-retry-close",
				Symbol:           "BTC",
				LongExchange:     "longex",
				ShortExchange:    "shortex",
				LongVenueSymbol:  "BTCUSDT",
				ShortVenueSymbol: "BTCUSDT",
				LongQty:          1,
				ShortQty:         1,
				LongEntryPrice:   100,
				ShortEntryPrice:  100,
			},
		},
	}
	svc := newTestExecutionService(&testOrderRepo{}, nil)
	svc.execRepo = execRepo
	svc.planRepo = planRepo

	inspection, err := svc.InspectLiveAutoClose(context.Background())
	if err != nil {
		t.Fatalf("expected inspect to succeed, got %v", err)
	}
	if inspection.Total != 3 {
		t.Fatalf("expected 3 live auto-close candidates, got %d", inspection.Total)
	}
	if inspection.Eligible != 2 {
		t.Fatalf("expected 2 eligible candidates, got %d", inspection.Eligible)
	}
	if inspection.ShouldClose != 1 {
		t.Fatalf("expected 1 candidate to require close now, got %d", inspection.ShouldClose)
	}
	if inspection.AutoCloseDisabled != 1 {
		t.Fatalf("expected 1 auto-close-disabled candidate, got %d", inspection.AutoCloseDisabled)
	}

	var retryCandidate *AutoCloseCandidate
	var disabledCandidate *AutoCloseCandidate
	var pendingCandidate *AutoCloseCandidate
	for i := range inspection.Candidates {
		item := inspection.Candidates[i]
		switch item.Execution.PlanKey {
		case "plan-retry-close":
			retryCandidate = &item
		case "plan-disabled-auto-close":
			disabledCandidate = &item
		case "plan-pending-close":
			pendingCandidate = &item
		}
	}
	if retryCandidate == nil {
		t.Fatal("expected retry-close candidate to be present")
	}
	if !retryCandidate.Decision.ShouldClose || retryCandidate.Decision.Trigger != "auto_retry_close" {
		t.Fatalf("expected retry-close candidate to trigger auto_retry_close, got %+v", retryCandidate.Decision)
	}
	if disabledCandidate == nil || disabledCandidate.Decision.AutoCloseEnabled {
		t.Fatalf("expected disabled candidate to report auto_close=false, got %+v", disabledCandidate)
	}
	if pendingCandidate == nil || pendingCandidate.Decision.Eligible {
		t.Fatalf("expected pending_close candidate to be excluded from scan, got %+v", pendingCandidate)
	}
}

func TestSweepLiveAutoClose_ClosesRetryCloseCandidates(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:           "plan-retry-close",
				Symbol:            "BTC",
				Status:            executionStateCloseFailed,
				LiveTrading:       true,
				AutoClose:         true,
				TargetCloseTimeMs: now.Add(2 * time.Minute).UnixMilli(),
			},
		},
	}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			{
				PlanKey:          "plan-retry-close",
				Symbol:           "BTC",
				LongExchange:     "longex",
				ShortExchange:    "shortex",
				LongVenueSymbol:  "BTCUSDT",
				ShortVenueSymbol: "BTCUSDT",
				LongQty:          1,
				ShortQty:         1,
				LongEntryPrice:   100,
				ShortEntryPrice:  100,
				ExitMode:         "taker",
			},
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.execRepo = execRepo
	svc.planRepo = planRepo
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100, AskPrice: 100.1, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100.2, AskPrice: 100.3, EventTimeMs: now.UnixMilli()})

	report, err := svc.SweepLiveAutoClose(context.Background())
	if err != nil {
		t.Fatalf("expected auto-close sweep to succeed, got %v", err)
	}
	if report.Attempted != 1 || report.Closed != 1 || report.Failed != 0 {
		t.Fatalf("expected one successful close attempt, got attempted=%d closed=%d failed=%d", report.Attempted, report.Closed, report.Failed)
	}
	if len(longAdapter.closed) != 1 || len(shortAdapter.closed) != 1 {
		t.Fatalf("expected retry-close sweep to close both legs, got long=%d short=%d", len(longAdapter.closed), len(shortAdapter.closed))
	}
	if longAdapter.closed[0].Reason != "auto_retry_close" || shortAdapter.closed[0].Reason != "auto_retry_close" {
		t.Fatalf("expected auto_retry_close reason to propagate, got long=%s short=%s", longAdapter.closed[0].Reason, shortAdapter.closed[0].Reason)
	}
}

func TestCloseByPlanKey_UsesExistingLiveRecordWhenExecutionDisabled(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:     "plan-manual-close-live",
				Status:      executionStateOpened,
				LiveTrading: true,
			},
		},
	}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			{
				PlanKey:          "plan-manual-close-live",
				Symbol:           "BTC",
				LongExchange:     "longex",
				ShortExchange:    "shortex",
				LongVenueSymbol:  "BTCUSDT",
				ShortVenueSymbol: "BTCUSDT",
				LongQty:          1,
				ShortQty:         1,
				LongEntryPrice:   100,
				ShortEntryPrice:  100,
				ExitMode:         "taker",
			},
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.planRepo = planRepo
	svc.execRepo = execRepo
	svc.cfg.Execution.Enabled = false
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100, AskPrice: 100.1, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100.2, AskPrice: 100.3, EventTimeMs: now.UnixMilli()})

	rec, err := svc.CloseByPlanKey(context.Background(), "plan-manual-close-live")
	if err != nil {
		t.Fatalf("expected manual close to succeed, got %v", err)
	}
	if rec == nil || rec.Status != executionStateClosed {
		t.Fatalf("expected manual close to finish as closed, got %#v", rec)
	}
	if len(longAdapter.closed) != 1 || len(shortAdapter.closed) != 1 {
		t.Fatalf("expected manual close to stay live even when execution.enabled=false, got long=%d short=%d", len(longAdapter.closed), len(shortAdapter.closed))
	}
}

func TestRunAutoClose_RecoversOpenPartialFailedRecords(t *testing.T) {
	now := time.Now().UTC()
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			{
				PlanKey:          "plan-1",
				Symbol:           "BTC",
				LongExchange:     "longex",
				ShortExchange:    "shortex",
				LongVenueSymbol:  "BTCUSDT",
				ShortVenueSymbol: "BTCUSDT",
				LongQty:          1,
				ShortQty:         1,
				LongEntryPrice:   100,
				ShortEntryPrice:  100,
				ExitMode:         "taker",
			},
		},
	}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:           "plan-1",
				Status:            executionStateOpenPartial,
				LiveTrading:       true,
				AutoClose:         true,
				TargetCloseTimeMs: now.Add(10 * time.Minute).UnixMilli(),
			},
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.planRepo = planRepo
	svc.execRepo = execRepo
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100, AskPrice: 100.1, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100.2, AskPrice: 100.3, EventTimeMs: now.UnixMilli()})

	svc.runAutoClose(context.Background())

	if len(longAdapter.closed) != 1 || len(shortAdapter.closed) != 1 {
		t.Fatalf("expected recovery auto-close to execute on both legs, got long=%d short=%d", len(longAdapter.closed), len(shortAdapter.closed))
	}
	if longAdapter.closed[0].Reason != "auto_recovery" || shortAdapter.closed[0].Reason != "auto_recovery" {
		t.Fatalf("expected recovery trigger to propagate into close requests, got long=%s short=%s", longAdapter.closed[0].Reason, shortAdapter.closed[0].Reason)
	}
}

func TestRunAutoClose_IncludesActiveLiveRecordsOutsideLatestWindow(t *testing.T) {
	now := time.Now().UTC()
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			{
				PlanKey:           "plan-live-old",
				Symbol:            "BTC",
				LongExchange:      "longex",
				ShortExchange:     "shortex",
				LongVenueSymbol:   "BTCUSDT",
				ShortVenueSymbol:  "BTCUSDT",
				LongQty:           1,
				ShortQty:          1,
				LongEntryPrice:    100,
				ShortEntryPrice:   100,
				ExitMode:          "taker",
				TargetCloseTimeMs: now.Add(-time.Minute).UnixMilli(),
			},
		},
	}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:     "plan-newer-blocked",
				Status:      executionStateRiskBlocked,
				LiveTrading: false,
				AutoClose:   true,
			},
			{
				PlanKey:           "plan-live-old",
				Status:            executionStateOpened,
				LiveTrading:       true,
				AutoClose:         true,
				TargetCloseTimeMs: now.Add(-time.Minute).UnixMilli(),
			},
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true}
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.planRepo = planRepo
	svc.execRepo = execRepo
	svc.cfg.Execution.MaxLatestPlans = 1
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})

	svc.runAutoClose(context.Background())

	if len(longAdapter.closed) != 1 || len(shortAdapter.closed) != 1 {
		t.Fatalf("expected older live record outside latest window to still auto-close, got long=%d short=%d", len(longAdapter.closed), len(shortAdapter.closed))
	}
	if longAdapter.closed[0].Reason != "auto_schedule" || shortAdapter.closed[0].Reason != "auto_schedule" {
		t.Fatalf("expected schedule-triggered close reason, got long=%s short=%s", longAdapter.closed[0].Reason, shortAdapter.closed[0].Reason)
	}
	rec, err := svc.execRepo.FindByPlanKey(context.Background(), "plan-live-old")
	if err != nil {
		t.Fatalf("expected execution record lookup to succeed, got %v", err)
	}
	if rec == nil || rec.Status != executionStateClosed {
		t.Fatalf("expected older live record to transition to closed, got %+v", rec)
	}
}

func TestBuildTradeRequest_UsesAdapterCapabilities(t *testing.T) {
	adapter := &testTradeAdapter{
		name:    "hyperlike",
		enabled: true,
		caps: exchange.TradeCapabilities{
			MakerLimitTIF:          "ALO",
			TakerOrderType:         "LIMIT",
			TakerTimeInForce:       "IOC",
			TakerUsesAggressiveIOC: true,
		},
	}
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{"hyperlike": adapter})
	book := entity.BookTopSnapshot{Exchange: "hyperlike", Symbol: "BTC", BidPrice: 100, AskPrice: 101}
	plan := &entity.ExecutionPlan{PlanKey: "plan", Symbol: "BTC", EntryMode: "taker"}

	req := svc.buildTradeRequest(plan, "open", "long_leg", "BUY", entity.Symbol{Exchange: "hyperlike", VenueSymbol: "BTC", TickSize: "0.01", StepSize: "0.001"}, book, 1, 100)
	if req.OrderType != "LIMIT" {
		t.Fatalf("expected taker order type LIMIT, got %s", req.OrderType)
	}
	if req.TimeInForce != "IOC" {
		t.Fatalf("expected taker tif IOC, got %s", req.TimeInForce)
	}
	if req.Price != 101.21 {
		t.Fatalf("expected aggressive IOC price rounded up to tick, got %f", req.Price)
	}
}

func TestBuildTradeRequest_RoundsSellLimitPriceDownToTick(t *testing.T) {
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{})
	book := entity.BookTopSnapshot{Exchange: "venue", Symbol: "BTC", BidPrice: 100.03, AskPrice: 100.07}
	plan := &entity.ExecutionPlan{PlanKey: "plan", Symbol: "BTC", ExitMode: "mixed"}

	req := svc.buildTradeRequest(plan, "close", "long_leg", "SELL", entity.Symbol{Exchange: "venue", VenueSymbol: "BTCUSDT", TickSize: "0.01", StepSize: "0.001"}, book, 1, 100)
	if req.OrderType != "LIMIT" || req.TimeInForce != "IOC" {
		t.Fatalf("expected mixed close to use LIMIT IOC, got type=%s tif=%s", req.OrderType, req.TimeInForce)
	}
	if req.Price != 99.82 {
		t.Fatalf("expected sell price rounded down to tick, got %f", req.Price)
	}
}

func TestShouldShortCircuitExecutionAction(t *testing.T) {
	if !shouldShortCircuitExecutionAction(executionStateOpened, "open") {
		t.Fatal("expected opened status to short-circuit repeated open")
	}
	if !shouldShortCircuitExecutionAction("dry_run_closed", "close") {
		t.Fatal("expected dry_run_closed status to short-circuit repeated close")
	}
	if shouldShortCircuitExecutionAction(executionStatePendingOpen, "open") {
		t.Fatal("expected pending_open to remain actionable for open recovery")
	}
	if shouldShortCircuitExecutionAction(executionStateOpenPartial, "close") {
		t.Fatal("expected open_partial_failed not to short-circuit close helper by itself")
	}
}

func TestSummarizeExecutionStatusWithPolicy(t *testing.T) {
	openResults := []entity.OrderRecord{
		{Phase: "open", Status: "FILLED", ExecutedQty: 1},
		{Phase: "open", Status: "FILLED", ExecutedQty: 1},
	}
	if got := summarizeExecutionStatusWithPolicy(openResults, openExecutionPolicy); got != executionStateOpened {
		t.Fatalf("expected opened status, got %s", got)
	}

	hedgingResults := []entity.OrderRecord{
		{Phase: "open", Status: "FILLED", ExecutedQty: 1},
		{Phase: "open", Status: "ERROR", ErrorMessage: "boom"},
		{Phase: "hedge_close", Status: "FILLED", ExecutedQty: 1},
	}
	if got := summarizeExecutionStatusWithPolicy(hedgingResults, openExecutionPolicy); got != executionStateOpenHedging {
		t.Fatalf("expected open_hedging status, got %s", got)
	}
}

func TestValidateExecutionAction(t *testing.T) {
	if err := validateExecutionAction(false, "", openExecutionPolicy); err != nil {
		t.Fatalf("expected open without existing record to be allowed, got %v", err)
	}
	if err := validateExecutionAction(false, "", closeExecutionPolicy); err == nil {
		t.Fatal("expected close without existing record to be rejected")
	}
	if err := validateExecutionAction(true, executionStateOpened, closeExecutionPolicy); err != nil {
		t.Fatalf("expected close from opened to be allowed, got %v", err)
	}
	if err := validateExecutionAction(true, executionStateOpenPartial, closeExecutionPolicy); err != nil {
		t.Fatalf("expected close from open_partial_failed to be allowed for recovery, got %v", err)
	}
	if err := validateExecutionAction(true, executionStateOpenPartial, openExecutionPolicy); err == nil {
		t.Fatal("expected repeated open from open_partial_failed to be rejected")
	}
}

func TestValidateExecutionTransition(t *testing.T) {
	if err := validateExecutionTransition("", executionStatePendingOpen, openExecutionTransitions); err != nil {
		t.Fatalf("expected initial -> pending_open transition to be allowed, got %v", err)
	}
	if err := validateExecutionTransition(executionStateOpened, executionStatePendingOpen, openExecutionTransitions); err == nil {
		t.Fatal("expected opened -> pending_open transition to be rejected")
	}
	if err := validateExecutionTransition(executionStatePendingClose, executionStateClosed, closeExecutionTransitions); err != nil {
		t.Fatalf("expected pending_close -> closed transition to be allowed, got %v", err)
	}
}

func TestApplyExecutionTransition(t *testing.T) {
	rec := &entity.ExecutionRecord{}
	if err := applyExecutionTransition(rec, executionStatePendingOpen, openExecutionTransitions); err != nil {
		t.Fatalf("expected apply transition to succeed, got %v", err)
	}
	if rec.Status != executionStatePendingOpen {
		t.Fatalf("expected status pending_open, got %s", rec.Status)
	}
	if err := applyExecutionTransition(rec, executionStateClosed, openExecutionTransitions); err == nil {
		t.Fatal("expected invalid transition to be rejected")
	}
}

func TestApplyExecutionEvent_PersistsTransitionMetadata(t *testing.T) {
	rec := &entity.ExecutionRecord{}
	err := applyExecutionEvent(rec, executionEvent{
		Name:         "open_requested",
		TargetStatus: executionStatePendingOpen,
		Reason:       "test pending transition",
		OccurredAtMs: 123456789,
	}, openExecutionTransitions)
	if err != nil {
		t.Fatalf("expected applyExecutionEvent to succeed, got %v", err)
	}
	if rec.Status != executionStatePendingOpen {
		t.Fatalf("expected pending_open status, got %s", rec.Status)
	}
	if rec.LastTransitionEvent != "open_requested" {
		t.Fatalf("expected last transition event open_requested, got %s", rec.LastTransitionEvent)
	}
	if rec.StatusReason != "test pending transition" {
		t.Fatalf("expected status reason to be stored, got %s", rec.StatusReason)
	}
	if rec.LastTransitionAtMs != 123456789 {
		t.Fatalf("expected explicit transition timestamp to be stored, got %d", rec.LastTransitionAtMs)
	}
}

func TestExecutionErrorStatus(t *testing.T) {
	err := fmt.Errorf("%s: binance until 2026-01-01T00:00:00Z", executionStateCircuitOpen)
	if got := executionErrorStatus(err, executionStateRiskBlocked); got != executionStateCircuitOpen {
		t.Fatalf("expected circuit-open classification, got %s", got)
	}
	if got := executionErrorStatus(errors.New("low balance"), executionStateRiskBlocked); got != executionStateRiskBlocked {
		t.Fatalf("expected fallback risk-blocked classification, got %s", got)
	}
}

func TestExecutionErrorEvent(t *testing.T) {
	err := fmt.Errorf("%s: binance until 2026-01-01T00:00:00Z", executionStateCircuitOpen)
	if got := executionErrorEvent("open", err); got != "open_circuit_blocked" {
		t.Fatalf("expected open_circuit_blocked event, got %s", got)
	}
	if got := executionErrorEvent("open", errors.New("low balance")); got != "open_risk_blocked" {
		t.Fatalf("expected open_risk_blocked event, got %s", got)
	}
}

func TestClosePlan_RejectsWhenNoExecutionRecordExists(t *testing.T) {
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{})
	svc.execRepo = execRepo

	plan := &entity.ExecutionPlan{PlanKey: "plan-close-no-rec", Symbol: "BTC", LongExchange: "longex", ShortExchange: "shortex"}
	rec, err := svc.closePlan(context.Background(), plan, "manual", false)
	if err == nil {
		t.Fatal("expected closePlan without execution record to fail")
	}
	if rec == nil {
		t.Fatal("expected failure path to still return execution record")
	}
	if rec.LastError == "" {
		t.Fatal("expected failure reason to be stored on execution record")
	}
}

func TestOpenPlan_RejectsUnsafeRetryState(t *testing.T) {
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey: "plan-open-partial",
				Status:  executionStateOpenPartial,
			},
		},
	}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{})
	svc.execRepo = execRepo

	plan := &entity.ExecutionPlan{PlanKey: "plan-open-partial", Symbol: "BTC", LongExchange: "longex", ShortExchange: "shortex"}
	rec, err := svc.openPlan(context.Background(), plan, "manual", false)
	if err == nil {
		t.Fatal("expected openPlan from open_partial_failed to be rejected")
	}
	if rec == nil {
		t.Fatal("expected existing execution record to be returned")
	}
	if rec.LastError == "" {
		t.Fatal("expected rejection reason to be persisted on record")
	}
}

func TestOpenPlan_CircuitOpenRiskControlSetsCircuitStatus(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{}
	adapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 500}}
	other := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 500}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": adapter, "shortex": other})
	svc.execRepo = execRepo
	svc.exchangeFailure["longex"] = exchangeFailureState{openUntil: time.Now().Add(time.Minute)}
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 99, AskPrice: 100, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100.02, AskPrice: 100.03, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "longex",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          -0.0200,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})
	svc.store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "shortex",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          0.0200,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})

	plan := &entity.ExecutionPlan{
		PlanKey:          "plan-circuit-open",
		Symbol:           "BTC",
		LongExchange:     "longex",
		ShortExchange:    "shortex",
		LongVenueSymbol:  "BTC",
		ShortVenueSymbol: "BTC",
		LongQty:          1,
		ShortQty:         1,
		LongEntryPrice:   100,
		ShortEntryPrice:  100,
	}

	rec, err := svc.openPlan(context.Background(), plan, "manual", true)
	if err == nil {
		t.Fatal("expected openPlan to fail when exchange circuit is open")
	}
	if rec == nil {
		t.Fatal("expected execution record to be returned")
	}
	if rec.Status != executionStateCircuitOpen {
		t.Fatalf("expected execution status api_circuit_open, got %s", rec.Status)
	}
	if rec.LastTransitionEvent != "open_circuit_blocked" {
		t.Fatalf("expected transition event open_circuit_blocked, got %s", rec.LastTransitionEvent)
	}
	if rec.StatusReason == "" {
		t.Fatal("expected status reason to be captured on record")
	}
}

func TestOpenPlan_DryRunWritesTransitionMetadata(t *testing.T) {
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{})
	svc.execRepo = execRepo

	plan := &entity.ExecutionPlan{PlanKey: "plan-dry-open", Symbol: "BTC", LongExchange: "longex", ShortExchange: "shortex"}
	rec, err := svc.openPlan(context.Background(), plan, "manual", false)
	if err != nil {
		t.Fatalf("expected dry-run open to succeed, got %v", err)
	}
	if rec.Status != "dry_run_opened" {
		t.Fatalf("expected dry_run_opened status, got %s", rec.Status)
	}
	if rec.LastTransitionEvent != "open_dry_run_completed" {
		t.Fatalf("expected dry-run transition event, got %s", rec.LastTransitionEvent)
	}
	if rec.StatusReason == "" {
		t.Fatal("expected dry-run status reason to be populated")
	}
}

func TestMergeOrderRecordFromExternalEvent(t *testing.T) {
	order := &entity.OrderRecord{
		Exchange:      "binance",
		ClientOrderID: "client-1",
		Status:        "NEW",
		ExecutedQty:   0.2,
		AvgPrice:      100,
	}
	mergeOrderRecordFromExternalEvent(order, ExternalOrderEvent{
		Exchange:      "binance",
		ClientOrderID: "client-1",
		Status:        "FILLED",
		ExecutedQty:   1,
		AveragePrice:  101,
		RawPayload:    "{\"status\":\"FILLED\"}",
	})
	if order.Status != "FILLED" {
		t.Fatalf("expected FILLED status, got %s", order.Status)
	}
	if order.ExecutedQty != 1 {
		t.Fatalf("expected executed qty 1, got %f", order.ExecutedQty)
	}
	if order.AvgPrice != 101 {
		t.Fatalf("expected avg price 101, got %f", order.AvgPrice)
	}
}

func TestApplyExternalOrderEvent_UpdatesExecutionFromPendingOpenToOpened(t *testing.T) {
	orderRepo := &testOrderRepo{
		items: []entity.OrderRecord{
			{
				ID:              1,
				PlanKey:         "plan-ext-open",
				ExecutionStatus: "open",
				Phase:           "open",
				LegRole:         "long_leg",
				Exchange:        "longex",
				Symbol:          "BTC",
				ClientOrderID:   "long-1",
				Status:          "FILLED",
				ExecutedQty:     1,
			},
			{
				ID:              2,
				PlanKey:         "plan-ext-open",
				ExecutionStatus: "open",
				Phase:           "open",
				LegRole:         "short_leg",
				Exchange:        "shortex",
				Symbol:          "BTC",
				ClientOrderID:   "short-1",
				Status:          "NEW",
				ExecutedQty:     0,
			},
		},
	}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey: "plan-ext-open",
				Status:  executionStatePendingOpen,
			},
		},
	}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{})
	svc.execRepo = execRepo

	rec, err := svc.ApplyExternalOrderEvent(context.Background(), ExternalOrderEvent{
		Source:        "user_stream",
		Exchange:      "shortex",
		ClientOrderID: "short-1",
		Status:        "FILLED",
		ExecutedQty:   1,
		AveragePrice:  100,
		Terminal:      true,
		OccurredAtMs:  999001,
	})
	if err != nil {
		t.Fatalf("expected external order event apply to succeed, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected execution record to be returned")
	}
	if rec.Status != executionStateOpened {
		t.Fatalf("expected execution to become opened, got %s", rec.Status)
	}
	if rec.LastTransitionEvent != "user_stream_order_filled" {
		t.Fatalf("expected user_stream_order_filled event, got %s", rec.LastTransitionEvent)
	}
	if rec.LastTransitionAtMs != 999001 {
		t.Fatalf("expected transition timestamp to come from event, got %d", rec.LastTransitionAtMs)
	}
}

func TestApplyExternalOrderEvent_UnknownOrderReturnsError(t *testing.T) {
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{})
	svc.execRepo = &testExecRepo{}

	_, err := svc.ApplyExternalOrderEvent(context.Background(), ExternalOrderEvent{
		Exchange:      "binance",
		ClientOrderID: "missing",
		Status:        "FILLED",
	})
	if err == nil {
		t.Fatal("expected unknown external order event to fail")
	}
}

func TestApplyExternalOrderEvent_UpdatesTimedOutOrderDuringRecovery(t *testing.T) {
	orderRepo := &testOrderRepo{
		items: []entity.OrderRecord{
			{
				ID:              1,
				PlanKey:         "plan-timeout-recovery",
				ExecutionStatus: "open",
				Phase:           "open",
				LegRole:         "long_leg",
				Exchange:        "longex",
				Symbol:          "BTC",
				ClientOrderID:   "long-1",
				Status:          "FILLED",
				ExecutedQty:     1,
			},
			{
				ID:              2,
				PlanKey:         "plan-timeout-recovery",
				ExecutionStatus: "open",
				Phase:           "open",
				LegRole:         "short_leg",
				Exchange:        "shortex",
				Symbol:          "BTC",
				ClientOrderID:   "short-timeout",
				Status:          "TIMEOUT",
				ExecutedQty:     0,
				ErrorMessage:    "context deadline exceeded",
			},
			{
				ID:              3,
				PlanKey:         "plan-timeout-recovery",
				ExecutionStatus: "hedge_close",
				Phase:           "hedge_close",
				LegRole:         "long_leg",
				Exchange:        "longex",
				Symbol:          "BTC",
				ClientOrderID:   "hedge-1",
				Status:          "NEW",
				ExecutedQty:     0,
			},
		},
	}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey: "plan-timeout-recovery",
				Status:  executionStateOpenHedging,
			},
		},
	}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{})
	svc.execRepo = execRepo

	rec, err := svc.ApplyExternalOrderEvent(context.Background(), ExternalOrderEvent{
		Source:        "bybit_private_stream",
		Exchange:      "shortex",
		ClientOrderID: "short-timeout",
		Status:        "FILLED",
		ExecutedQty:   1,
		AveragePrice:  100.5,
		Terminal:      true,
		OccurredAtMs:  999101,
	})
	if err != nil {
		t.Fatalf("expected timeout recovery event apply to succeed, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected execution record to be returned")
	}
	if rec.Status != executionStateOpenHedging {
		t.Fatalf("expected execution to stay in recovery status, got %s", rec.Status)
	}
	order, err := orderRepo.FindByExternalOrderID(context.Background(), "shortex", "short-timeout", "")
	if err != nil {
		t.Fatalf("expected order lookup to succeed, got %v", err)
	}
	if order == nil {
		t.Fatal("expected timed out order record to exist")
	}
	if order.Status != "FILLED" || order.ExecutedQty != 1 {
		t.Fatalf("expected timed out order to be corrected by async event, got %#v", order)
	}
}

func TestOrderEventLoop_ConsumesStreamerEvents(t *testing.T) {
	orderRepo := &testOrderRepo{
		items: []entity.OrderRecord{
			{
				ID:              1,
				PlanKey:         "plan-stream-open",
				ExecutionStatus: "open",
				Phase:           "open",
				LegRole:         "long_leg",
				Exchange:        "longex",
				Symbol:          "BTC",
				ClientOrderID:   "long-evt",
				Status:          "FILLED",
				ExecutedQty:     1,
			},
			{
				ID:              2,
				PlanKey:         "plan-stream-open",
				ExecutionStatus: "open",
				Phase:           "open",
				LegRole:         "short_leg",
				Exchange:        "shortex",
				Symbol:          "BTC",
				ClientOrderID:   "short-evt",
				Status:          "NEW",
				ExecutedQty:     0,
			},
		},
	}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey: "plan-stream-open",
				Status:  executionStatePendingOpen,
			},
		},
	}

	streamer := &testTradeAdapter{
		name:    "shortex",
		enabled: true,
		streamEvents: []exchange.OrderEvent{
			{
				Source:        "user_stream",
				Exchange:      "shortex",
				ClientOrderID: "short-evt",
				Status:        "FILLED",
				ExecutedQty:   1,
				AveragePrice:  100,
				Terminal:      true,
				OccurredAtMs:  777001,
			},
		},
	}

	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{
		"shortex": streamer,
	})
	svc.execRepo = execRepo

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.orderEventLoop(ctx)
	svc.startTradeOrderStreams(ctx)
	time.Sleep(20 * time.Millisecond)

	rec, err := svc.execRepo.FindByPlanKey(context.Background(), "plan-stream-open")
	if err != nil {
		t.Fatalf("expected execution record lookup to succeed, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected execution record to exist")
	}
	if rec.Status != executionStateOpened {
		t.Fatalf("expected execution status opened after streamed event, got %s", rec.Status)
	}
	if rec.LastTransitionEvent != "user_stream_order_filled" {
		t.Fatalf("expected streamed transition event, got %s", rec.LastTransitionEvent)
	}
}

func TestOpenPlan_RevalidatesCurrentOpportunityBeforeLiveOpen(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{
		"longex":  longAdapter,
		"shortex": shortAdapter,
	})
	svc.execRepo = execRepo

	// 这里故意给两条腿准备“当前 carry 已翻负”的 funding 快照。
	// 这样可以验证：即便这条 plan 曾经是 ready 的，只要发单前市场事实已经变了，
	// ExecutionService 也必须在真正调 PlaceOrder 之前把它挡住。
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 99, AskPrice: 100, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100, AskPrice: 101, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "longex",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          0.0020,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})
	svc.store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "shortex",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          0.0010,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})

	plan := &entity.ExecutionPlan{
		PlanKey:          "plan-open-revalidate",
		Symbol:           "BTC",
		LongExchange:     "longex",
		ShortExchange:    "shortex",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
		LongQty:          1,
		ShortQty:         1,
		LongEntryPrice:   100,
		ShortEntryPrice:  100,
		EntryMode:        "taker",
		ExitMode:         "taker",
	}

	rec, err := svc.openPlan(context.Background(), plan, "manual", true)
	if err == nil {
		t.Fatal("expected live open to fail when current opportunity no longer passes revalidation")
	}
	if rec == nil {
		t.Fatal("expected execution record to be returned on revalidation failure")
	}
	if rec.Status != executionStateRiskBlocked {
		t.Fatalf("expected risk_blocked status, got %s", rec.Status)
	}
	if rec.LastTransitionEvent != "open_revalidation_failed" {
		t.Fatalf("expected open_revalidation_failed event, got %s", rec.LastTransitionEvent)
	}
	if len(longAdapter.placed) != 0 || len(shortAdapter.placed) != 0 {
		t.Fatalf("expected revalidation failure to block all live orders, got long=%d short=%d", len(longAdapter.placed), len(shortAdapter.placed))
	}
}

func TestRunAutoOpen_RespectsMaxAutoOpenPerLoop(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			readyPlan("plan-auto-1", "BTC", "longex", "shortex", 100, 16, 1600),
			readyPlan("plan-auto-2", "ETH", "longex", "shortex", 100, 16, 1600),
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 2000, AvailableBalance: 1500}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 2000, AvailableBalance: 1500}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.execRepo = execRepo
	svc.planRepo = planRepo
	svc.cfg.Execution.Enabled = true
	svc.cfg.Execution.MaxAutoOpenPerLoop = 1
	svc.cfg.MinNetPNL = 0.1
	seedAutoOpenMarket(svc.store, now, "BTC", "longex", "shortex", 100)
	seedAutoOpenMarket(svc.store, now, "ETH", "longex", "shortex", 100)

	svc.runAutoOpen(context.Background())

	if len(orderRepo.items) != 2 {
		t.Fatalf("expected exactly one plan to place two primary-leg orders, got %d order records", len(orderRepo.items))
	}
	if len(execRepo.items) != 1 {
		t.Fatalf("expected exactly one execution record, got %d", len(execRepo.items))
	}
	if execRepo.items[0].PlanKey != "plan-auto-1" {
		t.Fatalf("expected first ready plan to open first, got %s", execRepo.items[0].PlanKey)
	}
}

func TestRunAutoOpen_AutoAllocatesRemainingBudgetAcrossCandidates(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:               "plan-existing",
				Status:                executionStateOpened,
				LiveTrading:           true,
				AllocatedNotionalUSDT: 800,
			},
		},
	}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			readyPlan("plan-auto-btc", "BTC", "longex", "shortex", 100, 16, 1600),
			readyPlan("plan-auto-eth", "ETH", "longex", "shortex", 100, 16, 1600),
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 2000, AvailableBalance: 1500}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 2000, AvailableBalance: 1500}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.execRepo = execRepo
	svc.planRepo = planRepo
	svc.cfg.Execution.Enabled = true
	svc.cfg.Execution.AutoAllocateCapital = true
	svc.cfg.Execution.MaxAutoOpenPerLoop = 2
	svc.cfg.Execution.MaxLivePlans = 3
	svc.cfg.MinNetPNL = 0.1
	seedAutoOpenMarket(svc.store, now, "BTC", "longex", "shortex", 100)
	seedAutoOpenMarket(svc.store, now, "ETH", "longex", "shortex", 100)

	svc.runAutoOpen(context.Background())

	if len(execRepo.items) != 3 {
		t.Fatalf("expected existing record plus two new live executions, got %d", len(execRepo.items))
	}
	for _, planKey := range []string{"plan-auto-btc", "plan-auto-eth"} {
		rec, err := execRepo.FindByPlanKey(context.Background(), planKey)
		if err != nil {
			t.Fatalf("expected execution lookup to succeed for %s, got %v", planKey, err)
		}
		if rec == nil {
			t.Fatalf("expected execution record for %s", planKey)
		}
		if rec.AllocatedNotionalUSDT != 400 {
			t.Fatalf("expected %s allocated notional 400, got %.2f", planKey, rec.AllocatedNotionalUSDT)
		}
	}
	if len(longAdapter.placed) != 2 || len(shortAdapter.placed) != 2 {
		t.Fatalf("expected two live open requests per side, got long=%d short=%d", len(longAdapter.placed), len(shortAdapter.placed))
	}
	for _, req := range append(append([]exchange.TradeOrderRequest(nil), longAdapter.placed...), shortAdapter.placed...) {
		if req.Quantity != 4 {
			t.Fatalf("expected auto-allocated quantity 4, got %.6f", req.Quantity)
		}
	}
}

func TestRunAutoOpen_RespectsMaxLivePlans(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:               "plan-live-existing",
				Status:                executionStateOpened,
				LiveTrading:           true,
				AllocatedNotionalUSDT: 800,
			},
		},
	}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			readyPlan("plan-live-1", "BTC", "longex", "shortex", 100, 16, 1600),
			readyPlan("plan-live-2", "ETH", "longex", "shortex", 100, 16, 1600),
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 2000, AvailableBalance: 1500}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 2000, AvailableBalance: 1500}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.execRepo = execRepo
	svc.planRepo = planRepo
	svc.cfg.Execution.Enabled = true
	svc.cfg.Execution.MaxLivePlans = 2
	svc.cfg.MinNetPNL = 0.1
	seedAutoOpenMarket(svc.store, now, "BTC", "longex", "shortex", 100)
	seedAutoOpenMarket(svc.store, now, "ETH", "longex", "shortex", 100)

	svc.runAutoOpen(context.Background())

	if len(orderRepo.items) != 2 {
		t.Fatalf("expected only one additional plan to open under max_live_plans, got %d order records", len(orderRepo.items))
	}
	rec, err := execRepo.FindByPlanKey(context.Background(), "plan-live-1")
	if err != nil {
		t.Fatalf("expected execution lookup to succeed, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected first candidate to open within remaining live slot")
	}
	if rec, err = execRepo.FindByPlanKey(context.Background(), "plan-live-2"); err != nil {
		t.Fatalf("expected second execution lookup to succeed, got %v", err)
	} else if rec != nil {
		t.Fatalf("expected second candidate to remain unopened after reaching max_live_plans, got %#v", rec)
	}
}

func TestOpenPlan_ReturnsInFlightErrorWhenClaimAlreadyHeld(t *testing.T) {
	now := time.Now().UTC()
	release := make(chan struct{})
	started := make(chan string, 2)
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{}
	longAdapter := &testTradeAdapter{
		name:         "longex",
		enabled:      true,
		account:      exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus:  exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
		placeStarted: started,
		placeBlock:   release,
	}
	shortAdapter := &testTradeAdapter{
		name:         "shortex",
		enabled:      true,
		account:      exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 600},
		orderStatus:  exchange.OrderStatus{Status: "FILLED", ExecutedQty: 1, Terminal: true},
		placeStarted: started,
		placeBlock:   release,
	}

	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{"longex": longAdapter, "shortex": shortAdapter})
	svc.execRepo = execRepo
	svc.cfg.MinNetPNL = 0.1
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10", FundingIntervalHours: 8})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 99.9, AskPrice: 100, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 100.1, AskPrice: 100.2, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "longex",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          -0.0020,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})
	svc.store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "shortex",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          0.0020,
		FundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})

	plan := &entity.ExecutionPlan{
		PlanKey:          "plan-claim-guard",
		Symbol:           "BTC",
		LongExchange:     "longex",
		ShortExchange:    "shortex",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
		LongQty:          1,
		ShortQty:         1,
		LongEntryPrice:   100,
		ShortEntryPrice:  100.1,
		EntryMode:        "taker",
		ExitMode:         "taker",
	}

	firstErrCh := make(chan error, 1)
	go func() {
		_, err := svc.openPlan(context.Background(), plan, "manual", true)
		firstErrCh <- err
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected first openPlan call to reach primary leg placement")
		}
	}

	rec, err := svc.openPlan(context.Background(), plan, "manual", true)
	if !errors.Is(err, ErrExecutionActionInFlight) {
		t.Fatalf("expected ErrExecutionActionInFlight, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected second openPlan call to return current execution record")
	}
	if rec.Status != executionStatePendingOpen {
		t.Fatalf("expected in-flight record to stay pending_open, got %s", rec.Status)
	}

	close(release)

	if err := <-firstErrCh; err != nil {
		t.Fatalf("expected first openPlan call to finish successfully, got %v", err)
	}
	if len(longAdapter.placed) != 1 || len(shortAdapter.placed) != 1 {
		t.Fatalf("expected only one live order per leg, got long=%d short=%d", len(longAdapter.placed), len(shortAdapter.placed))
	}
}

func TestOpenByPlanKey_MissingPlanReturnsNotFound(t *testing.T) {
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{})
	svc.planRepo = &testPlanRepo{}
	svc.execRepo = &testExecRepo{}

	rec, err := svc.OpenByPlanKey(context.Background(), "missing-plan")
	if !errors.Is(err, ErrExecutionPlanNotFound) {
		t.Fatalf("expected ErrExecutionPlanNotFound, got %v", err)
	}
	if rec != nil {
		t.Fatalf("expected no execution record for missing plan, got %#v", rec)
	}
}

func TestCloseByPlanKey_MissingPlanReturnsNotFound(t *testing.T) {
	svc := newTestExecutionService(&testOrderRepo{}, map[string]exchange.TradeAdapter{})
	svc.planRepo = &testPlanRepo{}
	svc.execRepo = &testExecRepo{}

	rec, err := svc.CloseByPlanKey(context.Background(), "missing-plan")
	if !errors.Is(err, ErrExecutionPlanNotFound) {
		t.Fatalf("expected ErrExecutionPlanNotFound, got %v", err)
	}
	if rec != nil {
		t.Fatalf("expected no execution record for missing plan, got %#v", rec)
	}
}

func TestRunAutoClose_TriggersDrawdownGuardForLivePositions(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &testOrderRepo{}
	execRepo := &testExecRepo{
		items: []entity.ExecutionRecord{
			{
				PlanKey:           "plan-drawdown-guard",
				Status:            executionStateOpened,
				LiveTrading:       true,
				AutoClose:         true,
				TargetCloseTimeMs: now.Add(10 * time.Minute).UnixMilli(),
			},
		},
	}
	planRepo := &testPlanRepo{
		items: []entity.ExecutionPlan{
			{
				PlanKey:          "plan-drawdown-guard",
				Symbol:           "BTC",
				LongExchange:     "longex",
				ShortExchange:    "shortex",
				LongVenueSymbol:  "BTCUSDT",
				ShortVenueSymbol: "BTCUSDT",
				LongQty:          1,
				ShortQty:         1,
				LongEntryPrice:   100,
				ShortEntryPrice:  100,
				ExitMode:         "taker",
			},
		},
	}
	longAdapter := &testTradeAdapter{name: "longex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 500}}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, account: exchange.AccountSnapshot{Equity: 1000, AvailableBalance: 500}}
	svc := newTestExecutionService(orderRepo, map[string]exchange.TradeAdapter{
		"longex":  longAdapter,
		"shortex": shortAdapter,
	})
	svc.execRepo = execRepo
	svc.planRepo = planRepo
	svc.cfg.Execution.Enabled = false
	svc.cfg.Execution.MaxUnrealizedLossUSDT = 5
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", StepSize: "0.001", MinNotional: "10"})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 90, AskPrice: 91, EventTimeMs: now.UnixMilli()})
	svc.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", VenueSymbol: "BTCUSDT", BidPrice: 109, AskPrice: 110, EventTimeMs: now.UnixMilli()})

	svc.runAutoClose(context.Background())

	if len(longAdapter.closed) != 1 || len(shortAdapter.closed) != 1 {
		t.Fatalf("expected drawdown guard to trigger live close on both legs, got long=%d short=%d", len(longAdapter.closed), len(shortAdapter.closed))
	}
	if longAdapter.closed[0].Reason != "auto_drawdown_guard" {
		t.Fatalf("expected long-leg close reason auto_drawdown_guard, got %s", longAdapter.closed[0].Reason)
	}
	if shortAdapter.closed[0].Reason != "auto_drawdown_guard" {
		t.Fatalf("expected short-leg close reason auto_drawdown_guard, got %s", shortAdapter.closed[0].Reason)
	}
	rec, err := svc.execRepo.FindByPlanKey(context.Background(), "plan-drawdown-guard")
	if err != nil {
		t.Fatalf("expected execution record lookup to succeed, got %v", err)
	}
	if rec == nil {
		t.Fatal("expected execution record to exist after auto close")
	}
	if rec.Status != executionStateClosed {
		t.Fatalf("expected drawdown-triggered auto close to finish as closed, got %s", rec.Status)
	}
	if rec.ClosedAtMs == 0 {
		t.Fatal("expected close timestamp to be recorded")
	}
}
