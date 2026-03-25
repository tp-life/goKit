package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"goKit/internal/infrastructure/exchange"
)

type fakeTradeAdapter struct {
	statuses []exchange.OrderStatus
	errs     []error
	calls    int
}

func (f *fakeTradeAdapter) Name() string  { return "fake" }
func (f *fakeTradeAdapter) Enabled() bool { return true }
func (f *fakeTradeAdapter) Capabilities() exchange.TradeCapabilities {
	return exchange.TradeCapabilities{}
}
func (f *fakeTradeAdapter) PlaceOrder(context.Context, exchange.TradeOrderRequest) (exchange.TradeOrderResult, error) {
	return exchange.TradeOrderResult{}, nil
}
func (f *fakeTradeAdapter) ClosePosition(context.Context, exchange.TradeOrderRequest) (exchange.TradeOrderResult, error) {
	return exchange.TradeOrderResult{}, nil
}
func (f *fakeTradeAdapter) GetPosition(context.Context, string, string, string) (exchange.Position, error) {
	return exchange.Position{}, nil
}
func (f *fakeTradeAdapter) GetAccountSnapshot(context.Context) (exchange.AccountSnapshot, error) {
	return exchange.AccountSnapshot{}, nil
}
func (f *fakeTradeAdapter) GetOrderStatus(context.Context, exchange.OrderLookupRequest) (exchange.OrderStatus, error) {
	idx := f.calls
	f.calls++
	if idx < len(f.errs) && f.errs[idx] != nil {
		return exchange.OrderStatus{}, f.errs[idx]
	}
	if idx < len(f.statuses) {
		return f.statuses[idx], nil
	}
	if len(f.statuses) == 0 {
		return exchange.OrderStatus{}, errors.New("no status configured")
	}
	return f.statuses[len(f.statuses)-1], nil
}

func TestAwaitOrderStatusReturnsTerminalStatus(t *testing.T) {
	adapter := &fakeTradeAdapter{
		statuses: []exchange.OrderStatus{
			{Status: "NEW", Terminal: false},
			{Status: "FILLED", Terminal: true},
		},
	}

	result, err := awaitOrderStatus(adapter, exchange.OrderLookupRequest{
		VenueSymbol:   "BTCUSDT",
		ClientOrderID: "probe-1",
	}, 2*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("awaitOrderStatus error = %v", err)
	}
	if !result.OK || !result.Terminal {
		t.Fatalf("expected successful terminal result, got %#v", result)
	}
	if result.LastStatus == nil || result.LastStatus.Status != "FILLED" {
		t.Fatalf("expected FILLED terminal status, got %#v", result.LastStatus)
	}
	if result.Polls != 2 {
		t.Fatalf("expected 2 polls, got %d", result.Polls)
	}
}

func TestAwaitOrderStatusRequiresOrderIdentifier(t *testing.T) {
	adapter := &fakeTradeAdapter{}
	_, err := awaitOrderStatus(adapter, exchange.OrderLookupRequest{VenueSymbol: "BTCUSDT"}, time.Second, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected missing order identifier error")
	}
}

func TestSuggestQuantityForMinNotionalRoundsUpToStep(t *testing.T) {
	got := suggestQuantityForMinNotional(100, 71202.3, 0.001, 0.001)
	if got != 0.002 {
		t.Fatalf("expected 0.002, got %v", got)
	}
}
