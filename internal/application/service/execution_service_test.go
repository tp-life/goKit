package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

type testOrderRepo struct {
	items []entity.OrderRecord
}

func (r *testOrderRepo) Create(_ context.Context, item *entity.OrderRecord) error {
	r.items = append(r.items, *item)
	return nil
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
	name      string
	enabled   bool
	placeErr  error
	placed    []exchange.TradeOrderRequest
	closed    []exchange.TradeOrderRequest
	closeResp exchange.TradeOrderResult
}

func (a *testTradeAdapter) Name() string  { return a.name }
func (a *testTradeAdapter) Enabled() bool { return a.enabled }
func (a *testTradeAdapter) PlaceOrder(_ context.Context, req exchange.TradeOrderRequest) (exchange.TradeOrderResult, error) {
	a.placed = append(a.placed, req)
	if a.placeErr != nil {
		return exchange.TradeOrderResult{}, a.placeErr
	}
	return exchange.TradeOrderResult{Status: "FILLED", ClientOrderID: req.ClientOrderID}, nil
}
func (a *testTradeAdapter) ClosePosition(_ context.Context, req exchange.TradeOrderRequest) (exchange.TradeOrderResult, error) {
	a.closed = append(a.closed, req)
	if a.closeResp.Status == "" {
		a.closeResp.Status = "FILLED"
	}
	if a.closeResp.ClientOrderID == "" {
		a.closeResp.ClientOrderID = req.ClientOrderID
	}
	return a.closeResp, nil
}
func (a *testTradeAdapter) GetPosition(_ context.Context, _, _, _ string) (exchange.Position, error) {
	return exchange.Position{}, nil
}

func TestPlacePlanOrders_OpenPartialFailureTriggersHedgeClose(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "longex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertSymbol(entity.Symbol{Exchange: "shortex", Symbol: "BTCUSDT", VenueSymbol: "BTCUSDT", StepSize: "0.001"})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTCUSDT", BidPrice: 100, AskPrice: 101})
	store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTCUSDT", BidPrice: 102, AskPrice: 103})

	longAdapter := &testTradeAdapter{name: "longex", enabled: true}
	shortAdapter := &testTradeAdapter{name: "shortex", enabled: true, placeErr: errors.New("short leg failed")}
	orderRepo := &testOrderRepo{}

	svc := &ExecutionService{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:     store,
		orderRepo: orderRepo,
		trades: map[string]exchange.TradeAdapter{
			"longex":  longAdapter,
			"shortex": shortAdapter,
		},
	}
	plan := &entity.ExecutionPlan{
		PlanKey:          "aaaaaaaaaabbbbbbbbbbccccccccccdddddddddd",
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
	if hedgeReq.OrderType != "MARKET" {
		t.Fatalf("expected hedge order type MARKET, got %s", hedgeReq.OrderType)
	}
	if hedgeReq.ClientOrderID == longAdapter.placed[0].ClientOrderID {
		t.Fatalf("expected hedge client order id to differ from open order id")
	}

	hedgeRecord := results[2]
	if hedgeRecord.Phase != "hedge_close" {
		t.Fatalf("expected hedge phase record, got %s", hedgeRecord.Phase)
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
