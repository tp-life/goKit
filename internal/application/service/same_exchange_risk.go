package service

import (
	"math"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

func sameExchangePerpLeverage(cfg Config) float64 {
	leverage := cfg.Leverage
	if cfg.SameExchangeMaxPerpLeverage > 0 && (leverage <= 0 || cfg.SameExchangeMaxPerpLeverage < leverage) {
		leverage = cfg.SameExchangeMaxPerpLeverage
	}
	if leverage < 1 {
		leverage = 1
	}
	return leverage
}

func effectivePlanLeverage(cfg Config, plan *entity.ExecutionPlan, meta entity.Symbol) float64 {
	if isSpotSymbol(meta) {
		return 1
	}
	leverage := cfg.Leverage
	if plan != nil && plan.TargetLeverage > 0 {
		leverage = plan.TargetLeverage
	}
	arbitrageMode := normalizeArbitrageMode(cfg.ArbitrageMode, ArbitrageModeCrossExchange)
	if plan != nil {
		arbitrageMode = normalizeArbitrageMode(plan.ArbitrageMode, arbitrageMode)
	}
	if arbitrageMode == ArbitrageModeSameExchangeSpotPerp && cfg.SameExchangeMaxPerpLeverage > 0 && leverage > cfg.SameExchangeMaxPerpLeverage {
		leverage = cfg.SameExchangeMaxPerpLeverage
	}
	if leverage < 1 {
		leverage = 1
	}
	return leverage
}

func sameExchangeFundingRiskSizeMultiplier(cfg Config, plan entity.ExecutionPlan) float64 {
	if normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(cfg.ArbitrageMode, ArbitrageModeCrossExchange)) != ArbitrageModeSameExchangeSpotPerp {
		return 1
	}
	if plan.PerpFundingHistorySampleCount <= 0 {
		return 1
	}
	extremePercentile := math.Max(plan.PerpFundingCurrentHistoricalPercentile, plan.PerpFundingRankPercentile)
	if extremePercentile+1e-9 < cfg.SameExchangeFundingExtremePercentile {
		return 1
	}
	if plan.PerpFundingHistoryNegativeRatio+1e-9 < cfg.SameExchangeExtremeFundingNegativeRatio {
		return 1
	}
	if cfg.SameExchangeExtremeFundingSizeMultiplier <= 0 || cfg.SameExchangeExtremeFundingSizeMultiplier >= 1 {
		return 1
	}
	return cfg.SameExchangeExtremeFundingSizeMultiplier
}

func sameExchangeBasisRiskSizeMultiplier(cfg Config, plan entity.ExecutionPlan) float64 {
	assessment := assessSameExchangeBasisRisk(
		cfg,
		plan.ArbitrageMode,
		plan.FundingWindowHours,
		rateFromPNL(plan.FundingCarryPNL, plan.TargetNotionalUSDT),
		plan.LongFundingEventCount,
		plan.ShortFundingEventCount,
		plan.CrossVenueBasisBps,
		plan.EntryFeePNL+plan.ExitFeePNL+plan.SlippagePNL+plan.SafetyBufferPNL,
		plan.TargetNotionalUSDT,
		allowedBasisThresholdBpsForConfig(cfg, plan.FundingWindowHours),
	)
	if assessment.SizeMultiplier > 0 && assessment.SizeMultiplier < 1 {
		return assessment.SizeMultiplier
	}
	return 1
}

func rateFromPNL(pnl, notional float64) float64 {
	if notional <= 0 {
		return 0
	}
	return pnl / notional
}

func positionLiquidationDistanceRatio(pos exchange.Position) float64 {
	if pos.MarkPrice <= 0 || pos.LiquidationPrice <= 0 || math.Abs(pos.Quantity) <= 1e-9 {
		return 0
	}
	switch {
	case pos.Quantity < 0:
		return math.Max(0, (pos.LiquidationPrice-pos.MarkPrice)/pos.MarkPrice)
	case pos.Quantity > 0:
		return math.Max(0, (pos.MarkPrice-pos.LiquidationPrice)/pos.MarkPrice)
	default:
		return 0
	}
}
