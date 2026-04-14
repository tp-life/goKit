package service

import (
	"crypto/sha1"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

// buildExecutionPlans 只为“满足当前执行阈值”的候选生成 plan。
// 也就是说：
// - opportunities = 全部候选
// - execution_plans = 允许实盘执行的子集
func (r *StrategyRunner) buildExecutionPlans(now time.Time, opportunityBatchID string, opportunities []entity.Opportunity) []entity.ExecutionPlan {
	plans := make([]entity.ExecutionPlan, 0, len(opportunities))

	for _, opp := range opportunities {
		if !r.isOpportunityEligible(now, opp) {
			continue
		}

		longBook, okLongBook := r.store.LatestBookTop(opp.LongExchange, opp.Symbol)
		shortBook, okShortBook := r.store.LatestBookTop(opp.ShortExchange, opp.Symbol)
		longMeta, okLongMeta := r.store.Symbol(opp.LongExchange, opp.Symbol)
		shortMeta, okShortMeta := r.store.Symbol(opp.ShortExchange, opp.Symbol)
		if !(okLongBook && okShortBook && okLongMeta && okShortMeta) {
			continue
		}

		plan := entity.ExecutionPlan{
			OpportunityBatchID:           opportunityBatchID,
			Symbol:                       opp.Symbol,
			Status:                       "watching",
			RollingGroupKey:              buildRollingGroupKey(opp.Symbol, opp.LongExchange, opp.ShortExchange),
			LongExchange:                 opp.LongExchange,
			ShortExchange:                opp.ShortExchange,
			LongVenueSymbol:              longMeta.VenueSymbol,
			ShortVenueSymbol:             shortMeta.VenueSymbol,
			LongSide:                     "BUY",
			ShortSide:                    "SELL",
			EntryMode:                    r.cfg.EntryMode,
			ExitMode:                     r.cfg.ExitMode,
			TargetLeverage:               r.cfg.Leverage,
			CapitalAllocatedUSDT:         round2(r.cfg.TotalCapitalUSDT * r.cfg.CapitalUtilization),
			TargetNotionalUSDT:           round2(r.cfg.EffectiveNotional()),
			FundingCarryPNL:              opp.GrossFundingPNL,
			EntryFeePNL:                  opp.EntryFeePNL,
			ExitFeePNL:                   opp.ExitFeePNL,
			SlippagePNL:                  opp.SlippagePNL,
			SafetyBufferPNL:              opp.SafetyBufferPNL,
			EntryPenaltyBps:              opp.EntryPenaltyBps,
			ExitPenaltyBps:               opp.ExitPenaltyBps,
			HedgePenaltyBps:              opp.HedgePenaltyBps,
			ExecutionPenaltyBps:          opp.ExecutionPenaltyBps,
			ExecutionPenaltyModel:        opp.ExecutionPenaltyModel,
			ExecutionPenaltyBucket:       opp.ExecutionPenaltyBucket,
			NetExpectedPNL:               opp.NetExpectedPNL,
			NetExpectedPNLBps:            opp.NetExpectedBps,
			Score:                        opp.Score,
			EarliestFundingTimeMs:        opp.EarliestFundingTimeMs,
			LatestFundingTimeMs:          opp.LatestFundingTimeMs,
			ProjectedFundingTimeMs:       opp.ProjectedFundingTimeMs,
			RequiredEntryByFundingTimeMs: opp.RequiredEntryByFundingTimeMs,
			LongFundingEventCount:        opp.LongFundingEventCount,
			ShortFundingEventCount:       opp.ShortFundingEventCount,
			FundingWindowHours:           opp.FundingWindowHours,
			ArbitrageMode:                normalizeArbitrageMode(r.cfg.ArbitrageMode, ArbitrageModeCrossExchange),
			StrategyMode:                 normalizeStrategyMode(opp.StrategyMode, StrategyModeLegacyProjection),
			FundingComputationMode:       opp.FundingComputationMode,
			NextReviewTimeMs:             opp.NextReviewTimeMs,
			SyncBoundaryTimeMs:           opp.SyncBoundaryTimeMs,
			EntryPathSegmentCount:        opp.EntryPathSegmentCount,
			EntryPathStopReason:          opp.EntryPathStopReason,
			AsOfTimeMs:                   now.UnixMilli(),
		}

		// 机会层更关心“收益在什么时候兑现”，执行层则更关心“何时必须开仓/复核”。
		// 因此这里先算出 entry / exit 的通用时间锚，再按 strategy mode 调整 target close。
		entryAnchor := opp.RequiredEntryByFundingTimeMs
		if entryAnchor <= 0 {
			entryAnchor = opp.EarliestFundingTimeMs
		}
		exitAnchor := opp.ProjectedFundingTimeMs
		if exitAnchor <= 0 {
			exitAnchor = opp.LatestFundingTimeMs
		}

		plan.EntryWindowOpenMs = entryAnchor - r.cfg.EntryLeadTime.Milliseconds()
		plan.EntryWindowCloseMs = entryAnchor - r.cfg.EntryCutoffTime.Milliseconds()
		plan.TargetCloseTimeMs = exitAnchor + r.cfg.Execution.CloseGracePeriod.Milliseconds()
		if plan.StrategyMode == StrategyModeRollingCycleAligned && plan.NextReviewTimeMs > 0 {
			// rolling 模式的首个 execution target 不是“最终预测收益结束点”，
			// 而是“下一次必须重新 review 的结算点”。
			//
			// 这样开仓后系统会在最近 settlement 处重新判断：
			// - 继续持有
			// - 平仓
			// - 方向反转后翻仓
			//
			// 机会页里展示的 ProjectedFundingTimeMs 仍然保留 entry path 的收益终点，
			// 但执行记录上的 TargetCloseTimeMs 会优先锚到 NextReviewTimeMs。
			plan.TargetCloseTimeMs = plan.NextReviewTimeMs + r.cfg.RollingReviewSettleGracePeriod.Milliseconds()
		}

		plan.LongEntryPrice = round6(longBook.AskPrice)
		plan.ShortEntryPrice = round6(shortBook.BidPrice)
		plan.CrossVenueBasisBps = round4(crossVenueBasisBps(longBook.AskPrice, shortBook.BidPrice))

		plan.LongMinQty = parseFloat(longMeta.MinQty)
		plan.ShortMinQty = parseFloat(shortMeta.MinQty)
		plan.LongMinNotionalUSDT = parseFloat(longMeta.MinNotional)
		plan.ShortMinNotionalUSDT = parseFloat(shortMeta.MinNotional)

		if plan.LongEntryPrice <= 0 || plan.ShortEntryPrice <= 0 {
			continue
		}
		if plan.ArbitrageMode == ArbitrageModeSameExchangeSpotPerp {
			plan.TargetLeverage = sameExchangePerpLeverage(r.cfg)
			perpRule := opp.ShortFundingRule
			if isPerpetualSymbol(longMeta) && !isPerpetualSymbol(shortMeta) {
				perpRule = opp.LongFundingRule
			}
			plan.PerpFundingRank = perpRule.CurrentFundingRank
			plan.PerpFundingRankTotal = perpRule.CurrentFundingRankTotal
			plan.PerpFundingRankPercentile = perpRule.CurrentFundingRankPercentile
			plan.PerpFundingHistorySampleCount = perpRule.HistorySampleCount
			plan.PerpFundingHistoryMeanRate = perpRule.HistoryMeanRate
			plan.PerpFundingHistoryNegativeRatio = perpRule.HistoryNegativeRatio
			plan.PerpFundingHistoryPositiveRatio = perpRule.HistoryPositiveRatio
			plan.PerpFundingHistoricalSupportRatio = perpRule.HistoricalSupportRatio
			plan.PerpFundingCurrentHistoricalPercentile = perpRule.CurrentHistoricalPercentile
			plan.PerpFundingEstimatedEventRate = perpRule.EstimatedEventRate
			plan.PerpFundingEstimatedAnnualizedCarryRate = perpRule.EstimatedAnnualizedCarryRate
			plan.PerpFundingEstimatedAnnualizedNetRate = perpRule.EstimatedAnnualizedNetRate
			plan.SameExchangeLongHoldEligible = perpRule.LongHoldEligible
			plan.SameExchangeLongHoldReason = perpRule.LongHoldReason
			plan.SameExchangeLongHoldUsingHistoryEstimate = opp.SameExchangeLongHoldUsingHistoryEstimate
			plan.SameExchangeLongHoldSuggestedFundingEvents = perpRule.SuggestedFundingEvents
			plan.SameExchangeLongHoldSuggestedHoldHours = perpRule.SuggestedHoldHours
			plan.SameExchangeLongHoldSuggestedFundingTimeMs = perpRule.SuggestedFundingTimeMs
			plan.SameExchangeLongHoldSuggestedGrossFundingPNL = perpRule.SuggestedGrossFundingPNL
			plan.SameExchangeLongHoldSuggestedNetPNL = perpRule.SuggestedNetPNL
			plan.SameExchangePriceRiskAllowed = opp.SameExchangePriceRiskAllowed
			plan.SameExchangePriceRiskReason = opp.SameExchangePriceRiskReason
			plan.SameExchangePriceShockCurrentMarkPrice = opp.SameExchangePriceShockCurrentMarkPrice
			plan.SameExchangePriceShockBaselineMarkPrice = opp.SameExchangePriceShockBaselineMarkPrice
			plan.SameExchangePriceShockRatio = opp.SameExchangePriceShockRatio
			if sameExchangeBasisAssessmentPresent(
				opp.SameExchangeBasisReason,
				opp.SameExchangeBasisUsesPaybackModel,
				opp.SameExchangeBasisCostBps,
				opp.SameExchangeBasisCarryPerEventBps,
				opp.SameExchangeBasisPaybackFundingEvents,
				opp.SameExchangeBasisRiskSizeMultiplier,
			) {
				plan.SameExchangeBasisUsesPaybackModel = opp.SameExchangeBasisUsesPaybackModel
				plan.SameExchangeBasisCostBps = opp.SameExchangeBasisCostBps
				plan.SameExchangeBasisCarryPerEventBps = opp.SameExchangeBasisCarryPerEventBps
				plan.SameExchangeBasisPaybackFundingEvents = opp.SameExchangeBasisPaybackFundingEvents
				plan.SameExchangeBasisAllowed = opp.SameExchangeBasisAllowed
				plan.SameExchangeBasisReason = opp.SameExchangeBasisReason
				plan.SameExchangeBasisRiskSizeMultiplier = opp.SameExchangeBasisRiskSizeMultiplier
			}
			if multiplier := sameExchangeFundingRiskSizeMultiplier(r.cfg, plan); multiplier > 0 && multiplier < 1 {
				plan.CapitalAllocatedUSDT = round2(plan.CapitalAllocatedUSDT * multiplier)
				plan.TargetNotionalUSDT = round2(plan.TargetNotionalUSDT * multiplier)
			}
		}
		maxAllowedBasisBps := r.allowedPlanBasisThresholdBps(opp)
		if plan.ArbitrageMode == ArbitrageModeSameExchangeSpotPerp {
			if !sameExchangeBasisAssessmentPresent(
				plan.SameExchangeBasisReason,
				plan.SameExchangeBasisUsesPaybackModel,
				plan.SameExchangeBasisCostBps,
				plan.SameExchangeBasisCarryPerEventBps,
				plan.SameExchangeBasisPaybackFundingEvents,
				plan.SameExchangeBasisRiskSizeMultiplier,
			) {
				assessmentNotional := r.cfg.EffectiveNotional()
				if assessmentNotional <= 0 {
					assessmentNotional = plan.TargetNotionalUSDT
				}
				assessment := assessSameExchangeBasisRisk(
					r.cfg,
					plan.ArbitrageMode,
					plan.FundingWindowHours,
					rateFromPNL(opp.GrossFundingPNL, assessmentNotional),
					plan.LongFundingEventCount,
					plan.ShortFundingEventCount,
					plan.CrossVenueBasisBps,
					opp.EntryFeePNL+opp.ExitFeePNL+opp.SlippagePNL+opp.SafetyBufferPNL,
					assessmentNotional,
					maxAllowedBasisBps,
				)
				applySameExchangeBasisAssessmentToPlan(&plan, assessment)
			}
			if multiplier := plan.SameExchangeBasisRiskSizeMultiplier; multiplier > 0 && multiplier < 1 {
				plan.CapitalAllocatedUSDT = round2(plan.CapitalAllocatedUSDT * multiplier)
				plan.TargetNotionalUSDT = round2(plan.TargetNotionalUSDT * multiplier)
			}
		} else if math.Abs(plan.CrossVenueBasisBps) > maxAllowedBasisBps {
			continue
		}
		if plan.TargetNotionalUSDT <= 0 || plan.CapitalAllocatedUSDT <= 0 {
			continue
		}

		longQty, shortQty, longNotional, shortNotional, ok := computeCommonLegQuantities(
			plan.TargetNotionalUSDT,
			plan.LongEntryPrice,
			plan.ShortEntryPrice,
			longMeta.StepSize,
			shortMeta.StepSize,
		)
		if !ok {
			continue
		}
		plan.LongQty = round8(longQty)
		plan.ShortQty = round8(shortQty)
		if plan.LongQty <= 0 || plan.ShortQty <= 0 {
			continue
		}
		if plan.LongQty < plan.LongMinQty || plan.ShortQty < plan.ShortMinQty {
			continue
		}

		longNotional = plan.LongQty * plan.LongEntryPrice
		shortNotional = plan.ShortQty * plan.ShortEntryPrice
		roundedNotional := math.Min(longNotional, shortNotional)
		plan.RoundedNotionalUSDT = round2(roundedNotional)
		if longNotional < plan.LongMinNotionalUSDT || shortNotional < plan.ShortMinNotionalUSDT {
			continue
		}
		r.applyScaledPlanEconomics(&plan, opp, roundedNotional)
		refreshSameExchangePlanLongHoldAssessment(r.cfg, &plan)
		if plan.NetExpectedPNL < r.cfg.MinNetPNL {
			continue
		}
		if plan.ArbitrageMode == ArbitrageModeSameExchangeSpotPerp &&
			r.cfg.SameExchangeRequireLongHoldEligible &&
			!plan.SameExchangeLongHoldEligible {
			continue
		}

		plan.ReadyNow = now.UnixMilli() >= plan.EntryWindowOpenMs && now.UnixMilli() <= plan.EntryWindowCloseMs
		switch {
		case plan.ReadyNow:
			plan.Status = "ready"
		case now.UnixMilli() > plan.EntryWindowCloseMs:
			plan.Status = "late"
		case now.UnixMilli() < plan.EntryWindowOpenMs:
			plan.Status = "watching"
		default:
			plan.Status = "watching"
		}

		plan.PlanKey = buildPlanKey(plan)
		plans = append(plans, plan)
	}

	return plans
}

func (r *StrategyRunner) allowedPlanBasisThresholdBps(opp entity.Opportunity) float64 {
	if opp.MaxAllowedBasisBps > 0 {
		return opp.MaxAllowedBasisBps
	}
	return r.allowedBasisThresholdBps(fundingProjection{FundingWindowHours: opp.FundingWindowHours})
}

// isOpportunityEligible 决定一个候选是否允许进入 execution_plans。
// 关键点：
// 1. 先尊重 computeCandidates() 已经算出的最终结论；
// 2. stale_data / basis_too_wide / outside_entry_window 这类候选，不能再绕过状态检查进入计划；
// 3. 下面的净收益和截止时间检查只是兜底，避免后续逻辑在边界情况下漏拦截。
func (r *StrategyRunner) isOpportunityEligible(now time.Time, opp entity.Opportunity) bool {
	if !opp.EligibleForExecution {
		return false
	}
	if strings.ToLower(strings.TrimSpace(opp.Status)) != OpportunityStatusEligible {
		return false
	}
	if normalizeArbitrageMode(r.cfg.ArbitrageMode, ArbitrageModeCrossExchange) == ArbitrageModeSameExchangeSpotPerp &&
		r.cfg.SameExchangeRequireLongHoldEligible {
		perpRule := opp.ShortFundingRule
		if isSpotFundingRule(perpRule) {
			perpRule = opp.LongFundingRule
		}
		if !perpRule.LongHoldEligible {
			return false
		}
	}
	if opp.NetExpectedPNL < r.cfg.MinNetPNL {
		return false
	}
	entryAnchor := opp.RequiredEntryByFundingTimeMs
	if entryAnchor <= 0 {
		entryAnchor = opp.EarliestFundingTimeMs
	}
	if entryAnchor > 0 {
		closeMs := entryAnchor - r.cfg.EntryCutoffTime.Milliseconds()
		if now.UnixMilli() > closeMs {
			return false
		}
	}
	return true
}

// crossVenueBasisBps 用 long 侧 ask 和 short 侧 bid 粗略估算跨平台 basis。
func crossVenueBasisBps(longAsk, shortBid float64) float64 {
	mid := (longAsk + shortBid) / 2
	if mid <= 0 {
		return 0
	}
	return (shortBid - longAsk) / mid * 10000
}

// isFresh 用来判断行情快照是否足够新鲜。
func isFresh(eventTimeMs int64, maxAge time.Duration, now time.Time) bool {
	if eventTimeMs <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(eventTimeMs))
	return age >= 0 && age <= maxAge
}

