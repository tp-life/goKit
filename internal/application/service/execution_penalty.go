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

// estimateExecutionPenalty 负责把“执行摩擦”拆成可解释的三段：
// 1. EntryPenaltyBps：开仓即刻吃到的冲击成本；
// 2. ExitPenaltyBps：平仓时残留 basis 风险 + 退出滑点；
// 3. HedgeRollbackBps：多事件路径越长，单腿失败后回滚/补救越复杂，需要更高冗余。
//
// 其中 exchange multiplier 不再在这里用 switch 写死，而是统一来自 venue profile。
// 这样 execution penalty 与 funding forecaster 就共享同一套 venue taxonomy。
func (r *StrategyRunner) estimateExecutionPenalty(now time.Time, symbol, longExchange, shortExchange string, projection fundingProjection, currentBasisBps float64) executionPenaltyBreakdown {
	base := maxFloat(r.cfg.SlippageBps, 0)
	exchangeMultiplier := (r.exchangePenaltyMultiplier(longExchange) + r.exchangePenaltyMultiplier(shortExchange)) / 2
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

func (r *StrategyRunner) exchangePenaltyMultiplier(exchangeName string) float64 {
	if r != nil && r.venues != nil {
		return r.venues.ExecutionPenaltyMultiplier(exchangeName)
	}
	return defaultVenueProfileRegistry().ExecutionPenaltyMultiplier(exchangeName)
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
