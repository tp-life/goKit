package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

const (
	rollingMonitorActionNone     = "none"
	rollingMonitorActionContinue = "continue"
	rollingMonitorActionClose    = "close"
	rollingMonitorActionFlip     = "flip"
)

// rollingMonitorDecision 是 rolling review 对当前 live 记录给出的下一步动作。
//
// 它故意把“为什么做这个决定”和“决定后需要更新哪些时间锚点”都放在一起，
// 这样 runRollingMonitor 的主循环可以只负责：
// 1. 读取候选；
// 2. 调一个决策器；
// 3. 按 action 执行 side effect。
type rollingMonitorDecision struct {
	Action string

	Trigger string
	Reason  string

	NextReviewTimeMs   int64
	SyncBoundaryTimeMs int64

	SameDirectionProjection    fundingProjection
	ReverseDirectionProjection fundingProjection
	DesiredLongExchange        string
	DesiredShortExchange       string
}

// runRollingMonitor 专门处理 rolling_cycle_aligned 仓位的“持仓后 review”。
//
// 它与原有 runAutoClose 的分工是：
// - runAutoClose: 处理 legacy 的 schedule close，以及所有模式共享的 safety guard；
// - runRollingMonitor: 处理 rolling 模式下“到 review 点后是续持、平仓还是翻仓”。
//
// 这样可以避免把两套完全不同的时间语义揉进一个 if/switch：
// - legacy 看固定 target close time；
// - rolling 看 next review time + 最新 funding 快照。
func (s *ExecutionService) runRollingMonitor(ctx context.Context) {
	records, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		s.logger.Error("execution_rolling_monitor_list_records_failed", slog.Any("err", err))
		return
	}

	now := time.Now().UTC()
	for _, rec := range records {
		if !shouldMonitorRollingRecord(rec) {
			continue
		}

		plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
		if err != nil {
			s.logger.Error(
				"execution_rolling_monitor_load_plan_failed",
				slog.String("plan_key", rec.PlanKey),
				slog.Any("err", err),
			)
			continue
		}
		if plan == nil {
			s.logger.Error(
				"execution_rolling_monitor_plan_missing",
				slog.String("plan_key", rec.PlanKey),
			)
			continue
		}

		decision, err := s.evaluateRollingMonitorDecision(ctx, now, rec, *plan)
		if err != nil {
			s.logger.Error(
				"execution_rolling_monitor_decision_failed",
				slog.String("plan_key", rec.PlanKey),
				slog.Any("err", err),
			)
			continue
		}

		switch decision.Action {
		case rollingMonitorActionContinue:
			if err := s.applyRollingContinueDecision(ctx, now, rec, decision); err != nil {
				s.logger.Error(
					"execution_rolling_monitor_continue_failed",
					slog.String("plan_key", rec.PlanKey),
					slog.Any("err", err),
				)
				continue
			}
			s.logger.Info(
				"execution_rolling_monitor_continued",
				slog.String("plan_key", rec.PlanKey),
				slog.String("reason", decision.Reason),
				slog.Int64("next_review_time_ms", decision.NextReviewTimeMs),
				slog.Int64("sync_boundary_time_ms", decision.SyncBoundaryTimeMs),
			)
		case rollingMonitorActionClose:
			if _, err := s.closePlan(ctx, plan, decision.Trigger, rec.LiveTrading); err != nil {
				s.logger.Error(
					"execution_rolling_monitor_close_failed",
					slog.String("plan_key", rec.PlanKey),
					slog.String("trigger", decision.Trigger),
					slog.Any("err", err),
				)
				continue
			}
			s.logger.Info(
				"execution_rolling_monitor_closed",
				slog.String("plan_key", rec.PlanKey),
				slog.String("trigger", decision.Trigger),
				slog.String("reason", decision.Reason),
			)
		case rollingMonitorActionFlip:
			if err := s.applyRollingFlipDecision(ctx, now, rec, *plan, decision); err != nil {
				s.logger.Error(
					"execution_rolling_monitor_flip_failed",
					slog.String("plan_key", rec.PlanKey),
					slog.Any("err", err),
				)
				continue
			}
			s.logger.Info(
				"execution_rolling_monitor_flipped",
				slog.String("plan_key", rec.PlanKey),
				slog.String("reason", decision.Reason),
				slog.String("next_long_exchange", decision.DesiredLongExchange),
				slog.String("next_short_exchange", decision.DesiredShortExchange),
			)
		}
	}
}

