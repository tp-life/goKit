package service

import (
	"strings"
	"time"
)

type executionPenaltyBreakdown struct {
	EntryPenaltyBps      float64
	ExitPenaltyBps       float64
	HedgeRollbackBps     float64
	TotalPenaltyBps      float64
	RiskMultiplier       float64
	ExperienceBucket     string
	ExperienceModel      string
	ExitBasisResidualBps float64
}

func (r *StrategyRunner) estimateExecutionPenalty(now time.Time, symbol, longExchange, shortExchange string, projection fundingProjection, currentBasisBps float64) executionPenaltyBreakdown {
	base := maxFloat(r.cfg.SlippageBps, 0)
	exchangeMultiplier := (exchangePenaltyMultiplier(longExchange) + exchangePenaltyMultiplier(shortExchange)) / 2
	symbolMultiplier := symbolPenaltyMultiplier(symbol)
	timeMultiplier, bucket := timeBucketPenaltyMultiplier(now)
	riskMultiplier := exchangeMultiplier * symbolMultiplier * timeMultiplier

	entryPenalty := base * riskMultiplier

	windowHours := projection.FundingWindowHours
	if windowHours <= 0 {
		windowHours = 0.25
	}
	basisRetention := 1 / (1 + windowHours/4)
	exitBasisResidual := currentBasisBps * basisRetention
	exitPenalty := base*riskMultiplier + exitBasisResidual*0.35

	maxEvents := maxInt(projection.LongFundingEventCount, projection.ShortFundingEventCount)
	hedgeRollbackPenalty := float64(maxInt(maxEvents-1, 0)) * 0.20 * riskMultiplier

	total := entryPenalty + exitPenalty + hedgeRollbackPenalty
	return executionPenaltyBreakdown{
		EntryPenaltyBps:      entryPenalty,
		ExitPenaltyBps:       exitPenalty,
		HedgeRollbackBps:     hedgeRollbackPenalty,
		TotalPenaltyBps:      total,
		RiskMultiplier:       riskMultiplier,
		ExperienceBucket:     bucket,
		ExperienceModel:      "exchange_symbol_time_bucket_v1",
		ExitBasisResidualBps: exitBasisResidual,
	}
}

func exchangePenaltyMultiplier(exchangeName string) float64 {
	switch strings.ToLower(strings.TrimSpace(exchangeName)) {
	case "binance":
		return 1.00
	case "aster":
		return 1.10
	case "hyperliquid":
		return 1.15
	default:
		return 1.05
	}
}

func symbolPenaltyMultiplier(symbol string) float64 {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "BTC", "ETH":
		return 0.85
	case "SOL", "BNB":
		return 0.92
	default:
		return 1.00
	}
}

func timeBucketPenaltyMultiplier(now time.Time) (float64, string) {
	hour := now.UTC().Hour()
	switch hour {
	case 0, 1, 8, 9, 16, 17:
		return 1.10, "funding_window_hot"
	case 2, 10, 18:
		return 1.05, "post_funding_transition"
	default:
		return 1.00, "normal_liquidity"
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
