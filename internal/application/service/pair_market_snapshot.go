package service

import "goKit/internal/domain/entity"

type pairMarketSnapshot struct {
	LongMeta     entity.Symbol
	ShortMeta    entity.Symbol
	LongBook     entity.BookTopSnapshot
	ShortBook    entity.BookTopSnapshot
	LongFunding  entity.FundingSnapshot
	ShortFunding entity.FundingSnapshot
}

func loadPairFundingSnapshot(store *MarketStore, cfg Config, symbol, longExchange, shortExchange string) (pairMarketSnapshot, bool) {
	if store == nil {
		return pairMarketSnapshot{}, false
	}

	longMeta, okLongMeta := store.Symbol(longExchange, symbol)
	shortMeta, okShortMeta := store.Symbol(shortExchange, symbol)
	if !(okLongMeta && okShortMeta) {
		return pairMarketSnapshot{}, false
	}

	longFunding, okLongFunding := store.LatestFunding(longExchange, symbol)
	shortFunding, okShortFunding := store.LatestFunding(shortExchange, symbol)
	longBook := entity.BookTopSnapshot{}
	shortBook := entity.BookTopSnapshot{}
	if normalizeArbitrageMode(cfg.ArbitrageMode, ArbitrageModeCrossExchange) == ArbitrageModeSameExchangeSpotPerp {
		switch {
		case !okLongFunding && isSpotSymbol(longMeta) && okShortFunding:
			var ok bool
			longBook, ok = store.LatestBookTop(longExchange, symbol)
			if !ok {
				return pairMarketSnapshot{}, false
			}
			longFunding = syntheticSpotFundingSnapshot(longMeta, longBook, shortFunding)
			okLongFunding = true
		case !okShortFunding && isSpotSymbol(shortMeta) && okLongFunding:
			var ok bool
			shortBook, ok = store.LatestBookTop(shortExchange, symbol)
			if !ok {
				return pairMarketSnapshot{}, false
			}
			shortFunding = syntheticSpotFundingSnapshot(shortMeta, shortBook, longFunding)
			okShortFunding = true
		}
	}
	if !(okLongFunding && okShortFunding) {
		return pairMarketSnapshot{}, false
	}

	return pairMarketSnapshot{
		LongMeta:     longMeta,
		ShortMeta:    shortMeta,
		LongBook:     longBook,
		ShortBook:    shortBook,
		LongFunding:  longFunding,
		ShortFunding: shortFunding,
	}, true
}

func loadPairMarketSnapshot(store *MarketStore, cfg Config, symbol, longExchange, shortExchange string) (pairMarketSnapshot, bool) {
	snapshots, ok := loadPairFundingSnapshot(store, cfg, symbol, longExchange, shortExchange)
	if !ok {
		return pairMarketSnapshot{}, false
	}
	longBook, okLongBook := store.LatestBookTop(longExchange, symbol)
	shortBook, okShortBook := store.LatestBookTop(shortExchange, symbol)
	if !(okLongBook && okShortBook) {
		return pairMarketSnapshot{}, false
	}

	snapshots.LongBook = longBook
	snapshots.ShortBook = shortBook
	return pairMarketSnapshot{
		LongMeta:     snapshots.LongMeta,
		ShortMeta:    snapshots.ShortMeta,
		LongBook:     longBook,
		ShortBook:    shortBook,
		LongFunding:  snapshots.LongFunding,
		ShortFunding: snapshots.ShortFunding,
	}, true
}
