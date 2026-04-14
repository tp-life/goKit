package service

import (
	"fmt"
	"math"

	"goKit/internal/domain/entity"
)

const (
	sameExchangeBasisReasonEligible            = "eligible"
	sameExchangeBasisReasonShortWindowTooWide  = "short_window_too_wide"
	sameExchangeBasisReasonPaybackCarryMissing = "payback_carry_missing"
	sameExchangeBasisReasonPaybackTooHigh      = "payback_too_high"
	sameExchangeBasisReasonExtremePayback      = "extreme_payback"
)

type sameExchangeBasisAssessment struct {
	BasisBps             float64
	BasisCostBps         float64
	CarryPerEventBps     float64
	PaybackFundingEvents float64
	UsesPaybackModel     bool
	Allowed              bool
	SizeMultiplier       float64
	Reason               string
}

func assessSameExchangeBasisRisk(
	cfg Config,
	arbitrageMode string,
	fundingWindowHours float64,
	carryRate float64,
	longEventCount int,
	shortEventCount int,
	currentBasisBps float64,
	totalCostsPNL float64,
	notional float64,
	hardLimitBps float64,
) sameExchangeBasisAssessment {
	assessment := sameExchangeBasisAssessment{
		BasisBps:       math.Abs(currentBasisBps),
		Allowed:        true,
		SizeMultiplier: 1,
		Reason:         sameExchangeBasisReasonEligible,
	}
	if normalizeArbitrageMode(arbitrageMode, normalizeArbitrageMode(cfg.ArbitrageMode, ArbitrageModeCrossExchange)) != ArbitrageModeSameExchangeSpotPerp {
		return assessment
	}

	costBps := 0.0
	if notional > 0 && !math.IsNaN(notional) && !math.IsInf(notional, 0) {
		costBps = maxFloat(totalCostsPNL, 0) / notional * 10000
	}
	assessment.BasisCostBps = assessment.BasisBps + costBps

	if !sameExchangeBasisUsesPaybackModel(cfg, fundingWindowHours) {
		if hardLimitBps > 0 && assessment.BasisBps > hardLimitBps {
			assessment.Reason = sameExchangeBasisReasonShortWindowTooWide
			if cfg.SameExchangeExtremeBasisSizeMultiplier > 0 && cfg.SameExchangeExtremeBasisSizeMultiplier < 1 {
				assessment.SizeMultiplier = cfg.SameExchangeExtremeBasisSizeMultiplier
			}
		}
		return assessment
	}

	assessment.UsesPaybackModel = true
	eventCount := maxInt(maxInt(longEventCount, shortEventCount), 1)
	if carryRate > 0 {
		assessment.CarryPerEventBps = carryRate * 10000 / float64(eventCount)
	}
	if assessment.CarryPerEventBps <= 0 {
		assessment.Reason = sameExchangeBasisReasonPaybackCarryMissing
		return assessment
	}

	assessment.PaybackFundingEvents = assessment.BasisCostBps / assessment.CarryPerEventBps
	if cfg.SameExchangeMaxBasisPaybackEvents > 0 && assessment.PaybackFundingEvents > cfg.SameExchangeMaxBasisPaybackEvents {
		assessment.Reason = sameExchangeBasisReasonPaybackTooHigh
		if cfg.SameExchangeExtremeBasisSizeMultiplier > 0 && cfg.SameExchangeExtremeBasisSizeMultiplier < 1 {
			assessment.SizeMultiplier = cfg.SameExchangeExtremeBasisSizeMultiplier
		}
		return assessment
	}
	if cfg.SameExchangeExtremeBasisPaybackEvents > 0 &&
		assessment.PaybackFundingEvents > cfg.SameExchangeExtremeBasisPaybackEvents &&
		cfg.SameExchangeExtremeBasisSizeMultiplier > 0 &&
		cfg.SameExchangeExtremeBasisSizeMultiplier < 1 {
		assessment.SizeMultiplier = cfg.SameExchangeExtremeBasisSizeMultiplier
		assessment.Reason = sameExchangeBasisReasonExtremePayback
	}
	return assessment
}

func sameExchangeBasisUsesPaybackModel(cfg Config, fundingWindowHours float64) bool {
	if cfg.SameExchangeBasisLongHoldWindowHours <= 0 {
		return false
	}
	return fundingWindowHours+1e-9 >= cfg.SameExchangeBasisLongHoldWindowHours
}

func sameExchangeBasisRejectReason(cfg Config, assessment sameExchangeBasisAssessment, hardLimitBps float64) string {
	switch assessment.Reason {
	case sameExchangeBasisReasonShortWindowTooWide:
		return fmt.Sprintf("same_exchange basis guard: basis %.4f bps > short-window max %.4f bps", assessment.BasisBps, hardLimitBps)
	case sameExchangeBasisReasonPaybackCarryMissing:
		return "same_exchange basis guard: carry per event is not positive"
	case sameExchangeBasisReasonPaybackTooHigh:
		return fmt.Sprintf(
			"same_exchange basis guard: payback %.2f funding events > max %.2f (basis_cost=%.2f bps carry/event=%.2f bps)",
			assessment.PaybackFundingEvents,
			cfg.SameExchangeMaxBasisPaybackEvents,
			assessment.BasisCostBps,
			assessment.CarryPerEventBps,
		)
	default:
		return ""
	}
}

func sameExchangeBasisAssessmentPresent(reason string, usesPayback bool, costBps, carryPerEventBps, paybackEvents, sizeMultiplier float64) bool {
	return reason != "" ||
		usesPayback ||
		math.Abs(costBps) > 1e-9 ||
		math.Abs(carryPerEventBps) > 1e-9 ||
		math.Abs(paybackEvents) > 1e-9 ||
		sizeMultiplier > 0
}

func applySameExchangeBasisAssessmentToOpportunity(opp *entity.Opportunity, assessment sameExchangeBasisAssessment) {
	if opp == nil {
		return
	}
	opp.SameExchangeBasisUsesPaybackModel = assessment.UsesPaybackModel
	opp.SameExchangeBasisCostBps = round4(assessment.BasisCostBps)
	opp.SameExchangeBasisCarryPerEventBps = round4(assessment.CarryPerEventBps)
	opp.SameExchangeBasisPaybackFundingEvents = round4(assessment.PaybackFundingEvents)
	opp.SameExchangeBasisAllowed = assessment.Allowed
	opp.SameExchangeBasisReason = assessment.Reason
	opp.SameExchangeBasisRiskSizeMultiplier = round4(assessment.SizeMultiplier)
}

func applySameExchangeBasisAssessmentToPlan(plan *entity.ExecutionPlan, assessment sameExchangeBasisAssessment) {
	if plan == nil {
		return
	}
	plan.SameExchangeBasisUsesPaybackModel = assessment.UsesPaybackModel
	plan.SameExchangeBasisCostBps = round4(assessment.BasisCostBps)
	plan.SameExchangeBasisCarryPerEventBps = round4(assessment.CarryPerEventBps)
	plan.SameExchangeBasisPaybackFundingEvents = round4(assessment.PaybackFundingEvents)
	plan.SameExchangeBasisAllowed = assessment.Allowed
	plan.SameExchangeBasisReason = assessment.Reason
	plan.SameExchangeBasisRiskSizeMultiplier = round4(assessment.SizeMultiplier)
}
