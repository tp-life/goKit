package service

import (
	"fmt"
	"math"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

const (
	OpportunityStatusLongHoldGuard = "long_hold_guard"

	sameExchangeLongHoldReasonEligible               = "eligible"
	sameExchangeLongHoldReasonFundingNotPositive     = "funding_not_positive"
	sameExchangeLongHoldReasonHistorySamplesLow      = "history_samples_low"
	sameExchangeLongHoldReasonSupportRatioLow        = "support_ratio_low"
	sameExchangeLongHoldReasonAnnualizedNetRateLow   = "annualized_net_rate_low"
	sameExchangeLongHoldReasonProjectedNetPNLLow     = "projected_net_pnl_low"
	sameExchangeLongHoldReasonMissingFundingInterval = "missing_funding_interval"
	sameExchangeLongHoldReasonMissingNotional        = "missing_notional"
)

type sameExchangeLongHoldAssessment struct {
	HistoryMeanRate              float64
	HistoricalSupportRatio       float64
	EstimatedEventRate           float64
	EstimatedAnnualizedCarryRate float64
	EstimatedAnnualizedNetRate   float64
	SuggestedFundingEvents       int
	SuggestedHoldHours           float64
	SuggestedFundingTimeMs       int64
	SuggestedGrossFundingPNL     float64
	SuggestedNetPNL              float64
	Eligible                     bool
	Reason                       string
}

func assessSameExchangeLongHold(cfg Config, asOf time.Time, rule entity.OpportunityFundingRule, currentRate float64, forecast fundingForecast, notional, totalCostsPNL float64) sameExchangeLongHoldAssessment {
	assessment := sameExchangeLongHoldAssessment{
		HistoryMeanRate:        forecast.HistoryMean,
		HistoricalSupportRatio: sameExchangeHistoricalSupportRatio(currentRate, forecast),
		Eligible:               false,
		Reason:                 sameExchangeLongHoldReasonEligible,
	}

	intervalHours := maxInt(rule.FundingIntervalHours, 0)
	if intervalHours <= 0 {
		assessment.Reason = sameExchangeLongHoldReasonMissingFundingInterval
		return assessment
	}
	if notional <= 0 || math.IsNaN(notional) || math.IsInf(notional, 0) {
		assessment.Reason = sameExchangeLongHoldReasonMissingNotional
		return assessment
	}

	eventsPerYear := 24 * 365 / float64(intervalHours)
	if forecast.HistorySampleCount <= 0 {
		assessment.EstimatedEventRate = currentRate
		assessment.EstimatedAnnualizedCarryRate = currentRate * eventsPerYear
		assessment.EstimatedAnnualizedNetRate = assessment.EstimatedAnnualizedCarryRate - totalCostsPNL/notional
		populateSameExchangeLongHoldSuggestion(&assessment, cfg, asOf, rule, currentRate, notional, totalCostsPNL)
		assessment.Reason = sameExchangeLongHoldReasonHistorySamplesLow
		return assessment
	}

	adjustedEventRate := currentRate*assessment.HistoricalSupportRatio + forecast.HistoryMean*(1-assessment.HistoricalSupportRatio)
	assessment.EstimatedEventRate = adjustedEventRate
	assessment.EstimatedAnnualizedCarryRate = adjustedEventRate * eventsPerYear
	assessment.EstimatedAnnualizedNetRate = assessment.EstimatedAnnualizedCarryRate - totalCostsPNL/notional
	populateSameExchangeLongHoldSuggestion(&assessment, cfg, asOf, rule, adjustedEventRate, notional, totalCostsPNL)

	switch {
	case currentRate <= 0:
		assessment.Reason = sameExchangeLongHoldReasonFundingNotPositive
	case forecast.HistorySampleCount < cfg.SameExchangeMinHistorySampleCount:
		assessment.Reason = sameExchangeLongHoldReasonHistorySamplesLow
	case assessment.HistoricalSupportRatio < cfg.SameExchangeMinHistoricalSupportRatio:
		assessment.Reason = sameExchangeLongHoldReasonSupportRatioLow
	case assessment.EstimatedAnnualizedNetRate < cfg.SameExchangeMinAnnualizedNetRate:
		assessment.Reason = sameExchangeLongHoldReasonAnnualizedNetRateLow
	case assessment.SuggestedFundingEvents <= 0 || assessment.SuggestedNetPNL < cfg.MinNetPNL:
		assessment.Reason = sameExchangeLongHoldReasonProjectedNetPNLLow
	default:
		assessment.Eligible = true
		assessment.Reason = sameExchangeLongHoldReasonEligible
	}
	return assessment
}

func sameExchangeHistoricalSupportRatio(currentRate float64, forecast fundingForecast) float64 {
	switch {
	case currentRate > 0:
		return clampFloat(forecast.HistoryPositiveRatio, 0, 1)
	case currentRate < 0:
		return clampFloat(forecast.HistoryNegativeRatio, 0, 1)
	default:
		return 0
	}
}

func applySameExchangeLongHoldAssessment(rule *entity.OpportunityFundingRule, assessment sameExchangeLongHoldAssessment) {
	if rule == nil {
		return
	}
	rule.HistoryMeanRate = assessment.HistoryMeanRate
	rule.HistoricalSupportRatio = assessment.HistoricalSupportRatio
	rule.EstimatedEventRate = assessment.EstimatedEventRate
	rule.EstimatedAnnualizedCarryRate = assessment.EstimatedAnnualizedCarryRate
	rule.EstimatedAnnualizedNetRate = assessment.EstimatedAnnualizedNetRate
	rule.SuggestedFundingEvents = assessment.SuggestedFundingEvents
	rule.SuggestedHoldHours = assessment.SuggestedHoldHours
	rule.SuggestedFundingTimeMs = assessment.SuggestedFundingTimeMs
	rule.SuggestedGrossFundingPNL = assessment.SuggestedGrossFundingPNL
	rule.SuggestedNetPNL = assessment.SuggestedNetPNL
	rule.LongHoldEligible = assessment.Eligible
	rule.LongHoldReason = assessment.Reason
}

func sameExchangeLongHoldRejectReason(cfg Config, assessment sameExchangeLongHoldAssessment, sampleCount int) string {
	switch assessment.Reason {
	case sameExchangeLongHoldReasonFundingNotPositive:
		return "same_exchange long-hold guard: current perp funding is not positive"
	case sameExchangeLongHoldReasonHistorySamplesLow:
		return fmt.Sprintf("same_exchange long-hold guard: history samples %d < min %d", sampleCount, cfg.SameExchangeMinHistorySampleCount)
	case sameExchangeLongHoldReasonSupportRatioLow:
		return fmt.Sprintf("same_exchange long-hold guard: support ratio %.2f < min %.2f", assessment.HistoricalSupportRatio, cfg.SameExchangeMinHistoricalSupportRatio)
	case sameExchangeLongHoldReasonAnnualizedNetRateLow:
		return fmt.Sprintf("same_exchange long-hold guard: annualized net %.4f < min %.4f", assessment.EstimatedAnnualizedNetRate, cfg.SameExchangeMinAnnualizedNetRate)
	case sameExchangeLongHoldReasonProjectedNetPNLLow:
		return fmt.Sprintf("same_exchange long-hold guard: projected net %.4f < min %.4f within recommended hold horizon", assessment.SuggestedNetPNL, cfg.MinNetPNL)
	case sameExchangeLongHoldReasonMissingFundingInterval:
		return "same_exchange long-hold guard: missing funding interval"
	case sameExchangeLongHoldReasonMissingNotional:
		return "same_exchange long-hold guard: missing notional"
	default:
		return ""
	}
}

func sameExchangeLongHoldReasonText(reason string, cfg Config) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", sameExchangeLongHoldReasonEligible:
		return "满足长期持有门槛"
	case sameExchangeLongHoldReasonFundingNotPositive:
		return "当前永续 funding <= 0"
	case sameExchangeLongHoldReasonHistorySamplesLow:
		return fmt.Sprintf("历史样本不足 %d", cfg.SameExchangeMinHistorySampleCount)
	case sameExchangeLongHoldReasonSupportRatioLow:
		return fmt.Sprintf("同向历史支持 < %.1f%%", cfg.SameExchangeMinHistoricalSupportRatio*100)
	case sameExchangeLongHoldReasonAnnualizedNetRateLow:
		return fmt.Sprintf("粗年化净收益 < %.1f%%", cfg.SameExchangeMinAnnualizedNetRate*100)
	case sameExchangeLongHoldReasonProjectedNetPNLLow:
		return fmt.Sprintf("建议持有期净收益 < %.2f USDT", cfg.MinNetPNL)
	case sameExchangeLongHoldReasonMissingFundingInterval:
		return "缺少 funding 间隔"
	case sameExchangeLongHoldReasonMissingNotional:
		return "缺少可用名义"
	default:
		return reason
	}
}

