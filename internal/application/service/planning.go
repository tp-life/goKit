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
			FundingComputationMode:       opp.FundingComputationMode,
			AsOfTimeMs:                   now.UnixMilli(),
		}

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
		maxAllowedBasisBps := r.allowedPlanBasisThresholdBps(opp)
		if math.Abs(plan.CrossVenueBasisBps) > maxAllowedBasisBps {
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
