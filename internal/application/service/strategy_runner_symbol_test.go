package service

import (
	"testing"

	"goKit/internal/domain/entity"
)

func TestFlattenSymbolInventory_PreservesSingleExchangeSymbols(t *testing.T) {
	inventory := flattenSymbolInventory(map[string][]entity.Symbol{
		"binance": {
			{Symbol: "BTC", VenueSymbol: "BTCUSDT"},
			{Symbol: "ONLYBINANCE", VenueSymbol: "ONLYBINANCEUSDT"},
		},
		"aster": {
			{Symbol: "BTC", VenueSymbol: "BTCUSDT"},
		},
	})

	if len(inventory) != 3 {
		t.Fatalf("expected all exchange symbols to be preserved, got %d", len(inventory))
	}

	foundSingleVenue := false
	for _, item := range inventory {
		if item.Exchange == "binance" && item.Symbol == "ONLYBINANCE" {
			foundSingleVenue = true
			if item.Watched {
				t.Fatalf("expected single-exchange inventory symbol to be stored but not watched")
			}
		}
	}
	if !foundSingleVenue {
		t.Fatalf("expected single-exchange symbol to remain in flattened inventory")
	}
}

func TestBuildMultiVenueWatchlist_OnlyKeepsCrossVenueIntersection(t *testing.T) {
	watchlist, byExchange := buildMultiVenueWatchlist(map[string][]entity.Symbol{
		"binance": {
			{Symbol: "BTC", VenueSymbol: "BTCUSDT"},
			{Symbol: "ONLYBINANCE", VenueSymbol: "ONLYBINANCEUSDT"},
		},
		"aster": {
			{Symbol: "BTC", VenueSymbol: "BTCUSDT"},
			{Symbol: "ONLYASTER", VenueSymbol: "ONLYASTERUSDT"},
		},
	})

	if len(watchlist) != 1 || watchlist[0] != "BTC" {
		t.Fatalf("expected watchlist to only keep cross-venue symbol BTC, got %#v", watchlist)
	}
	if got := len(byExchange["binance"]); got != 1 {
		t.Fatalf("expected only one watched binance symbol, got %d", got)
	}
	if got := len(byExchange["aster"]); got != 1 {
		t.Fatalf("expected only one watched aster symbol, got %d", got)
	}
	if !byExchange["binance"][0].Watched || !byExchange["aster"][0].Watched {
		t.Fatalf("expected watched symbols in cross-venue watchlist to be flagged as watched")
	}
}
