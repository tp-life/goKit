package service

import (
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

func TestMarketStore_UpsertFundingRejectsOlderSnapshot(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", FundingIntervalHours: 8})

	store.UpsertFunding(entity.FundingSnapshot{
		Exchange:    "binance",
		Symbol:      "btc",
		FundingRate: 0.001,
		EventTimeMs: 2000,
	})
	store.UpsertFunding(entity.FundingSnapshot{
		Exchange:    "binance",
		Symbol:      "BTC",
		FundingRate: 0.0001,
		EventTimeMs: 1000,
	})

	got, ok := store.LatestFunding("binance", "BTC")
	if !ok {
		t.Fatal("expected funding snapshot to exist")
	}
	if got.FundingRate != 0.001 {
		t.Fatalf("expected older funding snapshot to be ignored, got rate %.8f", got.FundingRate)
	}
}

func TestMarketStore_UpsertBookTopRejectsMissingTimestampWhenNewerExists(t *testing.T) {
	store := NewMarketStore()
	store.UpsertSymbol(entity.Symbol{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT"})

	store.UpsertBookTop(entity.BookTopSnapshot{
		Exchange:    "binance",
		Symbol:      "BTC",
		BidPrice:    100,
		AskPrice:    101,
		EventTimeMs: 2000,
	})
	store.UpsertBookTop(entity.BookTopSnapshot{
		Exchange: "binance",
		Symbol:   "BTC",
		BidPrice: 90,
		AskPrice: 91,
	})

	got, ok := store.LatestBookTop("binance", "BTC")
	if !ok {
		t.Fatal("expected book snapshot to exist")
	}
	if got.BidPrice != 100 || got.AskPrice != 101 {
		t.Fatalf("expected stale/missing-timestamp book snapshot to be ignored, got bid=%.4f ask=%.4f", got.BidPrice, got.AskPrice)
	}
}

func TestMarketStore_UpdateStatusDoesNotMoveConnectorClockBackward(t *testing.T) {
	store := NewMarketStore()
	newerMarket := time.Now().UTC()
	newerBook := newerMarket.Add(2 * time.Second)
	store.UpdateStatus(exchange.ConnectorStatus{
		Exchange:            "binance",
		LastMarketEventAt:   newerMarket,
		LastBookEventAt:     newerBook,
		MarkPriceConnected:  true,
		BookTickerConnected: true,
	})

	store.UpdateStatus(exchange.ConnectorStatus{
		Exchange:          "binance",
		LastMarketEventAt: newerMarket.Add(-time.Minute),
		LastBookEventAt:   newerBook.Add(-time.Minute),
	})

	statuses := store.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 connector status, got %d", len(statuses))
	}
	if !statuses[0].LastMarketEventAt.Equal(newerMarket) {
		t.Fatalf("expected market event timestamp to stay at latest value, got %s", statuses[0].LastMarketEventAt)
	}
	if !statuses[0].LastBookEventAt.Equal(newerBook) {
		t.Fatalf("expected book event timestamp to stay at latest value, got %s", statuses[0].LastBookEventAt)
	}
}
