package service

import (
	"context"
	"fmt"
	"math"
	"strings"
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
func (s *ExecutionService) revalidatePlanBeforeOpen(now time.Time, plan *entity.ExecutionPlan, trigger string) error {
	if plan == nil {
		return fmt.Errorf("nil execution plan")
	}

	guardCfg := s.cfg
	guardCfg.ArbitrageMode = normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange))
	snapshots, ok := loadPairMarketSnapshot(s.store, guardCfg, plan.Symbol, plan.LongExchange, plan.ShortExchange)
	if !ok {
		return fmt.Errorf("open revalidation: missing latest symbol/funding/book snapshot for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	longMeta, shortMeta := snapshots.LongMeta, snapshots.ShortMeta
	longFunding, shortFunding := snapshots.LongFunding, snapshots.ShortFunding
	longBook, shortBook := snapshots.LongBook, snapshots.ShortBook

	if isSnapshotStaleForConfig(guardCfg, now, longFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, shortFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, longBook.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, shortBook.EventTimeMs) {
		return fmt.Errorf("open revalidation: market data is stale for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}

	currentProjection, ok := projectFundingCarryForPlanRevalidation(guardCfg, now, plan, longFunding, shortFunding)
	if !ok || currentProjection.CarryRate <= 0 {
		return fmt.Errorf("open revalidation: current funding carry is no longer positive for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	// rolling 翻仓属于“当前 live 仓位在 review 点内的方向切换”，
	// 它的语义不是普通的“离结算点足够近才允许第一次开仓”。
	// 因此 successor reopen 会保留 funding/basis/余额复核，但跳过通用 entry window 门槛。
	if !shouldBypassRollingEntryWindowRevalidation(*plan, trigger) &&
		!isWithinEntryWindowForConfig(now, currentProjection.RequiredEntryByFundingTimeMs, guardCfg.EntryLeadTime, guardCfg.EntryCutoffTime) {
		return fmt.Errorf("open revalidation: current opportunity is outside entry window for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}

	currentLongNotional := plan.LongQty * longBook.AskPrice
	currentShortNotional := plan.ShortQty * shortBook.BidPrice
	if currentLongNotional <= 0 || currentShortNotional <= 0 {
		return fmt.Errorf("open revalidation: invalid current notional for %s", plan.Symbol)
	}
	currentNotional := math.Min(currentLongNotional, currentShortNotional)
	currentBasisBps := crossVenueBasisBps(longBook.AskPrice, shortBook.BidPrice)
	maxAllowedBasisBps := allowedBasisThresholdBpsForConfig(guardCfg, currentProjection.FundingWindowHours)
	if guardCfg.ArbitrageMode == ArbitrageModeSameExchangeSpotPerp {
		perpExchange := plan.ShortExchange
		perpFunding := shortFunding
		if isPerpetualSymbol(longMeta) && !isPerpetualSymbol(shortMeta) {
			perpExchange = plan.LongExchange
			perpFunding = longFunding
		}
		priceAssessment := assessSameExchangePriceRisk(
			context.Background(),
			guardCfg,
			s.marketRepo,
			plan.ArbitrageMode,
			perpExchange,
			plan.Symbol,
			perpFunding,
			now,
		)
		if !priceAssessment.Allowed {
			return fmt.Errorf("open revalidation: %s", sameExchangePriceRiskRejectReason(guardCfg, priceAssessment))
		}
	} else if math.Abs(currentBasisBps) > maxAllowedBasisBps {
		return fmt.Errorf("open revalidation: current basis %.4f bps exceeds dynamic limit %.4f bps", math.Abs(currentBasisBps), maxAllowedBasisBps)
	}

	if currentLongNotional < parseFloat(longMeta.MinNotional) || currentShortNotional < parseFloat(shortMeta.MinNotional) {
		return fmt.Errorf("open revalidation: current rounded notional falls below venue minimum for %s", plan.Symbol)
	}
	currentGrossFundingPNL := currentNotional * currentProjection.CarryRate
	currentNetExpectedPNL := currentGrossFundingPNL - plan.EntryFeePNL - plan.ExitFeePNL - plan.SlippagePNL - plan.SafetyBufferPNL
	if currentNetExpectedPNL < guardCfg.MinNetPNL {
		return fmt.Errorf("open revalidation: current net pnl %.4f below minimum %.4f", currentNetExpectedPNL, guardCfg.MinNetPNL)
	}
	return nil
}

func shouldBypassRollingEntryWindowRevalidation(plan entity.ExecutionPlan, trigger string) bool {
	return normalizedPlanStrategyMode(plan) == StrategyModeRollingCycleAligned &&
		strings.EqualFold(strings.TrimSpace(trigger), "rolling_flip_reopen")
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
	if rec.LiveTrading && requiresRetryClose(rec.Status) {
		return executionCloseDecision{
			shouldClose: true,
			trigger:     "auto_retry_close",
			reason:      fmt.Sprintf("execution status %s requires close retry", normalizeExecutionStatus(rec.Status)),
		}, nil
	}
	// rolling 模式的“到点”并不等于“直接平仓”：
	// - record.TargetCloseTimeMs 只是下一次 review 的保护性锚点；
	// - 真正到 review 点后，应先由 rolling monitor 决定继续持有 / 平仓 / 翻仓；
	// - 因此这里仅对 legacy 计划保留 schedule close 语义。
	if normalizeStrategyMode(rec.StrategyMode, StrategyModeLegacyProjection) != StrategyModeRollingCycleAligned &&
		rec.TargetCloseTimeMs > 0 && now.UnixMilli() >= rec.TargetCloseTimeMs {
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
	if decision, ok, err := s.evaluateSameExchangeLiquidationGuard(ctx, plan); err != nil {
		return executionCloseDecision{}, err
	} else if ok {
		return decision, nil
	}
	if decision, ok, err := s.evaluateSameExchangePriceShockGuard(ctx, now, plan); err != nil {
		return executionCloseDecision{}, err
	} else if ok {
		return decision, nil
	}
	if decision, ok, err := s.evaluateSameExchangeFundingExitGuard(now, plan); err != nil {
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

	legs := []struct {
		exchange string
		meta     entity.Symbol
	}{
		{exchange: plan.LongExchange},
		{exchange: plan.ShortExchange},
	}
	for i := range legs {
		meta, ok := s.store.Symbol(legs[i].exchange, plan.Symbol)
		if ok {
			legs[i].meta = meta
		}
	}

	for _, leg := range legs {
		exchangeName := leg.exchange
		if isSpotSymbol(leg.meta) {
			continue
		}
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

func (s *ExecutionService) evaluateSameExchangeLiquidationGuard(ctx context.Context, plan *entity.ExecutionPlan) (executionCloseDecision, bool, error) {
	if plan == nil {
		return executionCloseDecision{}, false, nil
	}
	arbitrageMode := normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange))
	if arbitrageMode != ArbitrageModeSameExchangeSpotPerp {
		return executionCloseDecision{}, false, nil
	}
	if s.cfg.SameExchangeReduceLiqDistanceRatio <= 0 && s.cfg.SameExchangeEmergencyLiqDistanceRatio <= 0 {
		return executionCloseDecision{}, false, nil
	}

	longMeta, _ := s.store.Symbol(plan.LongExchange, plan.Symbol)
	shortMeta, _ := s.store.Symbol(plan.ShortExchange, plan.Symbol)
	perpExchange := plan.ShortExchange
	perpVenueSymbol := plan.ShortVenueSymbol
	perpAssetID := shortMeta.VenueAssetID
	if isPerpetualSymbol(longMeta) && !isPerpetualSymbol(shortMeta) {
		perpExchange = plan.LongExchange
		perpVenueSymbol = plan.LongVenueSymbol
		perpAssetID = longMeta.VenueAssetID
	}

	adapter := s.trades[strings.ToLower(strings.TrimSpace(perpExchange))]
	if adapter == nil || !adapter.Enabled() {
		return executionCloseDecision{}, false, fmt.Errorf("close guard liquidation: trade adapter %s disabled", perpExchange)
	}
	pos, err := adapter.GetPosition(ctx, plan.Symbol, perpVenueSymbol, perpAssetID)
	if err != nil {
		s.registerAPIFailure(perpExchange)
		return executionCloseDecision{}, false, fmt.Errorf("close guard liquidation %s failed: %w", perpExchange, err)
	}
	s.registerAPISuccess(perpExchange)
	if math.Abs(pos.Quantity) <= 1e-9 || pos.LiquidationPrice <= 0 || pos.MarkPrice <= 0 {
		return executionCloseDecision{}, false, nil
	}

	liqDistance := positionLiquidationDistanceRatio(pos)
	if liqDistance <= 0 {
		return executionCloseDecision{}, false, nil
	}
	if s.cfg.SameExchangeEmergencyLiqDistanceRatio > 0 && liqDistance <= s.cfg.SameExchangeEmergencyLiqDistanceRatio {
		return executionCloseDecision{
			shouldClose: true,
			trigger:     "auto_liquidation_guard_emergency",
			reason: fmt.Sprintf(
				"%s liquidation distance %.4f <= emergency threshold %.4f (mark %.4f, liq %.4f)",
				perpExchange,
				liqDistance,
				s.cfg.SameExchangeEmergencyLiqDistanceRatio,
				pos.MarkPrice,
				pos.LiquidationPrice,
			),
		}, true, nil
	}
	if s.cfg.SameExchangeReduceLiqDistanceRatio > 0 && liqDistance <= s.cfg.SameExchangeReduceLiqDistanceRatio {
		return executionCloseDecision{
			shouldClose: true,
			trigger:     "auto_liquidation_guard_reduce",
			reason: fmt.Sprintf(
				"%s liquidation distance %.4f <= reduce threshold %.4f (mark %.4f, liq %.4f)",
				perpExchange,
				liqDistance,
				s.cfg.SameExchangeReduceLiqDistanceRatio,
				pos.MarkPrice,
				pos.LiquidationPrice,
			),
		}, true, nil
	}
	return executionCloseDecision{}, false, nil
}

func (s *ExecutionService) evaluateSameExchangeFundingExitGuard(now time.Time, plan *entity.ExecutionPlan) (executionCloseDecision, bool, error) {
	if plan == nil || !s.cfg.SameExchangeCloseOnNegativeFunding {
		return executionCloseDecision{}, false, nil
	}

	arbitrageMode := normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange))
	if arbitrageMode != ArbitrageModeSameExchangeSpotPerp {
		return executionCloseDecision{}, false, nil
	}

	guardCfg := s.cfg
	guardCfg.ArbitrageMode = arbitrageMode
	snapshots, ok := loadPairMarketSnapshot(s.store, guardCfg, plan.Symbol, plan.LongExchange, plan.ShortExchange)
	if !ok {
		return executionCloseDecision{}, false, fmt.Errorf("close guard same-exchange funding: missing latest pair snapshot for %s", plan.Symbol)
	}
	if isSnapshotStaleForConfig(guardCfg, now, snapshots.LongFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, snapshots.ShortFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, snapshots.LongBook.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, snapshots.ShortBook.EventTimeMs) {
		return executionCloseDecision{}, false, fmt.Errorf("close guard same-exchange funding: stale market data for %s", plan.Symbol)
	}

	perpFunding := snapshots.ShortFunding
	if isPerpetualSymbol(snapshots.LongMeta) && !isPerpetualSymbol(snapshots.ShortMeta) {
		perpFunding = snapshots.LongFunding
	}
	if perpFunding.FundingRate >= 0 {
		return executionCloseDecision{}, false, nil
	}

	historyThreshold := s.cfg.SameExchangeHistoryNegativeExitThreshold
	historySupportsExit := plan.PerpFundingHistorySampleCount == 0 || plan.PerpFundingHistoryNegativeRatio >= historyThreshold
	if !historySupportsExit {
		return executionCloseDecision{}, false, nil
	}

	estimatedClosePNL := sameExchangeEstimatedClosePNL(plan, snapshots.LongBook, snapshots.ShortBook)
	if s.cfg.SameExchangeExitRequirePositiveClosePNL && estimatedClosePNL < s.cfg.SameExchangeExitMinClosePNL {
		return executionCloseDecision{}, false, nil
	}

	reason := fmt.Sprintf(
		"perp funding turned negative at %.5f%%; history negative ratio %.1f%% over %d samples; estimated close pnl %.4f",
		perpFunding.FundingRate*100,
		plan.PerpFundingHistoryNegativeRatio*100,
		plan.PerpFundingHistorySampleCount,
		estimatedClosePNL,
	)
	return executionCloseDecision{
		shouldClose: true,
		trigger:     "auto_negative_funding_exit",
		reason:      reason,
	}, true, nil
}

func (s *ExecutionService) evaluateSameExchangePriceShockGuard(ctx context.Context, now time.Time, plan *entity.ExecutionPlan) (executionCloseDecision, bool, error) {
	if plan == nil {
		return executionCloseDecision{}, false, nil
	}
	arbitrageMode := normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange))
	if arbitrageMode != ArbitrageModeSameExchangeSpotPerp || s.cfg.SameExchangeMax1hPriceShockRatio <= 0 {
		return executionCloseDecision{}, false, nil
	}
	guardCfg := s.cfg
	guardCfg.ArbitrageMode = arbitrageMode
	snapshots, ok := loadPairMarketSnapshot(s.store, guardCfg, plan.Symbol, plan.LongExchange, plan.ShortExchange)
	if !ok {
		return executionCloseDecision{}, false, fmt.Errorf("close guard price shock: missing latest pair snapshot for %s", plan.Symbol)
	}
	if isSnapshotStaleForConfig(guardCfg, now, snapshots.LongFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(guardCfg, now, snapshots.ShortFunding.EventTimeMs) {
		return executionCloseDecision{}, false, fmt.Errorf("close guard price shock: stale funding snapshot for %s", plan.Symbol)
	}

	perpExchange := plan.ShortExchange
	perpFunding := snapshots.ShortFunding
	if isPerpetualSymbol(snapshots.LongMeta) && !isPerpetualSymbol(snapshots.ShortMeta) {
		perpExchange = plan.LongExchange
		perpFunding = snapshots.LongFunding
	}
	assessment := assessSameExchangePriceRisk(ctx, guardCfg, s.marketRepo, arbitrageMode, perpExchange, plan.Symbol, perpFunding, now)
	if assessment.Allowed {
		return executionCloseDecision{}, false, nil
	}
	return executionCloseDecision{
		shouldClose: true,
		trigger:     "auto_price_shock_guard",
		reason:      sameExchangePriceRiskRejectReason(guardCfg, assessment),
	}, true, nil
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
	if cfg.normalize().StrategyMode == StrategyModeRollingCycleAligned {
		plan := buildDirectionalFundingPlanForConfig(
			cfg,
			now,
			pickFirstNonEmpty(longFunding.Exchange, "long"),
			longFunding,
			longForecast,
			pickFirstNonEmpty(shortFunding.Exchange, "short"),
			shortFunding,
			shortForecast,
		)
		return selectPrimaryFundingProjectionForConfig(cfg.normalize(), plan.Projections)
	}
	projections := buildFundingProjectionsForConfig(cfg, now, longFunding, longForecast, shortFunding, shortForecast)
	return selectPrimaryFundingProjectionForConfig(cfg.normalize(), projections)
}

// projectFundingCarryForPlanRevalidation 会尽量沿用“计划生成时已经选中的持有周期”。
//
// 这样做的目的是保证：
// 1. 计划页上看到的收益窗口；
// 2. execution plan 里写下来的 ProjectedFundingTimeMs；
// 3. 真正发单前最后一次 revalidation；
// 三者尽量围绕同一条 funding 路径做判断。
//
// 否则就会出现这种错位：
// - 计划生成时选的是 4h 窗口；
// - 发单前 revalidation 却临时改按 1h 窗口重算；
// - 最终“预期收益”和“实际自动平仓时点”不再对应。
func projectFundingCarryForPlanRevalidation(cfg Config, now time.Time, plan *entity.ExecutionPlan, longFunding, shortFunding entity.FundingSnapshot) (fundingProjection, bool) {
	if plan == nil {
		return fundingProjection{}, false
	}
	if projection, ok := sameExchangeHistoricalLongHoldProjectionForPlan(now, plan, shortFunding); ok {
		return projection, true
	}

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
	if cfg.normalize().StrategyMode == StrategyModeRollingCycleAligned {
		planProjections := buildDirectionalFundingPlanForConfig(
			cfg,
			now,
			pickFirstNonEmpty(longFunding.Exchange, "long"),
			longFunding,
			longForecast,
			pickFirstNonEmpty(shortFunding.Exchange, "short"),
			shortFunding,
			shortForecast,
		).Projections
		return selectFundingProjectionForPlan(cfg, plan, planProjections)
	}
	projections := buildFundingProjectionsForConfig(cfg, now, longFunding, longForecast, shortFunding, shortForecast)
	return selectFundingProjectionForPlan(cfg, plan, projections)
}

func buildFundingProjectionsForConfig(cfg Config, now time.Time, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) []fundingProjection {
	return buildLegacyFundingProjectionsForConfig(cfg, now, longFunding, longForecast, shortFunding, shortForecast)
}

func buildFundingProjections(now time.Time, holdHours float64, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) []fundingProjection {
	return buildLegacyFundingProjections(now, holdHours, longFunding, longForecast, shortFunding, shortForecast)
}

func selectFundingProjectionForPlan(cfg Config, plan *entity.ExecutionPlan, projections []fundingProjection) (fundingProjection, bool) {
	if len(projections) == 0 {
		return fundingProjection{}, false
	}
	if plan == nil {
		return selectPrimaryFundingProjectionForConfig(cfg.normalize(), projections)
	}

	// 第一优先级：事件数完全匹配。
	// 对 funding 套利来说，“吃到了几轮 funding”比“绝对时间是否完全相等”更重要。
	matchedCounts := make([]fundingProjection, 0, len(projections))
	for _, projection := range projections {
		if projection.LongFundingEventCount == plan.LongFundingEventCount &&
			projection.ShortFundingEventCount == plan.ShortFundingEventCount {
			matchedCounts = append(matchedCounts, projection)
		}
	}
	if len(matchedCounts) > 0 {
		return nearestFundingProjection(plan.ProjectedFundingTimeMs, matchedCounts), true
	}

	// 第二优先级：如果事件数已经因为时间推进发生了偏移，至少尽量贴近原计划的兑现时点。
	if plan.ProjectedFundingTimeMs > 0 {
		return nearestFundingProjection(plan.ProjectedFundingTimeMs, projections), true
	}

	return selectPrimaryFundingProjectionForConfig(cfg.normalize(), projections)
}

func sameExchangeHistoricalLongHoldProjectionForPlan(now time.Time, plan *entity.ExecutionPlan, perpFunding entity.FundingSnapshot) (fundingProjection, bool) {
	if plan == nil {
		return fundingProjection{}, false
	}
	if normalizeArbitrageMode(plan.ArbitrageMode, ArbitrageModeCrossExchange) != ArbitrageModeSameExchangeSpotPerp {
		return fundingProjection{}, false
	}
	if !plan.SameExchangeLongHoldUsingHistoryEstimate || plan.SameExchangeLongHoldSuggestedFundingEvents <= 0 {
		return fundingProjection{}, false
	}
	if perpFunding.FundingRate <= 0 {
		return fundingProjection{}, false
	}
	adjustedEventRate := perpFunding.FundingRate
	if plan.PerpFundingHistorySampleCount > 0 {
		support := clampFloat(plan.PerpFundingHistoricalSupportRatio, 0, 1)
		adjustedEventRate = perpFunding.FundingRate*support + plan.PerpFundingHistoryMeanRate*(1-support)
	}
	if adjustedEventRate <= 0 {
		return fundingProjection{}, false
	}
	projectedFundingTimeMs := sameExchangeSuggestedFundingTimeMs(
		now.UnixMilli(),
		perpFunding.FundingTimeMs,
		maxInt(perpFunding.FundingIntervalHours, 1),
		plan.SameExchangeLongHoldSuggestedFundingEvents,
	)
	windowHours := sameExchangeSuggestedHoldHours(
		now.UnixMilli(),
		projectedFundingTimeMs,
		maxInt(perpFunding.FundingIntervalHours, 1),
		plan.SameExchangeLongHoldSuggestedFundingEvents,
	)
	carryRate := adjustedEventRate * float64(plan.SameExchangeLongHoldSuggestedFundingEvents)
	projection := fundingProjection{
		ProjectedFundingTimeMs:       projectedFundingTimeMs,
		RequiredEntryByFundingTimeMs: plan.RequiredEntryByFundingTimeMs,
		LongFundingEventCount:        plan.SameExchangeLongHoldSuggestedFundingEvents,
		ShortFundingEventCount:       plan.SameExchangeLongHoldSuggestedFundingEvents,
		FundingWindowHours:           windowHours,
		CarryRate:                    carryRate,
		ComputationMode:              "same_exchange_long_hold_history",
		StrategyMode:                 normalizedPlanStrategyMode(*plan),
		NextReviewTimeMs:             plan.NextReviewTimeMs,
		SyncBoundaryTimeMs:           plan.SyncBoundaryTimeMs,
		IncludedSegmentCount:         plan.EntryPathSegmentCount,
		PathEndReason:                "same_exchange_long_hold_history",
	}
	if windowHours > 0 {
		projection.CarryRateHourlyEquivalent = carryRate / windowHours
	}
	return projection, true
}

func nearestFundingProjection(targetMs int64, projections []fundingProjection) fundingProjection {
	best := projections[0]
	bestDiff := absInt64(best.ProjectedFundingTimeMs - targetMs)
	for i := 1; i < len(projections); i++ {
		diff := absInt64(projections[i].ProjectedFundingTimeMs - targetMs)
		if diff < bestDiff {
			best = projections[i]
			bestDiff = diff
			continue
		}
		if diff == bestDiff && projections[i].ProjectedFundingTimeMs < best.ProjectedFundingTimeMs {
			best = projections[i]
			bestDiff = diff
		}
	}
	return best
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

func sameExchangeEstimatedClosePNL(plan *entity.ExecutionPlan, longBook, shortBook entity.BookTopSnapshot) float64 {
	closePNL := markToMarketClosePNL(plan, longBook, shortBook)
	exitSlippagePNL := math.Abs(plan.SlippagePNL) * 0.5
	return closePNL - plan.ExitFeePNL - exitSlippagePNL
}

func closeSideBasisBps(longBid, shortAsk float64) float64 {
	mid := (longBid + shortAsk) / 2
	if mid <= 0 {
		return 0
	}
	return math.Abs(shortAsk-longBid) / mid * 10000
}