func isSpotFundingRule(rule entity.OpportunityFundingRule) bool {
	return strings.EqualFold(strings.TrimSpace(rule.ClampSource), "spot_synthetic_zero") ||
		strings.EqualFold(strings.TrimSpace(rule.ForecastRegime), "spot_zero")
}

func refreshSameExchangePlanLongHoldAssessment(cfg Config, plan *entity.ExecutionPlan) {
	if plan == nil || normalizeArbitrageMode(plan.ArbitrageMode, ArbitrageModeCrossExchange) != ArbitrageModeSameExchangeSpotPerp {
		return
	}
	if plan.RoundedNotionalUSDT <= 0 {
		return
	}
	totalCostsPNL := plan.EntryFeePNL + plan.ExitFeePNL + plan.SlippagePNL + plan.SafetyBufferPNL
	plan.PerpFundingEstimatedAnnualizedNetRate = plan.PerpFundingEstimatedAnnualizedCarryRate - totalCostsPNL/plan.RoundedNotionalUSDT
	if plan.SameExchangeLongHoldSuggestedFundingEvents > 0 && plan.PerpFundingEstimatedEventRate > 0 {
		plan.SameExchangeLongHoldSuggestedGrossFundingPNL = round2(plan.RoundedNotionalUSDT * plan.PerpFundingEstimatedEventRate * float64(plan.SameExchangeLongHoldSuggestedFundingEvents))
		plan.SameExchangeLongHoldSuggestedNetPNL = round2(plan.SameExchangeLongHoldSuggestedGrossFundingPNL - totalCostsPNL)
	}

	switch {
	case plan.PerpFundingHistorySampleCount <= 0:
		plan.SameExchangeLongHoldEligible = false
		plan.SameExchangeLongHoldReason = sameExchangeLongHoldReasonHistorySamplesLow
	case plan.PerpFundingHistorySampleCount < cfg.SameExchangeMinHistorySampleCount:
		plan.SameExchangeLongHoldEligible = false
		plan.SameExchangeLongHoldReason = sameExchangeLongHoldReasonHistorySamplesLow
	case plan.PerpFundingHistoricalSupportRatio < cfg.SameExchangeMinHistoricalSupportRatio:
		plan.SameExchangeLongHoldEligible = false
		plan.SameExchangeLongHoldReason = sameExchangeLongHoldReasonSupportRatioLow
	case plan.PerpFundingEstimatedAnnualizedNetRate < cfg.SameExchangeMinAnnualizedNetRate:
		plan.SameExchangeLongHoldEligible = false
		plan.SameExchangeLongHoldReason = sameExchangeLongHoldReasonAnnualizedNetRateLow
	case plan.SameExchangeLongHoldSuggestedFundingEvents <= 0 || plan.SameExchangeLongHoldSuggestedNetPNL < cfg.MinNetPNL:
		plan.SameExchangeLongHoldEligible = false
		plan.SameExchangeLongHoldReason = sameExchangeLongHoldReasonProjectedNetPNLLow
	default:
		plan.SameExchangeLongHoldEligible = true
		plan.SameExchangeLongHoldReason = sameExchangeLongHoldReasonEligible
	}
}