func shouldMonitorRollingRecord(rec entity.ExecutionRecord) bool {
	if !rec.LiveTrading || !rec.AutoClose {
		return false
	}
	if normalizeStrategyMode(rec.StrategyMode, StrategyModeLegacyProjection) != StrategyModeRollingCycleAligned {
		return false
	}
	// rolling review 只在“这条仓位已经正常打开，且尚未进入 close phase”时生效。
	// open/close 异常尾部仍然交给既有的 auto-close recovery 逻辑处理。
	return normalizeExecutionStatus(rec.Status) == executionStateOpened
}

// evaluateRollingMonitorDecision 根据“当前 review 点 + 当前 funding 快照”给出 rolling 动作。
//
// 决策顺序是刻意固定的：
// 1. 先确认是否真的到了 review 点；
// 2. 再确认 review 所需的 funding 快照是否已经刷新到位；
// 3. 再比较当前方向还能否继续持有；
// 4. 最后才考虑方向反转后的翻仓。
//
// 这样可以保证：
// - 不会因为旧 snapshot 误判方向；
// - 不会在当前方向仍然成立时，提前做无谓 flip；
// - close / flip 都建立在“当前这轮 review 已真实可见”的前提上。
func (s *ExecutionService) evaluateRollingMonitorDecision(ctx context.Context, now time.Time, rec entity.ExecutionRecord, plan entity.ExecutionPlan) (rollingMonitorDecision, error) {
	// review 锚点优先使用 execution record 上已经推进过的值。
	// 这样同一条 live 仓位在 continue 之后，会继续沿着新的 review 节奏往前走，
	// 而不是退回 plan 初始写入的第一档 review time。
	reviewTimeMs := firstPositiveInt64(rec.NextReviewTimeMs, plan.NextReviewTimeMs)
	if reviewTimeMs <= 0 {
		return rollingMonitorDecision{Action: rollingMonitorActionNone}, nil
	}

	reviewGraceMs := s.cfg.RollingReviewSettleGracePeriod.Milliseconds()
	// settlement 到点后通常还需要一点缓冲时间，等待交易所把最新 funding 快照写出来。
	// 在这个 grace 内我们只运行 safety guards，不抢跑做 continue / flip 决策。
	if now.UnixMilli() < reviewTimeMs+reviewGraceMs {
		return rollingMonitorDecision{Action: rollingMonitorActionNone}, nil
	}

	longFunding, okLong := s.store.LatestFunding(plan.LongExchange, plan.Symbol)
	shortFunding, okShort := s.store.LatestFunding(plan.ShortExchange, plan.Symbol)
	if !(okLong && okShort) {
		return s.rollingSnapshotWaitOrTimeoutDecision(now, rec, plan, "latest funding snapshot is missing"), nil
	}

	if !s.rollingReviewSnapshotsReady(now, rec, plan, longFunding, shortFunding) {
		return s.rollingSnapshotWaitOrTimeoutDecision(now, rec, plan, "review funding snapshot is not fresh enough yet"), nil
	}

	// rolling review 关注的是“此刻之后还能不能继续吃 funding”，
	// 所以这里直接围绕最新 snapshot 构造一份轻量 forecast，
	// 避免把旧历史状态继续带到 review 点之后。
	longForecast := rollingSpotForecast(s.cfg, longFunding)
	shortForecast := rollingSpotForecast(s.cfg, shortFunding)

	// samePlan / reversePlan 分别回答两个问题：
	// 1. 维持当前方向还能否继续获利；
	// 2. 如果现在反手，新的方向是否已经更优。
	//
	// 两者都复用同一个 segment engine，确保机会层和执行层完全同口径。
	samePlan := buildDirectionalFundingPlanForConfig(
		s.cfg,
		now,
		plan.LongExchange,
		longFunding,
		longForecast,
		plan.ShortExchange,
		shortFunding,
		shortForecast,
	)
	sameProjection, sameOK := selectPrimaryFundingProjectionForConfig(s.cfg, samePlan.Projections)

	reversePlan := buildDirectionalFundingPlanForConfig(
		s.cfg,
		now,
		plan.ShortExchange,
		shortFunding,
		shortForecast,
		plan.LongExchange,
		longFunding,
		longForecast,
	)
	reverseProjection, reverseOK := selectPrimaryFundingProjectionForConfig(s.cfg, reversePlan.Projections)

	// review 阶段优先用当前 execution 实际占用的 notional 来比较净收益门槛。
	// 这样 continue / flip 的门槛才和真实仓位规模对齐。
	notional := firstPositiveFloat(rec.AllocatedNotionalUSDT, plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT)
	if sameOK && sameProjection.CarryRate > 0 {
		continueNetPNL := rollingContinueNetPNL(notional, sameProjection)
		// 同方向续持只比较“接下来这一段还能新增多少 funding”。
		// 它不重复扣已经支付过的开仓成本，因此门槛通常比 flip 更宽松。
		if !s.cfg.RollingReviewRequireIncrementalNetPositive || continueNetPNL >= s.cfg.RollingReviewMinIncrementalNetPNL {
			return rollingMonitorDecision{
				Action:                  rollingMonitorActionContinue,
				Trigger:                 "rolling_continue",
				Reason:                  fmt.Sprintf("same-direction carry remains positive; incremental net %.4f", continueNetPNL),
				NextReviewTimeMs:        sameProjection.NextReviewTimeMs,
				SyncBoundaryTimeMs:      sameProjection.SyncBoundaryTimeMs,
				SameDirectionProjection: sameProjection,
			}, nil
		}
	}

	if s.cfg.RollingFlipEnabled && reverseOK && reverseProjection.CarryRate > 0 {
		flipNetPNL := rollingFlipNetPNL(notional, reverseProjection, plan, s.cfg)
		// flip 的经济口径更严格，因为它隐含：
		// - 平掉旧仓
		// - 开出新仓
		// - 预留新仓未来再平一次的成本
		if !s.cfg.RollingFlipRequireNetPositive || flipNetPNL >= s.cfg.RollingFlipMinNetPNL {
			return rollingMonitorDecision{
				Action:                     rollingMonitorActionFlip,
				Trigger:                    "rolling_flip",
				Reason:                     fmt.Sprintf("same direction no longer wins; reverse incremental net %.4f", flipNetPNL),
				NextReviewTimeMs:           reverseProjection.NextReviewTimeMs,
				SyncBoundaryTimeMs:         reverseProjection.SyncBoundaryTimeMs,
				ReverseDirectionProjection: reverseProjection,
				DesiredLongExchange:        plan.ShortExchange,
				DesiredShortExchange:       plan.LongExchange,
			}, nil
		}
	}

	if s.cfg.RollingReviewCloseOnUnprofitable {
		return rollingMonitorDecision{
			Action:  rollingMonitorActionClose,
			Trigger: "rolling_unprofitable",
			Reason:  "current review no longer supports continue/flip with positive incremental value",
		}, nil
	}
	return rollingMonitorDecision{Action: rollingMonitorActionNone}, nil
}

