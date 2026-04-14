package service

import (
	"sort"
	"strings"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

type crossExchangePair struct {
	LeftExchange  string
	RightExchange string
}

type spotPerpPair struct {
	SpotExchange string
	PerpExchange string
}

func normalizedExchangeConfigs(cfg exchange.ConfigSet) map[string]exchange.ExchangeConfig {
	items := cfg.Items()
	out := make(map[string]exchange.ExchangeConfig, len(items))
	for name, item := range items {
		out[strings.ToLower(strings.TrimSpace(name))] = item
	}
	return out
}

func isSpotSymbol(meta entity.Symbol) bool {
	return strings.EqualFold(strings.TrimSpace(meta.ContractType), "SPOT")
}

func isPerpetualSymbol(meta entity.Symbol) bool {
	contractType := strings.ToUpper(strings.TrimSpace(meta.ContractType))
	return contractType == "" || strings.Contains(contractType, "PERPETUAL")
}

func buildWatchlistForArbitrageMode(arbitrageMode string, symbolsByExchange map[string][]entity.Symbol, exchangeConfigs map[string]exchange.ExchangeConfig) ([]string, map[string][]entity.Symbol) {
	switch normalizeArbitrageMode(arbitrageMode, ArbitrageModeCrossExchange) {
	case ArbitrageModeSameExchangeSpotPerp:
		return buildSpotPerpWatchlist(symbolsByExchange, exchangeConfigs)
	default:
		return buildCrossExchangePerpWatchlist(symbolsByExchange)
	}
}

func buildCrossExchangePerpWatchlist(symbolsByExchange map[string][]entity.Symbol) ([]string, map[string][]entity.Symbol) {
	byCanonical := make(map[string]map[string]entity.Symbol)
	for ex, items := range symbolsByExchange {
		for _, item := range items {
			if !isPerpetualSymbol(item) {
				continue
			}
			key := strings.ToUpper(item.Symbol)
			if byCanonical[key] == nil {
				byCanonical[key] = make(map[string]entity.Symbol)
			}
			item.Exchange = ex
			if _, ok := byCanonical[key][ex]; ok {
				continue
			}
			byCanonical[key][ex] = item
		}
	}
	watchlist := make([]string, 0)
	recordsByExchange := make(map[string][]entity.Symbol)
	for symbol, m := range byCanonical {
		if len(m) < 2 {
			continue
		}
		watchlist = append(watchlist, symbol)
		for ex, item := range m {
			item.Watched = true
			recordsByExchange[ex] = append(recordsByExchange[ex], item)
		}
	}
	sort.Strings(watchlist)
	for ex := range recordsByExchange {
		sort.Slice(recordsByExchange[ex], func(i, j int) bool { return recordsByExchange[ex][i].Symbol < recordsByExchange[ex][j].Symbol })
	}
	return watchlist, recordsByExchange
}

func buildSpotPerpWatchlist(symbolsByExchange map[string][]entity.Symbol, exchangeConfigs map[string]exchange.ExchangeConfig) ([]string, map[string][]entity.Symbol) {
	type groupBucket struct {
		Spots map[string]entity.Symbol
		Perps map[string]entity.Symbol
	}

	byCanonical := make(map[string]map[string]*groupBucket)
	for ex, items := range symbolsByExchange {
		exKey := strings.ToLower(strings.TrimSpace(ex))
		group := exchangeConfigs[exKey].ArbitrageGroupKey(ex)
		for _, item := range items {
			canonical := strings.ToUpper(strings.TrimSpace(item.Symbol))
			if canonical == "" {
				continue
			}
			if byCanonical[canonical] == nil {
				byCanonical[canonical] = make(map[string]*groupBucket)
			}
			if byCanonical[canonical][group] == nil {
				byCanonical[canonical][group] = &groupBucket{
					Spots: make(map[string]entity.Symbol),
					Perps: make(map[string]entity.Symbol),
				}
			}
			item.Exchange = ex
			switch {
			case isSpotSymbol(item):
				if _, ok := byCanonical[canonical][group].Spots[exKey]; ok {
					continue
				}
				byCanonical[canonical][group].Spots[exKey] = item
			case isPerpetualSymbol(item):
				if _, ok := byCanonical[canonical][group].Perps[exKey]; ok {
					continue
				}
				byCanonical[canonical][group].Perps[exKey] = item
			}
		}
	}

	watchlist := make([]string, 0)
	recordsByExchange := make(map[string][]entity.Symbol)
	for symbol, groups := range byCanonical {
		eligible := false
		seen := make(map[string]struct{})
		for _, bucket := range groups {
			if len(bucket.Spots) == 0 || len(bucket.Perps) == 0 {
				continue
			}
			eligible = true
			for ex, item := range bucket.Spots {
				if _, ok := seen[ex]; ok {
					continue
				}
				item.Watched = true
				recordsByExchange[ex] = append(recordsByExchange[ex], item)
				seen[ex] = struct{}{}
			}
			for ex, item := range bucket.Perps {
				if _, ok := seen[ex]; ok {
					continue
				}
				item.Watched = true
				recordsByExchange[ex] = append(recordsByExchange[ex], item)
				seen[ex] = struct{}{}
			}
		}
		if eligible {
			watchlist = append(watchlist, symbol)
		}
	}

	sort.Strings(watchlist)
	for ex := range recordsByExchange {
		sort.Slice(recordsByExchange[ex], func(i, j int) bool { return recordsByExchange[ex][i].Symbol < recordsByExchange[ex][j].Symbol })
	}
	return watchlist, recordsByExchange
}

func (r *StrategyRunner) crossExchangePairs(symbol string) []crossExchangePair {
	exchanges := r.store.ExchangesForSymbol(symbol)
	filtered := make([]string, 0, len(exchanges))
	for _, ex := range exchanges {
		meta, ok := r.store.Symbol(ex, symbol)
		if !ok || !isPerpetualSymbol(meta) {
			continue
		}
		filtered = append(filtered, ex)
	}
	sort.Strings(filtered)
	out := make([]crossExchangePair, 0, len(filtered))
	for i := 0; i < len(filtered); i++ {
		for j := i + 1; j < len(filtered); j++ {
			out = append(out, crossExchangePair{
				LeftExchange:  filtered[i],
				RightExchange: filtered[j],
			})
		}
	}
	return out
}

func (r *StrategyRunner) sameExchangeSpotPerpPairs(symbol string) []spotPerpPair {
	type bucket struct {
		Spots []string
		Perps []string
	}

	grouped := make(map[string]*bucket)
	for _, ex := range r.store.ExchangesForSymbol(symbol) {
		meta, ok := r.store.Symbol(ex, symbol)
		if !ok {
			continue
		}
		group := ""
		if r != nil && r.exchangeConfigs != nil {
			group = r.exchangeConfigs[strings.ToLower(strings.TrimSpace(ex))].ArbitrageGroupKey(ex)
		}
		if strings.TrimSpace(group) == "" {
			group = strings.ToLower(strings.TrimSpace(ex))
		}
		if grouped[group] == nil {
			grouped[group] = &bucket{}
		}
		switch {
		case isSpotSymbol(meta):
			grouped[group].Spots = append(grouped[group].Spots, ex)
		case isPerpetualSymbol(meta):
			grouped[group].Perps = append(grouped[group].Perps, ex)
		}
	}

	out := make([]spotPerpPair, 0)
	for _, item := range grouped {
		if len(item.Spots) == 0 || len(item.Perps) == 0 {
			continue
		}
		sort.Strings(item.Spots)
		sort.Strings(item.Perps)
		for _, spotExchange := range item.Spots {
			for _, perpExchange := range item.Perps {
				out = append(out, spotPerpPair{
					SpotExchange: spotExchange,
					PerpExchange: perpExchange,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SpotExchange == out[j].SpotExchange {
			return out[i].PerpExchange < out[j].PerpExchange
		}
		return out[i].SpotExchange < out[j].SpotExchange
	})
	return out
}

func bookReferencePrice(book entity.BookTopSnapshot) float64 {
	switch {
	case book.BidPrice > 0 && book.AskPrice > 0:
		return (book.BidPrice + book.AskPrice) / 2
	case book.AskPrice > 0:
		return book.AskPrice
	default:
		return book.BidPrice
	}
}

func syntheticSpotFundingSnapshot(spotMeta entity.Symbol, spotBook entity.BookTopSnapshot, anchor entity.FundingSnapshot) entity.FundingSnapshot {
	price := bookReferencePrice(spotBook)
	if price <= 0 {
		price = firstPositiveFloat(anchor.IndexPrice, anchor.MarkPrice, anchor.EstimatedSettlePrice)
	}
	return entity.FundingSnapshot{
		Exchange:             spotMeta.Exchange,
		Symbol:               spotMeta.Symbol,
		VenueSymbol:          spotMeta.VenueSymbol,
		MarkPrice:            price,
		IndexPrice:           price,
		EstimatedSettlePrice: price,
		FundingRate:          0,
		FundingTimeMs:        anchor.FundingTimeMs,
		FundingIntervalHours: maxInt(anchor.FundingIntervalHours, 1),
		EventTimeMs:          spotBook.EventTimeMs,
	}
}

func syntheticSpotFundingForecast() fundingForecast {
	return fundingForecast{
		CurrentRate:        0,
		BaselineRate:       0,
		HistoryMean:        0,
		Regime:             "spot_zero",
		Confidence:         "high",
		MeanReversion:      0,
		ContinuationDecay:  1,
		ClampSource:        "spot_synthetic_zero",
		EffectiveFloorRate: 0,
		EffectiveCapRate:   0,
	}
}
