package service

import (
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

func TestConfigEffectiveNotional_SameExchangeSpotPerpIgnoresLeverage(t *testing.T) {
	cfg := Config{
		ArbitrageMode:      ArbitrageModeSameExchangeSpotPerp,
		TotalCapitalUSDT:   1000,
		CapitalUtilization: 0.8,
		Leverage:           3,
	}.normalize()

	if got := cfg.EffectiveNotional(); got != 800 {
		t.Fatalf("expected same-exchange spot-perp effective notional 800, got %.2f", got)
	}
}

func TestBuildSpotPerpWatchlist_RequiresSharedArbitrageGroup(t *testing.T) {
	symbolsByExchange := map[string][]entity.Symbol{
		"binance_spot": {
			{Exchange: "binance_spot", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "SPOT"},
		},
		"binance_perp": {
			{Exchange: "binance_perp", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "PERPETUAL"},
		},
		"bybit_spot": {
			{Exchange: "bybit_spot", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "SPOT"},
		},
	}
	exchangeConfigs := map[string]exchange.ExchangeConfig{
		"binance_spot": {ArbitrageGroup: "binance"},
		"binance_perp": {ArbitrageGroup: "binance"},
		"bybit_spot":   {ArbitrageGroup: "bybit"},
	}

	watchlist, recordsByExchange := buildSpotPerpWatchlist(symbolsByExchange, exchangeConfigs)
	if len(watchlist) != 1 || watchlist[0] != "BTC" {
		t.Fatalf("expected BTC to be the only watchlist symbol, got %#v", watchlist)
	}
	if len(recordsByExchange["binance_spot"]) != 1 || len(recordsByExchange["binance_perp"]) != 1 {
		t.Fatalf("expected binance spot/perp pair to be retained, got %+v", recordsByExchange)
	}
	if len(recordsByExchange["bybit_spot"]) != 0 {
		t.Fatalf("expected unmatched bybit spot leg to be excluded, got %+v", recordsByExchange["bybit_spot"])
	}
}

func TestLoadPairFundingSnapshot_SynthesizesSpotFundingFromPerpAnchor(t *testing.T) {
	store := NewMarketStore()
	now := time.Now().UTC()
	cfg := Config{ArbitrageMode: ArbitrageModeSameExchangeSpotPerp}.normalize()

	store.UpsertSymbol(entity.Symbol{
		Exchange:     "binance_spot",
		Symbol:       "BTC",
		VenueSymbol:  "BTCUSDT",
		BaseAsset:    "BTC",
		QuoteAsset:   "USDT",
		ContractType: "SPOT",
	})
	store.UpsertSymbol(entity.Symbol{
		Exchange:             "binance_perp",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		BaseAsset:            "BTC",
		QuoteAsset:           "USDT",
		ContractType:         "PERPETUAL",
		FundingIntervalHours: 8,
	})
	store.UpsertBookTop(entity.BookTopSnapshot{
		Exchange:    "binance_spot",
		Symbol:      "BTC",
		VenueSymbol: "BTCUSDT",
		BidPrice:    99.9,
		AskPrice:    100.1,
		EventTimeMs: now.UnixMilli(),
	})
	store.UpsertFunding(entity.FundingSnapshot{
		Exchange:             "binance_perp",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		MarkPrice:            100,
		IndexPrice:           100,
		FundingRate:          0.0012,
		FundingTimeMs:        now.Add(2 * time.Hour).UnixMilli(),
		FundingIntervalHours: 8,
		EventTimeMs:          now.UnixMilli(),
	})

	snapshots, ok := loadPairFundingSnapshot(store, cfg, "BTC", "binance_spot", "binance_perp")
	if !ok {
		t.Fatal("expected spot-perp snapshots to resolve successfully")
	}
	if snapshots.LongFunding.FundingRate != 0 {
		t.Fatalf("expected synthetic spot funding rate 0, got %.8f", snapshots.LongFunding.FundingRate)
	}
	if snapshots.LongFunding.FundingTimeMs != snapshots.ShortFunding.FundingTimeMs {
		t.Fatalf("expected synthetic spot funding time to align with perp anchor, got %d vs %d", snapshots.LongFunding.FundingTimeMs, snapshots.ShortFunding.FundingTimeMs)
	}
	if snapshots.LongFunding.EventTimeMs != now.UnixMilli() {
		t.Fatalf("expected synthetic spot funding event time to reuse spot book time, got %d", snapshots.LongFunding.EventTimeMs)
	}
}