// rollingReviewSnapshotsReady 用“fundingTime 是否推进过 review 点”来判断 review 快照是否真的刷新。
//
// 这里不用简单的 EventTime 作为唯一判断，是因为 rolling review 更关心“结算语义是否前进了”：
// - 非 shared review：至少应有一侧 fundingTime 推进到 review 之后；
// - shared boundary review：两侧 fundingTime 都必须推进到 review 之后。
func (s *ExecutionService) rollingReviewSnapshotsReady(now time.Time, rec entity.ExecutionRecord, plan entity.ExecutionPlan, longFunding, shortFunding entity.FundingSnapshot) bool {
	if isSnapshotStaleForConfig(s.cfg, now, longFunding.EventTimeMs) || isSnapshotStaleForConfig(s.cfg, now, shortFunding.EventTimeMs) {
		return false
	}

	reviewTimeMs := firstPositiveInt64(rec.NextReviewTimeMs, plan.NextReviewTimeMs)
	syncBoundaryMs := firstPositiveInt64(rec.CurrentSyncBoundaryMs, plan.SyncBoundaryTimeMs)
	longAdvanced := longFunding.FundingTimeMs > reviewTimeMs
	shortAdvanced := shortFunding.FundingTimeMs > reviewTimeMs

	if syncBoundaryMs > 0 && reviewTimeMs >= syncBoundaryMs {
		// 到 shared boundary 时，两边都必须推进到 review 点之后。
		// 否则会出现“一边是真实 16:00，另一边还是旧 12:00”的混算。
		return longAdvanced && shortAdvanced
	}
	// 非 shared review 只要最先结算的那一腿已经推进，就足以支持这次重新判断。
	return longAdvanced || shortAdvanced
}