func populateSameExchangeLongHoldSuggestion(assessment *sameExchangeLongHoldAssessment, cfg Config, asOf time.Time, rule entity.OpportunityFundingRule, adjustedEventRate, notional, totalCostsPNL float64) {
	if assessment == nil {
		return
	}
	if adjustedEventRate <= 0 || notional <= 0 {
		return
	}
	intervalHours := maxInt(rule.FundingIntervalHours, 0)
	if intervalHours <= 0 {
		return
	}
	requiredGrossPNL := totalCostsPNL + maxFloat(cfg.MinNetPNL, 0)
	grossPerEventPNL := notional * adjustedEventRate
	if grossPerEventPNL <= 0 {
		return
	}
	eventsNeeded := int(math.Ceil(requiredGrossPNL / grossPerEventPNL))
	if eventsNeeded < 1 {
		eventsNeeded = 1
	}
	maxEvents := sameExchangeLongHoldMaxRecommendationEvents(cfg, intervalHours)
	if maxEvents > 0 && eventsNeeded > maxEvents {
		return
	}
	assessment.SuggestedFundingEvents = eventsNeeded
	assessment.SuggestedFundingTimeMs = sameExchangeSuggestedFundingTimeMs(asOf.UnixMilli(), rule.NextFundingTimeMs, intervalHours, eventsNeeded)
	assessment.SuggestedHoldHours = sameExchangeSuggestedHoldHours(asOf.UnixMilli(), assessment.SuggestedFundingTimeMs, intervalHours, eventsNeeded)
	assessment.SuggestedGrossFundingPNL = round2(grossPerEventPNL * float64(eventsNeeded))
	assessment.SuggestedNetPNL = round2(assessment.SuggestedGrossFundingPNL - totalCostsPNL)
}

