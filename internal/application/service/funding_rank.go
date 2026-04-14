package service

import (
	"sort"
	"strings"
	"time"
)

type fundingRankInfo struct {
	Rank       int
	Total      int
	Percentile float64
}

type fundingRankContext map[string]map[string]fundingRankInfo

type fundingRankRow struct {
	Symbol string
	Rate   float64
}

func (ctx fundingRankContext) lookup(exchangeName, symbol string) fundingRankInfo {
	exchangeKey := strings.ToLower(strings.TrimSpace(exchangeName))
	symbolKey := strings.ToUpper(strings.TrimSpace(symbol))
	if rows, ok := ctx[exchangeKey]; ok {
		if info, ok := rows[symbolKey]; ok {
			return info
		}
	}
	return fundingRankInfo{}
}

func (r *StrategyRunner) buildFundingRankContext(now time.Time) fundingRankContext {
	if r == nil || r.store == nil {
		return nil
	}

	byExchange := make(map[string][]fundingRankRow)
	for _, symbol := range r.store.Watchlist() {
		for _, exchangeName := range r.store.ExchangesForSymbol(symbol) {
			meta, ok := r.store.Symbol(exchangeName, symbol)
			if !ok || !isPerpetualSymbol(meta) {
				continue
			}
			snapshot, ok := r.store.LatestFunding(exchangeName, symbol)
			if !ok || r.isSnapshotStale(now, snapshot.EventTimeMs) {
				continue
			}
			exchangeKey := strings.ToLower(strings.TrimSpace(exchangeName))
			byExchange[exchangeKey] = append(byExchange[exchangeKey], fundingRankRow{
				Symbol: strings.ToUpper(strings.TrimSpace(symbol)),
				Rate:   snapshot.FundingRate,
			})
		}
	}

	out := make(fundingRankContext, len(byExchange))
	for exchangeKey, rows := range byExchange {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Rate == rows[j].Rate {
				return rows[i].Symbol < rows[j].Symbol
			}
			return rows[i].Rate > rows[j].Rate
		})
		out[exchangeKey] = make(map[string]fundingRankInfo, len(rows))
		for i, row := range rows {
			rank := i + 1
			out[exchangeKey][row.Symbol] = fundingRankInfo{
				Rank:       rank,
				Total:      len(rows),
				Percentile: rankPercentile(rank, len(rows)),
			}
		}
	}
	return out
}

func rankPercentile(rank, total int) float64 {
	switch {
	case total <= 0 || rank <= 0:
		return 0
	case total == 1:
		return 1
	default:
		return 1 - float64(rank-1)/float64(total-1)
	}
}

func sameExchangeFundingSelectionBias(info fundingRankInfo, forecast fundingForecast) float64 {
	bias := 0.0
	if info.Total > 0 {
		bias += maxFloat(0, info.Percentile-0.50) * 12
	}
	if forecast.HistorySampleCount > 0 {
		bias += maxFloat(0, forecast.CurrentHistoricalPercentile-0.50) * 8
		bias += maxFloat(0, forecast.HistoryNegativeRatio-0.50) * 6
	}
	return bias
}