func (s *ExecutionService) rollingSnapshotWaitOrTimeoutDecision(now time.Time, rec entity.ExecutionRecord, plan entity.ExecutionPlan, reason string) rollingMonitorDecision {
	reviewTimeMs := firstPositiveInt64(rec.NextReviewTimeMs, plan.NextReviewTimeMs)
	deadlineMs := reviewTimeMs + s.cfg.RollingReviewFreshSnapshotMaxWait.Milliseconds()
	if reviewTimeMs <= 0 || now.UnixMilli() < deadlineMs {
		// deadline 之前持续等待，让 review 有机会拿到真正的新 funding snapshot。
		return rollingMonitorDecision{Action: rollingMonitorActionNone}
	}
	if s.cfg.RollingReviewCloseOnSnapshotTimeout {
		// 过了 freshness deadline 仍拿不到快照，说明策略判断基础已经不足。
		// 按保守模式直接回到 flat，避免带着旧方向继续暴露。
		return rollingMonitorDecision{
			Action:  rollingMonitorActionClose,
			Trigger: "rolling_snapshot_timeout",
			Reason:  fmt.Sprintf("%s before snapshot timeout deadline", reason),
		}
	}
	return rollingMonitorDecision{Action: rollingMonitorActionNone}
}

// applyRollingContinueDecision 只更新 review 锚点，不改变 execution 状态。
//
// 这一步故意不触发 open/close 状态机事件，因为它并没有改变持仓动作，
// 只是把“下一次何时必须重新判断”推进到新的 review 点。
func (s *ExecutionService) applyRollingContinueDecision(ctx context.Context, now time.Time, rec entity.ExecutionRecord, decision rollingMonitorDecision) error {
	rec.NextReviewTimeMs = decision.NextReviewTimeMs
	rec.CurrentSyncBoundaryMs = decision.SyncBoundaryTimeMs
	if decision.NextReviewTimeMs > 0 {
		// rolling record 上的 target close 只是“下一次必须重新审视仓位”的提醒锚点。
		// 真到这个时间点后，是续持还是平仓，仍由下一轮 rolling monitor 再判。
		rec.TargetCloseTimeMs = decision.NextReviewTimeMs + s.cfg.RollingReviewSettleGracePeriod.Milliseconds()
	}
	rec.ReviewCount++
	rec.LastReviewAtMs = now.UnixMilli()
	rec.LastReviewReason = decision.Reason
	rec.LastTransitionAtMs = now.UnixMilli()
	rec.LastTransitionEvent = "rolling_review_continue"
	rec.StatusReason = decision.Reason
	rec.LastError = ""
	return s.execRepo.Upsert(ctx, &rec)
}