func sameExchangeLongHoldMaxRecommendationEvents(cfg Config, intervalHours int) int {
	if intervalHours <= 0 {
		return 0
	}
	lookback := cfg.FundingRateHistoryLookback
	if lookback <= 0 {
		lookback = cfg.FundingHistoryLookback
	}
	if lookback <= 0 {
		lookback = 30 * 24 * time.Hour
	}
	events := int(math.Ceil(lookback.Hours() / float64(intervalHours)))
	if events < 1 {
		events = 1
	}
	if events > 180 {
		events = 180
	}
	return events
}

func sameExchangeSuggestedFundingTimeMs(asOfMs, nextFundingTimeMs int64, intervalHours, events int) int64 {
	if intervalHours <= 0 || events <= 0 {
		return 0
	}
	intervalMs := int64(time.Duration(intervalHours) * time.Hour / time.Millisecond)
	if nextFundingTimeMs > asOfMs {
		return nextFundingTimeMs + int64(events-1)*intervalMs
	}
	return asOfMs + int64(events)*intervalMs
}

func sameExchangeSuggestedHoldHours(asOfMs, fundingTimeMs int64, intervalHours, events int) float64 {
	if fundingTimeMs > asOfMs {
		return float64(fundingTimeMs-asOfMs) / float64(time.Hour/time.Millisecond)
	}
	if intervalHours <= 0 || events <= 0 {
		return 0
	}
	return float64(intervalHours * events)
}

func sameExchangeShouldUseHistoricalLongHoldEstimate(cfg Config, currentNetPNL float64, assessment sameExchangeLongHoldAssessment) bool {
	return currentNetPNL < cfg.MinNetPNL &&
		assessment.Eligible &&
		assessment.SuggestedFundingEvents > 0 &&
		assessment.SuggestedNetPNL >= cfg.MinNetPNL
}

func sameExchangeHistoricalLongHoldProjection(base fundingProjection, assessment sameExchangeLongHoldAssessment) fundingProjection {
	projection := base
	if assessment.SuggestedFundingEvents <= 0 {
		return projection
	}
	projection.ProjectedFundingTimeMs = assessment.SuggestedFundingTimeMs
	if assessment.SuggestedHoldHours > 0 {
		projection.FundingWindowHours = assessment.SuggestedHoldHours
	}
	projection.LongFundingEventCount = assessment.SuggestedFundingEvents
	projection.ShortFundingEventCount = assessment.SuggestedFundingEvents
	projection.CarryRate = assessment.EstimatedEventRate * float64(assessment.SuggestedFundingEvents)
	if projection.FundingWindowHours > 0 {
		projection.CarryRateHourlyEquivalent = projection.CarryRate / projection.FundingWindowHours
	}
	projection.ComputationMode = "same_exchange_long_hold_history"
	projection.PathEndReason = "same_exchange_long_hold_history"
	return projection
}

func applySameExchangeHistoricalProjectionDetails(rows []entity.OpportunityProjection, projection fundingProjection, grossFundingPNL, netExpectedPNL, notional float64) []entity.OpportunityProjection {
	out := make([]entity.OpportunityProjection, 0, len(rows)+1)
	for _, row := range rows {
		row.IsBestProjection = false
		out = append(out, row)
	}
	netExpectedBps := 0.0
	if notional > 0 {
		netExpectedBps = netExpectedPNL / notional * 10000
	}
	historyProjection := entity.OpportunityProjection{
		ProjectionRank:               1,
		IsBestProjection:             true,
		ProjectedFundingTimeMs:       projection.ProjectedFundingTimeMs,
		RequiredEntryByFundingTimeMs: projection.RequiredEntryByFundingTimeMs,
		LongFundingEventCount:        projection.LongFundingEventCount,
		ShortFundingEventCount:       projection.ShortFundingEventCount,
		FundingWindowHours:           projection.FundingWindowHours,
		CarryRate:                    projection.CarryRate,
		CarryRateHourlyEquivalent:    projection.CarryRateHourlyEquivalent,
		GrossFundingPNL:              grossFundingPNL,
		NetExpectedPNL:               netExpectedPNL,
		NetExpectedBps:               netExpectedBps,
		ComputationMode:              projection.ComputationMode,
		StrategyMode:                 projection.StrategyMode,
		IncludedSegmentCount:         projection.IncludedSegmentCount,
		NextReviewTimeMs:             projection.NextReviewTimeMs,
		SyncBoundaryTimeMs:           projection.SyncBoundaryTimeMs,
		PathEndReason:                projection.PathEndReason,
	}
	return append([]entity.OpportunityProjection{historyProjection}, out...)
}
