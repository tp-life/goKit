package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"goKit/internal/domain/entity"
)

type executionCloseDecision struct {
	shouldClose bool
	trigger     string
	reason      string
}

// revalidatePlanBeforeOpen 在真正发单前，重新用“当前市场事实”审一遍计划。
//
// 这层校验的核心目的，是补上 plan 生成和真实发单之间的时间差：
// 1. plan 生成时的 `ReadyNow` 只是当时的快照，不保证几秒后、几十秒后还成立；
// 2. funding / book / basis / 时间窗 都可能在这段时间里变化；
// 3. 如果这里不重审，系统就可能基于“生成计划那一刻还合理”的旧事实去开仓。
//
// 这不是为了在 execution 层完全复制一份 StrategyRunner，
// 而是为了给真实发单加一道最后的安全门：
// - 数据仍然新鲜；
// - 当前固定方向的 funding carry 仍然为正；
// - 当前 basis 没有明显恶化；
// - 当前预估净收益仍然过线；
// - 当前依然在入场时间窗内。
func (s *ExecutionService) revalidatePlanBeforeOpen(now time.Time, plan *entity.ExecutionPlan) error {
	if plan == nil {
		return fmt.Errorf("nil execution plan")
	}

	longMeta, okLongMeta := s.store.Symbol(plan.LongExchange, plan.Symbol)
	shortMeta, okShortMeta := s.store.Symbol(plan.ShortExchange, plan.Symbol)
	longFunding, okLongFunding := s.store.LatestFunding(plan.LongExchange, plan.Symbol)
	shortFunding, okShortFunding := s.store.LatestFunding(plan.ShortExchange, plan.Symbol)
	longBook, okLongBook := s.store.LatestBookTop(plan.LongExchange, plan.Symbol)
	shortBook, okShortBook := s.store.LatestBookTop(plan.ShortExchange, plan.Symbol)
	if !(okLongMeta && okShortMeta && okLongFunding && okShortFunding && okLongBook && okShortBook) {
		return fmt.Errorf("open revalidation: missing latest symbol/funding/book snapshot for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}

	if isSnapshotStaleForConfig(s.cfg, now, longFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(s.cfg, now, shortFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(s.cfg, now, longBook.EventTimeMs) ||
		isSnapshotStaleForConfig(s.cfg, now, shortBook.EventTimeMs) {
		return fmt.Errorf("open revalidation: market data is stale for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}

	currentProjection, ok := projectFundingCarryFromCurrentSnapshots(s.cfg, now, longFunding, shortFunding)
	if !ok || currentProjection.CarryRate <= 0 {
		return fmt.Errorf("open revalidation: current funding carry is no longer positive for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	if !isWithinEntryWindowForConfig(now, currentProjection.RequiredEntryByFundingTimeMs, s.cfg.EntryLeadTime, s.cfg.EntryCutoffTime) {
		return fmt.Errorf("open revalidation: current opportunity is outside entry window for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}

	currentBasisBps := crossVenueBasisBps(longBook.AskPrice, shortBook.BidPrice)
	maxAllowedBasisBps := allowedBasisThresholdBpsForConfig(s.cfg, currentProjection.FundingWindowHours)
	if math.Abs(currentBasisBps) > maxAllowedBasisBps {
		return fmt.Errorf("open revalidation: current basis %.4f bps exceeds dynamic limit %.4f bps", currentBasisBps, maxAllowedBasisBps)
	}

	currentLongNotional := plan.LongQty * longBook.AskPrice
	currentShortNotional := plan.ShortQty * shortBook.BidPrice
	if currentLongNotional <= 0 || currentShortNotional <= 0 {
		return fmt.Errorf("open revalidation: invalid current notional for %s", plan.Symbol)
	}
	if currentLongNotional < parseFloat(longMeta.MinNotional) || currentShortNotional < parseFloat(shortMeta.MinNotional) {
		return fmt.Errorf("open revalidation: current rounded notional falls below venue minimum for %s", plan.Symbol)
	}

	currentNotional := math.Min(currentLongNotional, currentShortNotional)
	currentGrossFundingPNL := currentNotional * currentProjection.CarryRate
	currentNetExpectedPNL := currentGrossFundingPNL - plan.EntryFeePNL - plan.ExitFeePNL - plan.SlippagePNL - plan.SafetyBufferPNL
	if currentNetExpectedPNL < s.cfg.MinNetPNL {
		return fmt.Errorf("open revalidation: current net pnl %.4f below minimum %.4f", currentNetExpectedPNL, s.cfg.MinNetPNL)
	}
	return nil
}

// evaluateCloseDecision 统一回答：
// “这条已打开的 execution 现在是不是应该自动平仓？”
//
// 它把两类触发源并排放在一起：
// 1. schedule close：到达原计划的 target close time；
// 2. safety close：浮亏、跨所基差或保证金缓冲恶化到不可接受水平。
//
// 这样 runAutoClose 就不必再散落多套 if/switch，
// 也让后续继续扩展新的退出条件时有明确入口。
func (s *ExecutionService) evaluateCloseDecision(ctx context.Context, now time.Time, rec entity.ExecutionRecord, plan *entity.ExecutionPlan) (executionCloseDecision, error) {
	if plan == nil {
		return executionCloseDecision{}, fmt.Errorf("nil execution plan")
	}
	// open_partial_failed / open_hedging 代表这条计划可能已经留下了单腿风险。
	// 这类状态不应该继续静置到 target close time，而应该尽快触发一轮 recovery close。
	if rec.LiveTrading && requiresRecoveryClose(rec.Status) {
		return executionCloseDecision{
			shouldClose: true,
			trigger:     "auto_recovery",
			reason:      fmt.Sprintf("execution status %s requires recovery close", normalizeExecutionStatus(rec.Status)),
		}, nil
	}
	if rec.TargetCloseTimeMs > 0 && now.UnixMilli() >= rec.TargetCloseTimeMs {
		return executionCloseDecision{
			shouldClose: true,
			trigger:     "auto_schedule",
			reason:      "target close time reached",
		}, nil
	}
	if !rec.LiveTrading {
		return executionCloseDecision{}, nil
	}

	if decision, ok, err := s.evaluateMarkToMarketGuard(now, plan); err != nil {
		return executionCloseDecision{}, err
	} else if ok {
		return decision, nil
	}
	if decision, ok, err := s.evaluateBasisGuard(now, plan); err != nil {
		return executionCloseDecision{}, err
	} else if ok {
		return decision, nil
	}
	if decision, ok, err := s.evaluateBalanceGuard(ctx, plan); err != nil {
		return executionCloseDecision{}, err
	} else if ok {
		return decision, nil
	}
	return executionCloseDecision{}, nil
}

// evaluateMarkToMarketGuard 用当前可成交方向的盘口，保守估算“如果此刻被迫平仓，会亏多少”。
//
// 这里使用：
// - long leg 用 bid（卖出能拿到的价格）
// - short leg 用 ask（买回需要付出的价格）
//
// 这样得到的是“偏保守的可实现盯市盈亏”，比单纯看 mark price 更适合作为止损触发依据。
func (s *ExecutionService) evaluateMarkToMarketGuard(now time.Time, plan *entity.ExecutionPlan) (executionCloseDecision, bool, error) {
	threshold := s.cfg.Execution.MaxUnrealizedLossUSDT
	if threshold <= 0 {
		return executionCloseDecision{}, false, nil
	}
	longBook, okLong := s.store.LatestBookTop(plan.LongExchange, plan.Symbol)
	shortBook, okShort := s.store.LatestBookTop(plan.ShortExchange, plan.Symbol)
	if !(okLong && okShort) {
		return executionCloseDecision{}, false, fmt.Errorf("close guard mtm: missing latest book snapshot for %s", plan.Symbol)
	}
	if isSnapshotStaleForConfig(s.cfg, now, longBook.EventTimeMs) || isSnapshotStaleForConfig(s.cfg, now, shortBook.EventTimeMs) {
		return executionCloseDecision{}, false, fmt.Errorf("close guard mtm: stale book snapshot for %s", plan.Symbol)
	}

	mtmPNL := markToMarketClosePNL(plan, longBook, shortBook)
	if mtmPNL > -threshold {
		return executionCloseDecision{}, false, nil
	}
	return executionCloseDecision{
		shouldClose: true,
		trigger:     "auto_drawdown_guard",
		reason:      fmt.Sprintf("mark-to-market pnl %.4f <= -%.4f", mtmPNL, threshold),
	}, true, nil
}

// evaluateBasisGuard 关注的是“当前如果要解这组对冲仓，跨所价差是否已经恶化过头”。
//
// 这里使用 close-side basis：
// - long leg 平仓看 bid；
// - short leg 平仓看 ask。
//
// 这比继续沿用开仓时的 ask/bid 组合更接近真实的退出摩擦。
func (s *ExecutionService) evaluateBasisGuard(now time.Time, plan *entity.ExecutionPlan) (executionCloseDecision, bool, error) {
	threshold := s.cfg.Execution.MaxUnwindBasisBps
	if threshold <= 0 {
		return executionCloseDecision{}, false, nil
	}
	longBook, okLong := s.store.LatestBookTop(plan.LongExchange, plan.Symbol)
	shortBook, okShort := s.store.LatestBookTop(plan.ShortExchange, plan.Symbol)
	if !(okLong && okShort) {
		return executionCloseDecision{}, false, fmt.Errorf("close guard basis: missing latest book snapshot for %s", plan.Symbol)
	}
	if isSnapshotStaleForConfig(s.cfg, now, longBook.EventTimeMs) || isSnapshotStaleForConfig(s.cfg, now, shortBook.EventTimeMs) {
		return executionCloseDecision{}, false, fmt.Errorf("close guard basis: stale book snapshot for %s", plan.Symbol)
	}

	unwindBasisBps := closeSideBasisBps(longBook.BidPrice, shortBook.AskPrice)
	if unwindBasisBps <= threshold {
		return executionCloseDecision{}, false, nil
	}
	return executionCloseDecision{
		shouldClose: true,
		trigger:     "auto_basis_guard",
		reason:      fmt.Sprintf("close-side basis %.4f bps > max %.4f bps", unwindBasisBps, threshold),
	}, true, nil
}

// evaluateBalanceGuard 负责在仓位已经打开后继续盯账户缓冲。
//
// 开仓前的余额检查只能说明“当时开得起”；
// 但真正持仓期间，保证金占用和浮亏都会变。
// 因此这里额外提供一个“持仓后的最低可用余额比例”护栏，
// 用来在账户缓冲明显恶化时提前退出。
func (s *ExecutionService) evaluateBalanceGuard(ctx context.Context, plan *entity.ExecutionPlan) (executionCloseDecision, bool, error) {
	threshold := s.cfg.Execution.EmergencyMinAvailableBalanceRatio
	if threshold <= 0 {
		return executionCloseDecision{}, false, nil
	}

	for _, exchangeName := range []string{plan.LongExchange, plan.ShortExchange} {
		adapter := s.trades[normalizeVenueName(exchangeName)]
		if adapter == nil || !adapter.Enabled() {
			return executionCloseDecision{}, false, fmt.Errorf("close guard balance: trade adapter %s disabled", exchangeName)
		}
		acct, err := adapter.GetAccountSnapshot(ctx)
		if err != nil {
			s.registerAPIFailure(exchangeName)
			return executionCloseDecision{}, false, fmt.Errorf("close guard balance %s failed: %w", exchangeName, err)
		}
		s.registerAPISuccess(exchangeName)
		if acct.Equity <= 0 {
			continue
		}
		ratio := acct.AvailableBalance / acct.Equity
		if ratio < threshold {
			return executionCloseDecision{
				shouldClose: true,
				trigger:     "auto_balance_guard",
				reason:      fmt.Sprintf("%s available ratio %.4f < emergency minimum %.4f", exchangeName, ratio, threshold),
			}, true, nil
		}
	}
	return executionCloseDecision{}, false, nil
}

func projectFundingCarryFromCurrentSnapshots(cfg Config, now time.Time, longFunding, shortFunding entity.FundingSnapshot) (fundingProjection, bool) {
	decay := normalizedContinuationDecay(cfg.FundingRateContinuationDecay)
	longForecast := fundingForecast{
		CurrentRate:        longFunding.FundingRate,
		BaselineRate:       longFunding.FundingRate,
		HistoryMean:        longFunding.FundingRate,
		Regime:             "spot_only",
		Confidence:         "low",
		MeanReversion:      0.20,
		ContinuationDecay:  decay,
		EffectiveFloorRate: longFunding.FundingRate,
		EffectiveCapRate:   longFunding.FundingRate,
	}
	shortForecast := fundingForecast{
		CurrentRate:        shortFunding.FundingRate,
		BaselineRate:       shortFunding.FundingRate,
		HistoryMean:        shortFunding.FundingRate,
		Regime:             "spot_only",
		Confidence:         "low",
		MeanReversion:      0.20,
		ContinuationDecay:  decay,
		EffectiveFloorRate: shortFunding.FundingRate,
		EffectiveCapRate:   shortFunding.FundingRate,
	}
	projections := buildFundingProjections(now, cfg.HoldHours, longFunding, longForecast, shortFunding, shortForecast)
	return selectBestFundingProjection(projections)
}

func buildFundingProjections(now time.Time, holdHours float64, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) []fundingProjection {
	nowMs := now.UnixMilli()
	candidateTimes := buildFundingCandidateTimes(nowMs, longFunding, shortFunding, holdHours)
	if len(candidateTimes) == 0 {
		return nil
	}

	projections := make([]fundingProjection, 0, len(candidateTimes))
	for _, projectedTime := range candidateTimes {
		longCount := fundingEventCountUntil(nowMs, projectedTime, longFunding.FundingTimeMs, longFunding.FundingIntervalHours)
		shortCount := fundingEventCountUntil(nowMs, projectedTime, shortFunding.FundingTimeMs, shortFunding.FundingIntervalHours)
		if longCount == 0 && shortCount == 0 {
			continue
		}

		shortCarry := projectedLegFundingCarry(shortForecast, shortCount)
		longCarry := projectedLegFundingCarry(longForecast, longCount)
		carryRate := shortCarry - longCarry
		windowHours := float64(projectedTime-nowMs) / float64(time.Hour/time.Millisecond)
		if windowHours <= 0 {
			windowHours = 1.0 / 60.0
		}

		requiredEntryBy := int64(0)
		if longCount > 0 {
			requiredEntryBy = longFunding.FundingTimeMs
		}
		if shortCount > 0 && (requiredEntryBy == 0 || shortFunding.FundingTimeMs < requiredEntryBy) {
			requiredEntryBy = shortFunding.FundingTimeMs
		}

		projections = append(projections, fundingProjection{
			ProjectedFundingTimeMs:       projectedTime,
			RequiredEntryByFundingTimeMs: requiredEntryBy,
			LongFundingEventCount:        longCount,
			ShortFundingEventCount:       shortCount,
			FundingWindowHours:           windowHours,
			CarryRate:                    carryRate,
			CarryRateHourlyEquivalent:    carryRate / windowHours,
			ComputationMode:              "event_based_spot_revalidation",
		})
	}
	return projections
}

func allowedBasisThresholdBpsForConfig(cfg Config, fundingWindowHours float64) float64 {
	base := cfg.MaxSpreadBps
	if base <= 0 {
		base = 12
	}
	multiplier := cfg.DynamicMaxSpreadMultiplier
	if multiplier < 1 {
		multiplier = 1
	}
	refHours := cfg.DynamicMaxSpreadReferenceHours
	if refHours <= 0 {
		refHours = cfg.HoldHours
	}
	if refHours <= 0 {
		return base
	}
	ratio := fundingWindowHours / refHours
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	return base * (1 + (multiplier-1)*ratio)
}

func isWithinEntryWindowForConfig(now time.Time, fundingTimeMs int64, openBefore, closeBefore time.Duration) bool {
	if fundingTimeMs <= 0 {
		return true
	}
	if openBefore <= 0 || closeBefore <= 0 {
		return true
	}
	untilFunding := time.UnixMilli(fundingTimeMs).Sub(now)
	return untilFunding <= openBefore && untilFunding >= closeBefore
}

func isSnapshotStaleForConfig(cfg Config, now time.Time, eventTimeMs int64) bool {
	if eventTimeMs <= 0 {
		return true
	}
	maxAge := cfg.MaxDataAge
	if maxAge <= 0 {
		maxAge = 30 * time.Second
	}
	return now.Sub(time.UnixMilli(eventTimeMs)) > maxAge
}

func markToMarketClosePNL(plan *entity.ExecutionPlan, longBook, shortBook entity.BookTopSnapshot) float64 {
	longPNL := (longBook.BidPrice - plan.LongEntryPrice) * plan.LongQty
	shortPNL := (plan.ShortEntryPrice - shortBook.AskPrice) * plan.ShortQty
	return longPNL + shortPNL
}

func closeSideBasisBps(longBid, shortAsk float64) float64 {
	mid := (longBid + shortAsk) / 2
	if mid <= 0 {
		return 0
	}
	return math.Abs(shortAsk-longBid) / mid * 10000
}