// applyRollingFlipDecision 先安全退出当前方向，再尝试把 successor 方向接上。
//
// 这里的顺序不能反：
// 1. 先 close 旧仓，避免同一 rolling group 双向并存；
// 2. close 成功后，再去找当前 batch 里最合适的 successor plan；
// 3. successor 如果不存在或 revalidation 失败，也只会留下 flat 状态，不会放大风险。
func (s *ExecutionService) applyRollingFlipDecision(ctx context.Context, now time.Time, rec entity.ExecutionRecord, plan entity.ExecutionPlan, decision rollingMonitorDecision) error {
	// 第一步必须先把旧方向安全关掉。
	// 这样即使 successor plan 稍后找不到或复核失败，系统也只会退回 flat，
	// 不会留下同一 rolling group 双向并存的风险。
	closedRec, err := s.closePlan(ctx, &plan, decision.Trigger, rec.LiveTrading)
	if err != nil {
		return err
	}
	if closedRec == nil || normalizeExecutionStatus(closedRec.Status) != executionStateClosed {
		return fmt.Errorf("rolling flip close did not finish cleanly for plan %s", plan.PlanKey)
	}

	closedRec.ReviewCount++
	closedRec.LastReviewAtMs = now.UnixMilli()
	closedRec.LastReviewReason = decision.Reason
	closedRec.LastTransitionAtMs = now.UnixMilli()
	closedRec.LastTransitionEvent = "rolling_review_flip_close"
	closedRec.StatusReason = decision.Reason

	if !s.cfg.Execution.Enabled {
		return s.execRepo.Upsert(ctx, closedRec)
	}

	// 优先复用最新 batch 里已经生成好的 successor plan。
	// 只有当 StrategyRunner 来不及产出时，才退化到“基于当前快照即时构建”。
	successorPlan, err := s.findRollingSuccessorPlan(ctx, rec.RollingGroupKey, plan.PlanKey, decision.DesiredLongExchange, decision.DesiredShortExchange)
	if err != nil {
		return err
	}
	if successorPlan == nil {
		successorPlan, err = s.buildRollingSuccessorPlan(now, rec, plan, decision)
		if err != nil {
			return err
		}
		if successorPlan == nil {
			return s.execRepo.Upsert(ctx, closedRec)
		}
		if err := s.planRepo.SaveBatch(ctx, successorPlan.BatchID, successorPlan.OpportunityBatchID, []entity.ExecutionPlan{*successorPlan}); err != nil {
			return err
		}
	}

	closedRec.SuccessorPlanKey = successorPlan.PlanKey
	if err := s.execRepo.Upsert(ctx, closedRec); err != nil {
		return err
	}

	// rolling flip reopen 会绕过普通 entry window 限制，
	// 但 funding / basis / balance 这些实时风控仍会照常复核。
	nextRec, err := s.openPlan(ctx, successorPlan, "rolling_flip_reopen", true)
	if err != nil {
		return err
	}
	if nextRec == nil {
		return nil
	}
	nextRec.PredecessorPlanKey = plan.PlanKey
	nextRec.LastReviewAtMs = now.UnixMilli()
	nextRec.LastReviewReason = fmt.Sprintf("opened as rolling flip successor for %s", plan.PlanKey)
	return s.execRepo.Upsert(ctx, nextRec)
}