func parseFloat(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}

// roundDownStep 按交易所 step size 向下取整。
// 例如 step=0.001，则 1.23456 -> 1.234
func roundDownStep(value float64, stepRaw string) float64 {
	step := parseFloat(stepRaw)
	if step <= 0 {
		return value
	}
	return math.Floor(value/step) * step
}

// roundPriceToTick 按 tick size 对限价做方向敏感的取整。
//
// 这里不能简单用 round/floor：
// - BUY 单如果向下取整，可能把本来“足够激进”的价格压低，导致 IOC/LIMIT 不成交；
// - SELL 单如果向上取整，也会把价格抬高，降低成交概率。
//
// 因此这里使用：
// - BUY -> 向上取整到最近 tick；
// - SELL -> 向下取整到最近 tick。
func roundPriceToTick(value float64, tickRaw, side string) float64 {
	tick := parseFloat(tickRaw)
	if value <= 0 || tick <= 0 {
		return value
	}
	scaled := value / tick
	if strings.EqualFold(side, "BUY") {
		return math.Ceil(scaled-1e-12) * tick
	}
	return math.Floor(scaled+1e-12) * tick
}

func buildPlanKey(plan entity.ExecutionPlan) string {
	raw := strings.Join([]string{
		strings.ToUpper(plan.Symbol),
		strings.ToLower(plan.LongExchange),
		strings.ToLower(plan.ShortExchange),
		strings.ToUpper(plan.LongVenueSymbol),
		strings.ToUpper(plan.ShortVenueSymbol),
		strconv.FormatInt(plan.EarliestFundingTimeMs, 10),
		strconv.FormatInt(plan.LatestFundingTimeMs, 10),
	}, "|")
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// buildRollingGroupKey 把“同一币种、同一交易所对”的 rolling 计划归到同一组。
//
// 它刻意不编码当前方向，因此：
// - Long A / Short B
// - Long B / Short A
// 得到的 group key 一样。
//
// 这能让执行层在同一时刻只允许一条 rolling group 持仓存活，
// 避免方向切换时出现同组双开。
func buildRollingGroupKey(symbol, leftExchange, rightExchange string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	left := strings.ToLower(strings.TrimSpace(leftExchange))
	right := strings.ToLower(strings.TrimSpace(rightExchange))
	if left > right {
		left, right = right, left
	}
	return strings.Join([]string{symbol, left, right}, "|")
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func round6(v float64) float64 { return math.Round(v*1_000_000) / 1_000_000 }
func round8(v float64) float64 { return math.Round(v*100_000_000) / 1_000_000_00 }

func computeCommonLegQuantities(targetNotionalUSDT, longPrice, shortPrice float64, longStepSize, shortStepSize string) (longQty, shortQty, longNotional, shortNotional float64, ok bool) {
	if targetNotionalUSDT <= 0 || longPrice <= 0 || shortPrice <= 0 {
		return 0, 0, 0, 0, false
	}

	longQty = roundDownStep(targetNotionalUSDT/longPrice, longStepSize)
	shortQty = roundDownStep(targetNotionalUSDT/shortPrice, shortStepSize)
	if longQty <= 0 || shortQty <= 0 {
		return 0, 0, 0, 0, false
	}

	commonNotional := math.Min(longQty*longPrice, shortQty*shortPrice)
	if commonNotional <= 0 {
		return 0, 0, 0, 0, false
	}

	longQty = roundDownStep(commonNotional/longPrice, longStepSize)
	shortQty = roundDownStep(commonNotional/shortPrice, shortStepSize)
	if longQty <= 0 || shortQty <= 0 {
		return 0, 0, 0, 0, false
	}

	longNotional = longQty * longPrice
	shortNotional = shortQty * shortPrice
	commonNotional = math.Min(longNotional, shortNotional)
	if commonNotional <= 0 {
		return 0, 0, 0, 0, false
	}

	// 再做一轮以共同名义价值为锚的 round-down，尽量缩小双腿名义偏差。
	longQty = roundDownStep(commonNotional/longPrice, longStepSize)
	shortQty = roundDownStep(commonNotional/shortPrice, shortStepSize)
	longNotional = longQty * longPrice
	shortNotional = shortQty * shortPrice
	return longQty, shortQty, longNotional, shortNotional, longQty > 0 && shortQty > 0
}

func (r *StrategyRunner) applyScaledPlanEconomics(plan *entity.ExecutionPlan, opp entity.Opportunity, actualNotional float64) {
	if plan == nil || actualNotional <= 0 {
		return
	}

	baseNotional := plan.TargetNotionalUSDT
	if baseNotional <= 0 {
		baseNotional = r.cfg.EffectiveNotional()
	}
	if baseNotional <= 0 {
		return
	}

	scale := actualNotional / baseNotional
	plan.FundingCarryPNL = round2(opp.GrossFundingPNL * scale)
	plan.EntryFeePNL = round2(opp.EntryFeePNL * scale)
	plan.ExitFeePNL = round2(opp.ExitFeePNL * scale)
	plan.SlippagePNL = round2(actualNotional * (opp.EntryPenaltyBps + opp.ExitPenaltyBps) / 10000)
	plan.SafetyBufferPNL = round2(r.cfg.SafetyBufferUSDT + actualNotional*opp.HedgePenaltyBps/10000)
	plan.NetExpectedPNL = round2(plan.FundingCarryPNL - plan.EntryFeePNL - plan.ExitFeePNL - plan.SlippagePNL - plan.SafetyBufferPNL)
	plan.NetExpectedPNLBps = 0
	if actualNotional > 0 {
		plan.NetExpectedPNLBps = round4(plan.NetExpectedPNL / actualNotional * 10000)
	}
	plan.Score = r.scoreOpportunity(plan.NetExpectedPNL, opp.GrossEdgeHourly, plan.CrossVenueBasisBps)
}