// findRollingSuccessorPlan 从当前最新的 ready plans 里找“同组、反向、仍可开”的 successor。
//
// 它不会直接重算一份新 plan，而是优先复用 StrategyRunner 已经写进 execution_plans 的结果。
// 这样可以保证：
// - successor 的名义、数量、basis、净收益，仍来自统一的机会/计划链路；
// - rolling monitor 只负责“何时切换”，不负责复制整套 plan 构建逻辑。
func (s *ExecutionService) findRollingSuccessorPlan(ctx context.Context, rollingGroupKey, currentPlanKey, desiredLongExchange, desiredShortExchange string) (*entity.ExecutionPlan, error) {
	if strings.TrimSpace(rollingGroupKey) == "" {
		return nil, nil
	}

	plans, err := s.planRepo.ListLatest(ctx, maxInt(s.cfg.Execution.MaxLatestPlans*4, 500))
	if err != nil {
		return nil, err
	}

	activeRecords, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		return nil, err
	}
	activePlanKeys := make(map[string]struct{}, len(activeRecords))
	for _, item := range activeRecords {
		activePlanKeys[item.PlanKey] = struct{}{}
	}

	for i := range plans {
		plan := plans[i]
		// 先按 cheap filter 剪掉明显不合格的候选，再做较贵的 live-record 检查。
		// 这样 successor 搜索在批次较大时也不会把所有 plan 都查一遍状态。
		if plan.PlanKey == currentPlanKey {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(plan.Status), "late") {
			continue
		}
		if normalizedPlanStrategyMode(plan) != StrategyModeRollingCycleAligned {
			continue
		}
		if strings.TrimSpace(plan.RollingGroupKey) != strings.TrimSpace(rollingGroupKey) {
			continue
		}
		if !strings.EqualFold(plan.LongExchange, desiredLongExchange) || !strings.EqualFold(plan.ShortExchange, desiredShortExchange) {
			continue
		}
		if _, exists := activePlanKeys[plan.PlanKey]; exists {
			continue
		}
		if existing, err := s.execRepo.FindByPlanKey(ctx, plan.PlanKey); err == nil && existing != nil && countsTowardLivePlanLimit(*existing) {
			continue
		}
		cp := plan
		return &cp, nil
	}
	return nil, nil
}

// buildRollingSuccessorPlan 在最新 batch 里找不到现成 successor 时，直接基于当前市场快照构造一条新计划。
//
// 这一步主要是为了解决 rolling flip 的特殊性：
// - 普通 auto-open 计划受 entry window 限制；
// - 但 review 点的翻仓是“旧仓刚平、方向必须立刻切换”的连续动作；
// - 因此 successor 计划需要允许在普通 entry window 之外被即时构造。
func (s *ExecutionService) buildRollingSuccessorPlan(now time.Time, rec entity.ExecutionRecord, currentPlan entity.ExecutionPlan, decision rollingMonitorDecision) (*entity.ExecutionPlan, error) {
	longMeta, okLongMeta := s.store.Symbol(decision.DesiredLongExchange, currentPlan.Symbol)
	shortMeta, okShortMeta := s.store.Symbol(decision.DesiredShortExchange, currentPlan.Symbol)
	longBook, okLongBook := s.store.LatestBookTop(decision.DesiredLongExchange, currentPlan.Symbol)
	shortBook, okShortBook := s.store.LatestBookTop(decision.DesiredShortExchange, currentPlan.Symbol)
	longFunding, okLongFunding := s.store.LatestFunding(decision.DesiredLongExchange, currentPlan.Symbol)
	shortFunding, okShortFunding := s.store.LatestFunding(decision.DesiredShortExchange, currentPlan.Symbol)
	if !(okLongMeta && okShortMeta && okLongBook && okShortBook && okLongFunding && okShortFunding) {
		return nil, fmt.Errorf("build rolling successor plan: missing current market snapshots for %s %s/%s", currentPlan.Symbol, decision.DesiredLongExchange, decision.DesiredShortExchange)
	}

	// successor 默认尽量继承当前 live 仓位已经占用的名义。
	// 这样 flip 更接近“方向切换”，而不是 review 点临时改变风险暴露大小。
	targetNotional := firstPositiveFloat(rec.AllocatedNotionalUSDT, currentPlan.RoundedNotionalUSDT, currentPlan.TargetNotionalUSDT)
	longQty, shortQty, _, _, ok := computeCommonLegQuantities(
		targetNotional,
		longBook.AskPrice,
		shortBook.BidPrice,
		longMeta.StepSize,
		shortMeta.StepSize,
	)
	if !ok {
		return nil, fmt.Errorf("build rolling successor plan: unable to compute common leg quantities for %s", currentPlan.Symbol)
	}

	batchID := fmt.Sprintf("rolling-flip-%d", now.UnixMilli())
	plan := entity.ExecutionPlan{
		BatchID:                      batchID,
		OpportunityBatchID:           currentPlan.OpportunityBatchID,
		Symbol:                       currentPlan.Symbol,
		RollingGroupKey:              currentPlan.RollingGroupKey,
		Status:                       "ready",
		ReadyNow:                     true,
		LongExchange:                 decision.DesiredLongExchange,
		ShortExchange:                decision.DesiredShortExchange,
		LongVenueSymbol:              longMeta.VenueSymbol,
		ShortVenueSymbol:             shortMeta.VenueSymbol,
		LongSide:                     "BUY",
		ShortSide:                    "SELL",
		EntryMode:                    currentPlan.EntryMode,
		ExitMode:                     currentPlan.ExitMode,
		TargetLeverage:               currentPlan.TargetLeverage,
		CapitalAllocatedUSDT:         currentPlan.CapitalAllocatedUSDT,
		TargetNotionalUSDT:           targetNotional,
		RoundedNotionalUSDT:          targetNotional,
		LongEntryPrice:               round6(longBook.AskPrice),
		ShortEntryPrice:              round6(shortBook.BidPrice),
		LongQty:                      round8(longQty),
		ShortQty:                     round8(shortQty),
		LongMinQty:                   parseFloat(longMeta.MinQty),
		ShortMinQty:                  parseFloat(shortMeta.MinQty),
		LongMinNotionalUSDT:          parseFloat(longMeta.MinNotional),
		ShortMinNotionalUSDT:         parseFloat(shortMeta.MinNotional),
		CrossVenueBasisBps:           round4(crossVenueBasisBps(longBook.AskPrice, shortBook.BidPrice)),
		FundingCarryPNL:              round2(targetNotional * decision.ReverseDirectionProjection.CarryRate),
		EntryFeePNL:                  currentPlan.EntryFeePNL,
		ExitFeePNL:                   currentPlan.ExitFeePNL,
		SlippagePNL:                  currentPlan.SlippagePNL,
		SafetyBufferPNL:              currentPlan.SafetyBufferPNL,
		EntryPenaltyBps:              currentPlan.EntryPenaltyBps,
		ExitPenaltyBps:               currentPlan.ExitPenaltyBps,
		HedgePenaltyBps:              currentPlan.HedgePenaltyBps,
		ExecutionPenaltyBps:          currentPlan.ExecutionPenaltyBps,
		ExecutionPenaltyModel:        currentPlan.ExecutionPenaltyModel,
		ExecutionPenaltyBucket:       currentPlan.ExecutionPenaltyBucket,
		NetExpectedPNL:               round2(targetNotional*decision.ReverseDirectionProjection.CarryRate - currentPlan.EntryFeePNL - currentPlan.ExitFeePNL - currentPlan.SlippagePNL - currentPlan.SafetyBufferPNL),
		EarliestFundingTimeMs:        minInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs),
		LatestFundingTimeMs:          maxInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs),
		ProjectedFundingTimeMs:       decision.ReverseDirectionProjection.ProjectedFundingTimeMs,
		RequiredEntryByFundingTimeMs: now.UnixMilli(),
		LongFundingEventCount:        decision.ReverseDirectionProjection.LongFundingEventCount,
		ShortFundingEventCount:       decision.ReverseDirectionProjection.ShortFundingEventCount,
		FundingWindowHours:           decision.ReverseDirectionProjection.FundingWindowHours,
		StrategyMode:                 StrategyModeRollingCycleAligned,
		FundingComputationMode:       decision.ReverseDirectionProjection.ComputationMode,
		NextReviewTimeMs:             decision.NextReviewTimeMs,
		SyncBoundaryTimeMs:           decision.SyncBoundaryTimeMs,
		EntryPathSegmentCount:        decision.ReverseDirectionProjection.IncludedSegmentCount,
		EntryPathStopReason:          decision.ReverseDirectionProjection.PathEndReason,
		EntryWindowOpenMs:            now.Add(-time.Second).UnixMilli(),
		EntryWindowCloseMs:           now.Add(time.Minute).UnixMilli(),
		TargetCloseTimeMs:            decision.NextReviewTimeMs + s.cfg.RollingReviewSettleGracePeriod.Milliseconds(),
		AsOfTimeMs:                   now.UnixMilli(),
	}
	if plan.RoundedNotionalUSDT > 0 {
		plan.NetExpectedPNLBps = round4(plan.NetExpectedPNL / plan.RoundedNotionalUSDT * 10000)
	}
	// successor 即使是即时构造，也尽量沿用 execution plan 的排序口径：
	// funding 越高越好，basis 越窄越好。
	plan.Score = plan.NetExpectedPNL*100 + decision.ReverseDirectionProjection.CarryRateHourlyEquivalent*1_000_000 - plan.CrossVenueBasisBps*2
	plan.PlanKey = buildPlanKey(plan)
	return &plan, nil
}

func rollingSpotForecast(cfg Config, item entity.FundingSnapshot) fundingForecast {
	decay := normalizedContinuationDecay(cfg.FundingRateContinuationDecay)
	// review 点之后我们只需要一个保守的短期延续假设：
	// 当前 funding rate 既是 baseline，也是均值和上下限。
	// 这样 forecast 不会在策略最敏感的切换时刻再引入额外历史拟合偏差。
	return fundingForecast{
		CurrentRate:        item.FundingRate,
		BaselineRate:       item.FundingRate,
		HistoryMean:        item.FundingRate,
		Regime:             "spot_only",
		Confidence:         "low",
		MeanReversion:      0.20,
		ContinuationDecay:  decay,
		EffectiveFloorRate: item.FundingRate,
		EffectiveCapRate:   item.FundingRate,
	}
}

func rollingContinueNetPNL(notional float64, projection fundingProjection) float64 {
	if notional <= 0 {
		return 0
	}
	// 同方向续持只比较“后续增量 funding”，不重复扣 entry fee。
	return notional * projection.CarryRate
}

func rollingFlipNetPNL(notional float64, projection fundingProjection, plan entity.ExecutionPlan, cfg Config) float64 {
	if notional <= 0 {
		return 0
	}
	grossFundingPNL := notional * projection.CarryRate
	// flip 属于“先平旧仓，再开新仓，并预留新仓最终平仓”的组合动作。
	// 由于这里仍在同一交易所对里翻方向，因此可以直接复用当前 plan 的成本文本：
	// - 旧仓 close fee ~= 当前 ExitFeePNL
	// - 新仓 open fee ~= 当前 EntryFeePNL
	// - 新仓最终 close reserve ~= 当前 ExitFeePNL
	flipFees := plan.EntryFeePNL + plan.ExitFeePNL + plan.ExitFeePNL
	flipSlippage := plan.SlippagePNL * cfg.RollingFlipSlippageMultiplier
	flipBuffer := cfg.RollingFlipExtraSafetyBufferUSDT
	return grossFundingPNL - flipFees - flipSlippage - flipBuffer
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
