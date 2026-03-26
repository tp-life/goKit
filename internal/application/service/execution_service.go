package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"

	"go.uber.org/fx"
)

const (
	executionStatePendingOpen  = "pending_open"
	executionStateOpened       = "opened"
	executionStateOpenPartial  = "open_partial_failed"
	executionStateOpenFailed   = "open_failed"
	executionStateOpenHedging  = "open_hedging"
	executionStatePendingClose = "pending_close"
	executionStateClosed       = "closed"
	executionStateClosePartial = "close_partial_failed"
	executionStateCloseFailed  = "close_failed"
	executionStateCloseHedging = "close_hedging"
	executionStateRiskBlocked  = "risk_blocked"
	executionStateCircuitOpen  = "api_circuit_open"
)

type exchangeFailureState struct {
	consecutive int
	openUntil   time.Time
}

type ExecutionServiceParams struct {
	fx.In

	Cfg       Config
	Logger    *slog.Logger
	Store     *MarketStore
	PlanRepo  repository.ExecutionPlanRepository
	ExecRepo  repository.ExecutionRepository
	OrderRepo repository.OrderRepository
	Trades    []exchange.TradeAdapter `group:"trades"`
}

type ExecutionService struct {
	cfg             Config
	logger          *slog.Logger
	store           *MarketStore
	planRepo        repository.ExecutionPlanRepository
	execRepo        repository.ExecutionRepository
	orderRepo       repository.OrderRepository
	trades          map[string]exchange.TradeAdapter
	orderEventCh    chan exchange.OrderEvent
	mu              sync.Mutex
	exchangeFailure map[string]exchangeFailureState
}

func NewExecutionService(p ExecutionServiceParams) *ExecutionService {
	return &ExecutionService{
		cfg:             p.Cfg.normalize(),
		logger:          p.Logger,
		store:           p.Store,
		planRepo:        p.PlanRepo,
		execRepo:        p.ExecRepo,
		orderRepo:       p.OrderRepo,
		trades:          exchange.BuildTradeMap(p.Trades),
		orderEventCh:    make(chan exchange.OrderEvent, 256),
		exchangeFailure: make(map[string]exchangeFailureState),
	}
}

func StartExecutionEngine(lc fx.Lifecycle, svc *ExecutionService) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			runCtx, c := context.WithCancel(context.Background())
			cancel = c
			go svc.orderEventLoop(runCtx)
			svc.startTradeOrderStreams(runCtx)
			go svc.loop(runCtx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			return nil
		},
	})
}

func (s *ExecutionService) loop(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	ticker := time.NewTicker(s.cfg.Execution.LoopInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.cfg.Execution.AutoEntry {
				s.runAutoOpen(ctx)
			}
			if s.cfg.Execution.AutoClose {
				s.runAutoClose(ctx)
			}
		}
	}
}

func (s *ExecutionService) runAutoOpen(ctx context.Context) {
	plans, err := s.planRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		s.logger.Error("execution_auto_open_list_plans_failed", slog.Any("err", err))
		return
	}
	activeRecords, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		s.logger.Error("execution_auto_open_list_active_records_failed", slog.Any("err", err))
		return
	}

	candidates := make([]entity.ExecutionPlan, 0, len(plans))
	for _, plan := range plans {
		if !plan.ReadyNow || strings.ToLower(plan.Status) != "ready" {
			continue
		}
		rec, err := s.execRepo.FindByPlanKey(ctx, plan.PlanKey)
		if err != nil {
			s.logger.Error("execution_auto_open_load_record_failed", slog.String("plan_key", plan.PlanKey), slog.Any("err", err))
			continue
		}
		if rec != nil {
			continue
		}
		candidates = append(candidates, plan)
	}

	activeLiveCount := len(activeRecords)
	activeAllocatedNotional := s.sumAllocatedNotional(ctx, activeRecords)
	openAttempts := 0

	for idx, candidate := range candidates {
		if s.cfg.Execution.MaxAutoOpenPerLoop > 0 && openAttempts >= s.cfg.Execution.MaxAutoOpenPerLoop {
			break
		}
		if s.cfg.Execution.MaxLivePlans > 0 && activeLiveCount >= s.cfg.Execution.MaxLivePlans {
			break
		}

		planToOpen := candidate
		if s.cfg.Execution.AutoAllocateCapital {
			remainingBudget := s.cfg.EffectiveNotional() - activeAllocatedNotional
			if remainingBudget <= 0 {
				s.logger.Info(
					"execution_auto_open_budget_exhausted",
					slog.Float64("effective_notional", s.cfg.EffectiveNotional()),
					slog.Float64("active_allocated_notional", activeAllocatedNotional),
				)
				break
			}

			remainingTargets := len(candidates) - idx
			if s.cfg.Execution.MaxAutoOpenPerLoop > 0 {
				remainingTargets = minInt(remainingTargets, s.cfg.Execution.MaxAutoOpenPerLoop-openAttempts)
			}
			if s.cfg.Execution.MaxLivePlans > 0 {
				remainingTargets = minInt(remainingTargets, s.cfg.Execution.MaxLivePlans-activeLiveCount)
			}
			if remainingTargets <= 0 {
				break
			}

			scaledPlan, scaleErr := s.scalePlanForAutoBudget(candidate, remainingBudget/float64(remainingTargets))
			if scaleErr != nil {
				s.logger.Warn(
					"execution_auto_open_plan_skipped_after_budget_scale",
					slog.String("plan_key", candidate.PlanKey),
					slog.String("symbol", candidate.Symbol),
					slog.Any("err", scaleErr),
				)
				continue
			}
			planToOpen = scaledPlan
		}

		rec, openErr := s.openPlan(ctx, &planToOpen, "auto", s.cfg.Execution.Enabled)
		openAttempts++
		if rec != nil && countsTowardLivePlanLimit(*rec) {
			activeLiveCount++
			activeAllocatedNotional += s.allocatedNotionalForRecord(ctx, *rec, &planToOpen)
		}
		if openErr != nil {
			s.logger.Error("execution_auto_open_failed", slog.String("plan_key", planToOpen.PlanKey), slog.Any("err", openErr))
		}
	}
}

func (s *ExecutionService) runAutoClose(ctx context.Context) {
	records, err := s.execRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		s.logger.Error("execution_auto_close_list_records_failed", slog.Any("err", err))
		return
	}
	now := time.Now().UTC()
	for _, rec := range records {
		if !shouldAutoCloseRecord(rec) {
			continue
		}
		if !rec.AutoClose {
			continue
		}
		plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
		if err != nil {
			s.logger.Error("execution_auto_close_load_plan_failed", slog.String("plan_key", rec.PlanKey), slog.Any("err", err))
			continue
		}
		if plan == nil {
			s.logger.Warn("execution_auto_close_plan_missing", slog.String("plan_key", rec.PlanKey))
			continue
		}
		// 自动平仓现在不再只看“目标时间是否到了”。
		// 它会统一走 evaluateCloseDecision，把：
		// 1. schedule close（到时间）
		// 2. safety close（浮亏 / basis / 保证金缓冲恶化）
		// 放到同一个入口判定。
		decision, err := s.evaluateCloseDecision(ctx, now, rec, plan)
		if err != nil {
			s.logger.Error(
				"execution_auto_close_decision_failed",
				slog.String("plan_key", rec.PlanKey),
				slog.Any("err", err),
			)
			continue
		}
		if !decision.shouldClose {
			continue
		}
		// 这里使用 record 自身的 LiveTrading，而不是当前全局 execution.enabled。
		// 原因是：如果一条仓位已经真实打开，即便后面把“允许新开仓”关掉，
		// 自动平仓也仍然必须对这条 live 仓位执行真实 close，不能退化成 dry-run。
		if _, err := s.closePlan(ctx, plan, decision.trigger, rec.LiveTrading); err != nil {
			s.logger.Error("execution_auto_close_failed", slog.String("plan_key", rec.PlanKey), slog.Any("err", err))
			continue
		}
		s.logger.Info(
			"execution_auto_close_triggered",
			slog.String("plan_key", rec.PlanKey),
			slog.String("trigger", decision.trigger),
			slog.String("reason", decision.reason),
		)
	}
}

// shouldAutoCloseRecord 明确约束“哪些 execution record 可以进入自动平仓扫描”。
//
// 当前允许两类记录进入 auto-close：
// 1. 正常已打开完成的 `opened` / `dry_run_opened`；
// 2. open 阶段的异常尾部 `open_partial_failed` / `open_hedging`。
//
// 第 2 类不再像之前那样一律跳过，而是交给 evaluateCloseDecision()
// 作为“需要尽快执行 recovery close”的候选。
//
// 这么做的前提，是 trade adapter 的 ClosePosition 已改成“最多按 req.Quantity reduce-only 平仓”，
// 不再粗暴整仓 flatten。这样异常恢复可以更保守地推进，而不是无限期躺在数据库里。
func shouldAutoCloseRecord(rec entity.ExecutionRecord) bool {
	switch strings.ToLower(strings.TrimSpace(rec.Status)) {
	case executionStateOpened, "dry_run_opened", executionStateOpenPartial, executionStateOpenHedging:
		return true
	default:
		return false
	}
}

func (s *ExecutionService) ListLatest(ctx context.Context, limit int) ([]entity.ExecutionRecord, error) {
	return s.execRepo.ListLatest(ctx, limit)
}

func (s *ExecutionService) ListOrdersByPlanKey(ctx context.Context, planKey string) ([]entity.OrderRecord, error) {
	return s.orderRepo.ListByPlanKey(ctx, planKey)
}

func (s *ExecutionService) OpenByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error) {
	plan, err := s.planRepo.FindByPlanKey(ctx, planKey)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, ErrExecutionPlanNotFound
	}
	return s.openPlan(ctx, plan, "manual", s.cfg.Execution.Enabled)
}

func (s *ExecutionService) CloseByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error) {
	plan, err := s.planRepo.FindByPlanKey(ctx, planKey)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, ErrExecutionPlanNotFound
	}
	live := s.cfg.Execution.Enabled
	// 手动 close 和 auto close 一样，优先尊重“这条 execution record 当初是不是 live 仓位”。
	// 这样即使后续把 execution.enabled 关成 false，人工处理历史 live 仓位时也不会误走 dry-run。
	if rec, err := s.execRepo.FindByPlanKey(ctx, planKey); err != nil {
		return nil, err
	} else if rec != nil && rec.LiveTrading {
		live = true
	}
	return s.closePlan(ctx, plan, "manual", live)
}

func (s *ExecutionService) openPlan(ctx context.Context, plan *entity.ExecutionPlan, trigger string, live bool) (*entity.ExecutionRecord, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil execution plan")
	}
	rec, err := s.execRepo.FindByPlanKey(ctx, plan.PlanKey)
	if err != nil {
		return nil, err
	}
	if rec != nil && shouldShortCircuitExecutionAction(rec.Status, openExecutionPolicy.phase) {
		return rec, nil
	}
	if rec != nil && isExecutionActionPending(rec.Status, openExecutionPolicy.phase) {
		return rec, fmt.Errorf("%w: plan_key=%s status=%s", ErrExecutionActionInFlight, plan.PlanKey, rec.Status)
	}
	if err := validateExecutionAction(rec != nil, pickExecutionStatus(rec), openExecutionPolicy); err != nil {
		if rec == nil {
			rec = s.newExecutionRecord(plan, live)
		}
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	if rec == nil {
		rec = s.newExecutionRecord(plan, live)
	}
	if !live {
		// dry-run 的目标不是模拟所有腿级细节，而是给调用方一个“这条 plan
		// 现在被视为已打开”的稳定终态。因此这里直接写入 dry-run 终态，
		// 避免后续自动流程把它继续当成 pending 状态处理。
		if err := applyExecutionEvent(rec, executionEvent{
			Name:         "open_dry_run_completed",
			TargetStatus: openExecutionPolicy.dryRunStatus,
			Reason:       "dry-run open completed without sending live orders",
		}, openExecutionTransitions); err != nil {
			rec.LastError = err.Error()
			_ = s.execRepo.Upsert(ctx, rec)
			return rec, err
		}
		rec.OpenedAtMs = time.Now().UnixMilli()
		rec.LastError = ""
		if err := s.execRepo.Upsert(ctx, rec); err != nil {
			return nil, err
		}
		return rec, nil
	}
	// 真实开仓前先做一次“最后一分钟复核”。
	//
	// 这里刻意把 revalidation 放在账户余额/暴露风控之前：
	// - 它只依赖当前市场快照，不需要消耗交易 API；
	// - 能先把“机会已经过期/恶化”的 plan 拦掉；
	// - 也能减少在明显不该开仓时仍去拉余额、增加 API 压力。
	if err := s.revalidatePlanBeforeOpen(time.Now().UTC(), plan); err != nil {
		if transitionErr := applyExecutionEvent(rec, executionEvent{
			Name:         "open_revalidation_failed",
			TargetStatus: executionStateRiskBlocked,
			Reason:       err.Error(),
		}, openExecutionTransitions); transitionErr != nil {
			rec.LastError = transitionErr.Error()
			_ = s.execRepo.Upsert(ctx, rec)
			return rec, transitionErr
		}
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	if err := s.enforceRiskControls(ctx, plan); err != nil {
		if transitionErr := applyExecutionEvent(rec, executionEvent{
			Name:         executionErrorEvent(openExecutionPolicy.phase, err),
			TargetStatus: executionErrorStatus(err, executionStateRiskBlocked),
			Reason:       err.Error(),
		}, openExecutionTransitions); transitionErr != nil {
			rec.LastError = transitionErr.Error()
			_ = s.execRepo.Upsert(ctx, rec)
			return rec, transitionErr
		}
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	// live open 从这里进入显式 pending 状态。
	// 这样就算进程在真正下单前后中断，数据库里也至少能看到：
	// “这条记录已经进入 open 流程，但还没有拿到最终汇总结果”。
	if err := applyExecutionEvent(rec, executionEvent{
		Name:         "open_requested",
		TargetStatus: openExecutionPolicy.pendingStatus,
		Reason:       "live open request accepted and waiting for leg execution results",
	}, openExecutionTransitions); err != nil {
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	claimedRec, claimed, err := s.execRepo.TryClaimAction(ctx, rec, executionClaimableStatuses(openExecutionPolicy))
	if err != nil {
		return nil, err
	}
	if !claimed {
		if claimedRec != nil {
			return claimedRec, nil
		}
		return rec, nil
	}
	rec = claimedRec

	results, errMsg := s.placePlanOrders(ctx, plan, openExecutionPolicy.phase, trigger)
	finalStatus := summarizeExecutionStatus(results, openExecutionPolicy.phase)
	if shouldRecordOpenedAt(openExecutionPolicy.phase) && (finalStatus == executionStateOpened || finalStatus == executionStateOpenPartial || finalStatus == executionStateOpenHedging) {
		rec.OpenedAtMs = time.Now().UnixMilli()
	}
	rec.OpenOrderCount = len(results)
	rec.LastError = errMsg
	if err := applyExecutionEvent(rec, executionEvent{
		Name:         "open_results_applied",
		TargetStatus: finalStatus,
		Reason:       summarizeExecutionOutcomeReason(openExecutionPolicy.phase, finalStatus, len(results), errMsg),
	}, openExecutionTransitions); err != nil {
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	if errMsg != "" {
		return rec, fmt.Errorf("%s", errMsg)
	}
	return rec, nil
}

func (s *ExecutionService) closePlan(ctx context.Context, plan *entity.ExecutionPlan, trigger string, live bool) (*entity.ExecutionRecord, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil execution plan")
	}
	rec, err := s.execRepo.FindByPlanKey(ctx, plan.PlanKey)
	if err != nil {
		return nil, err
	}
	if rec != nil && shouldShortCircuitExecutionAction(rec.Status, closeExecutionPolicy.phase) {
		return rec, nil
	}
	if rec != nil && isExecutionActionPending(rec.Status, closeExecutionPolicy.phase) {
		return rec, fmt.Errorf("%w: plan_key=%s status=%s", ErrExecutionActionInFlight, plan.PlanKey, rec.Status)
	}
	if err := validateExecutionAction(rec != nil, pickExecutionStatus(rec), closeExecutionPolicy); err != nil {
		if rec == nil {
			rec = s.newExecutionRecord(plan, live)
		}
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	if rec == nil {
		rec = s.newExecutionRecord(plan, live)
	}
	if !live {
		// dry-run close 和 dry-run open 的意图一致：让记录进入一个稳定终态，
		// 便于前端、查询接口和自动流程用同一套逻辑读取。
		if err := applyExecutionEvent(rec, executionEvent{
			Name:         "close_dry_run_completed",
			TargetStatus: closeExecutionPolicy.dryRunStatus,
			Reason:       "dry-run close completed without sending live orders",
		}, closeExecutionTransitions); err != nil {
			rec.LastError = err.Error()
			_ = s.execRepo.Upsert(ctx, rec)
			return rec, err
		}
		rec.ClosedAtMs = time.Now().UnixMilli()
		rec.LastError = ""
		if err := s.execRepo.Upsert(ctx, rec); err != nil {
			return nil, err
		}
		return rec, nil
	}
	// close 先落 pending，表示系统已经决定尝试平仓，但双腿结果还没汇总完成。
	if err := applyExecutionEvent(rec, executionEvent{
		Name:         "close_requested",
		TargetStatus: closeExecutionPolicy.pendingStatus,
		Reason:       "live close request accepted and waiting for leg execution results",
	}, closeExecutionTransitions); err != nil {
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	claimedRec, claimed, err := s.execRepo.TryClaimAction(ctx, rec, executionClaimableStatuses(closeExecutionPolicy))
	if err != nil {
		return nil, err
	}
	if !claimed {
		if claimedRec != nil {
			return claimedRec, nil
		}
		return rec, nil
	}
	rec = claimedRec
	results, errMsg := s.placePlanOrders(ctx, plan, closeExecutionPolicy.phase, trigger)
	finalStatus := summarizeExecutionStatus(results, closeExecutionPolicy.phase)
	if shouldRecordClosedAt(closeExecutionPolicy.phase) && finalStatus == executionStateClosed {
		rec.ClosedAtMs = time.Now().UnixMilli()
	}
	rec.CloseOrderCount += len(results)
	rec.LastError = errMsg
	if err := applyExecutionEvent(rec, executionEvent{
		Name:         "close_results_applied",
		TargetStatus: finalStatus,
		Reason:       summarizeExecutionOutcomeReason(closeExecutionPolicy.phase, finalStatus, len(results), errMsg),
	}, closeExecutionTransitions); err != nil {
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	if errMsg != "" {
		return rec, fmt.Errorf("%s", errMsg)
	}
	return rec, nil
}

// summarizeExecutionOutcomeReason 负责给 execution 终态补一条简明的人类可读说明。
//
// 当前我们还没有单独的 execution_event 表，所以这条 reason 会先存在 record 上，
// 作为“最近一次状态变化为什么发生”的摘要。
// 后续如果真的引入事件表，这里也可以自然退化成 event summary 的生成器。
func summarizeExecutionOutcomeReason(phase, status string, resultCount int, errMsg string) string {
	phase = normalizeExecutionStatus(phase)
	status = normalizeExecutionStatus(status)
	switch {
	case strings.TrimSpace(errMsg) != "":
		return fmt.Sprintf("%s finished with status %s after %d order records; aggregated error: %s", phase, status, resultCount, errMsg)
	default:
		return fmt.Sprintf("%s finished with status %s after %d order records", phase, status, resultCount)
	}
}

func (s *ExecutionService) newExecutionRecord(plan *entity.ExecutionPlan, live bool) *entity.ExecutionRecord {
	return &entity.ExecutionRecord{
		PlanKey:               plan.PlanKey,
		BatchID:               plan.BatchID,
		OpportunityBatchID:    plan.OpportunityBatchID,
		Symbol:                plan.Symbol,
		LongExchange:          plan.LongExchange,
		ShortExchange:         plan.ShortExchange,
		LiveTrading:           live,
		AutoClose:             s.cfg.Execution.AutoClose,
		AllocatedNotionalUSDT: firstPositiveFloat(plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT),
		TargetCloseTimeMs:     plan.TargetCloseTimeMs,
	}
}

func summarizeExecutionStatus(results []entity.OrderRecord, phase string) string {
	switch normalizeExecutionStatus(phase) {
	case openExecutionPolicy.phase:
		return summarizeExecutionStatusWithPolicy(results, openExecutionPolicy)
	case closeExecutionPolicy.phase:
		return summarizeExecutionStatusWithPolicy(results, closeExecutionPolicy)
	default:
		return executionStateCloseFailed
	}
}

func (s *ExecutionService) placePlanOrders(ctx context.Context, plan *entity.ExecutionPlan, phase, trigger string) ([]entity.OrderRecord, string) {
	// legs 描述“当前 plan 在某个 phase 下，系统希望执行的两条主腿”。
	//
	// 这里显式展开 long / short 两腿，而不是在后面通过一堆分支临时推导，
	// 是为了让后续每个步骤都能用统一的腿级结构来处理：
	// 1. 生成 TradeOrderRequest
	// 2. 落 OrderRecord
	// 3. 做 reconcile
	// 4. 如有需要，生成 hedge rollback
	legs := []struct {
		role        string
		exchange    string
		side        string
		venueSymbol string
		qty         float64
		price       float64
	}{
		{role: "long_leg", exchange: plan.LongExchange, side: ternarySide(phase == "open", "BUY", "SELL"), venueSymbol: plan.LongVenueSymbol, qty: plan.LongQty, price: plan.LongEntryPrice},
		{role: "short_leg", exchange: plan.ShortExchange, side: ternarySide(phase == "open", "SELL", "BUY"), venueSymbol: plan.ShortVenueSymbol, qty: plan.ShortQty, price: plan.ShortEntryPrice},
	}

	// successfulLeg 只记录那些“主腿已经看起来成交成功”的结果。
	//
	// open 阶段如果出现“一条腿成功、一条腿失败”，系统需要基于这里保存的请求信息
	// 发起 hedge rollback。这个结构故意只保留 hedge 所需的最小信息，
	// 避免后续 rollback 再去从其他对象里反推。
	type exposedLeg struct {
		role     string
		exchange string
		req      exchange.TradeOrderRequest
	}

	// primaryLegResult 表示一条主腿执行完成后的汇总结果。
	//
	// 这里把“请求对象”“订单记录”“是否属于成功主腿”放在一起，
	// 是为了让主腿可以先并发执行，等所有 goroutine 结束后，再在主协程里：
	// 1. 顺序落库；
	// 2. 顺序写日志；
	// 3. 汇总 successes / errors；
	// 4. 决定是否进入 hedge rollback。
	type primaryLegResult struct {
		record      entity.OrderRecord
		errText     string
		exposureLeg *exposedLeg
	}

	// 对套利来说，两条主腿之间的时间差本身就是风险来源。
	// 因此这里把“主腿下单 + reconcile”改成并发执行：
	// - long / short 尽量同时把请求打出去，减少人为腿间时差；
	// - 所有结果收齐后，再统一汇总 execution status；
	// - 如果 open 阶段出现部分失败，再进入后续串行的 hedge rollback。
	//
	// 这里刻意没有把 hedge 也并发化，因为 hedge 是补救路径：
	// 它更看重“可解释、可复盘、顺序明确”，而不是极致吞吐。
	legResults := make([]primaryLegResult, len(legs))
	var wg sync.WaitGroup
	for idx, leg := range legs {
		wg.Add(1)
		go func(index int, leg struct {
			role        string
			exchange    string
			side        string
			venueSymbol string
			qty         float64
			price       float64
		}) {
			defer wg.Done()

			adapter := s.trades[strings.ToLower(leg.exchange)]
			meta, _ := s.store.Symbol(leg.exchange, plan.Symbol)
			book, _ := s.store.LatestBookTop(leg.exchange, plan.Symbol)
			req := s.buildTradeRequest(plan, phase, leg.role, leg.side, meta, book, leg.qty, leg.price)
			if strings.TrimSpace(trigger) != "" {
				req.Reason = trigger
			}
			orderRecord := entity.OrderRecord{
				PlanKey:         plan.PlanKey,
				ExecutionStatus: phase,
				Phase:           phase,
				LegRole:         leg.role,
				Exchange:        leg.exchange,
				Symbol:          plan.Symbol,
				VenueSymbol:     req.VenueSymbol,
				ClientOrderID:   req.ClientOrderID,
				Side:            req.Side,
				OrderType:       req.OrderType,
				TimeInForce:     req.TimeInForce,
				ReduceOnly:      req.ReduceOnly,
				RequestedQty:    req.Quantity,
				RequestedPrice:  req.Price,
				Status:          "PENDING",
			}

			if adapter == nil || !adapter.Enabled() {
				orderRecord.Status = "SKIPPED"
				orderRecord.ErrorMessage = fmt.Sprintf("trade adapter %s disabled or missing credentials", leg.exchange)
				legResults[index] = primaryLegResult{
					record:  orderRecord,
					errText: orderRecord.ErrorMessage,
				}
				return
			}
			if err := s.ensureExchangeAvailable(leg.exchange); err != nil {
				orderRecord.Status = "CIRCUIT_OPEN"
				orderRecord.ErrorMessage = err.Error()
				legResults[index] = primaryLegResult{
					record:  orderRecord,
					errText: orderRecord.ErrorMessage,
				}
				return
			}

			var (
				resp exchange.TradeOrderResult
				err  error
			)
			legCtx, cancel := s.primaryLegContext(ctx)
			defer cancel()
			if phase == "open" {
				resp, err = adapter.PlaceOrder(legCtx, req)
			} else {
				resp, err = adapter.ClosePosition(legCtx, req)
			}
			if err != nil {
				if s.isPrimaryLegTimeout(err) {
					// 主腿超时并不等于“交易所一定没有接到这笔单”。
					// 请求可能已经发出，只是响应在本地超时边界之后才回来。
					//
					// 因此这里不会立刻把它当成纯粹失败，而是：
					// 1. 先把记录标成 TIMEOUT；
					// 2. 再用同一个 client_order_id 做一轮 best-effort reconcile；
					// 3. 如果查单/查仓证明它其实已经成交，就把这条腿重新归回成功路径。
					orderRecord.Status = "TIMEOUT"
					orderRecord.ErrorMessage = err.Error()
					orderRecord = s.reconcileTimedOutPrimaryLeg(ctx, adapter, orderRecord, req)
					if isOrderFullySatisfied(orderRecord) {
						s.registerAPISuccess(leg.exchange)
						var exposure *exposedLeg
						if phase == "open" && orderHasOpenExposure(orderRecord) {
							exposure = &exposedLeg{role: leg.role, exchange: leg.exchange, req: req}
						}
						legResults[index] = primaryLegResult{
							record:      orderRecord,
							exposureLeg: exposure,
						}
						return
					}
				} else {
					orderRecord.Status = "ERROR"
					orderRecord.ErrorMessage = err.Error()
				}
				s.registerAPIFailure(leg.exchange)
				var exposure *exposedLeg
				if phase == "open" && orderHasOpenExposure(orderRecord) {
					exposure = &exposedLeg{role: leg.role, exchange: leg.exchange, req: req}
				}
				legResults[index] = primaryLegResult{
					record:      orderRecord,
					errText:     pickNonEmpty(orderRecord.ErrorMessage, fmt.Sprintf("%s:%s", leg.exchange, err.Error())),
					exposureLeg: exposure,
				}
				return
			}

			s.registerAPISuccess(leg.exchange)
			orderRecord.Status = pickNonEmpty(resp.Status, "SUBMITTED")
			orderRecord.VenueOrderID = resp.VenueOrderID
			orderRecord.ExecutedQty = resp.ExecutedQty
			orderRecord.AvgPrice = resp.AveragePrice
			orderRecord.RawResponse = resp.RawResponse
			orderRecord = s.reconcileOrder(ctx, adapter, orderRecord, req)

			var exposure *exposedLeg
			if phase == "open" && orderHasOpenExposure(orderRecord) {
				exposure = &exposedLeg{role: leg.role, exchange: leg.exchange, req: req}
			}
			errText := ""
			if !isOrderFullySatisfied(orderRecord) {
				errText = describeOrderAttention(orderRecord)
			}
			legResults[index] = primaryLegResult{
				record:      orderRecord,
				errText:     errText,
				exposureLeg: exposure,
			}
		}(idx, leg)
	}
	wg.Wait()

	results := make([]entity.OrderRecord, 0, 4)
	errors := make([]string, 0, 4)
	exposures := make([]exposedLeg, 0, 2)
	primaryNeedsRecovery := false
	for _, legResult := range legResults {
		orderRecord := legResult.record
		if !isOrderFullySatisfied(orderRecord) {
			primaryNeedsRecovery = true
		}
		if legResult.errText != "" {
			errors = append(errors, legResult.errText)
		}
		if legResult.exposureLeg != nil {
			exposures = append(exposures, *legResult.exposureLeg)
		}
		_ = s.orderRepo.Create(ctx, &orderRecord)
		results = append(results, orderRecord)
		s.logger.Info("execution_order_placed",
			slog.String("plan_key", plan.PlanKey),
			slog.String("phase", phase),
			slog.String("trigger", trigger),
			slog.String("exchange", orderRecord.Exchange),
			slog.String("symbol", plan.Symbol),
			slog.String("side", orderRecord.Side),
			slog.Float64("qty", orderRecord.RequestedQty),
			slog.String("status", orderRecord.Status),
		)
	}

	// 只有 open 阶段才会触发 hedge rollback。
	// close 阶段的失败暂时仍然只做记录，不做自动反向补救，
	// 因为“平仓失败后是否需要再开回去”在没有完整状态机时风险更高。
	if phase == "open" && primaryNeedsRecovery && len(exposures) > 0 {
		s.logger.Warn("execution_open_partial_failure_hedge_start",
			slog.String("plan_key", plan.PlanKey),
			slog.Int("exposed_legs", len(exposures)),
			slog.Int("error_legs", len(errors)),
		)
		for _, okLeg := range exposures {
			adapter := s.trades[strings.ToLower(okLeg.exchange)]
			hedgeReq := okLeg.req
			hedgeReq.ClientOrderID = buildClientOrderID(plan, "hedge", okLeg.role)
			hedgeReq.Reason = "open_leg_failed_hedge"
			hedgeReq.Side = reverseSide(hedgeReq.Side)
			hedgeReq.OrderType = "MARKET"
			hedgeReq.ReduceOnly = true
			hedgeReq.TimeInForce = "IOC"
			hedgeResp := exchange.TradeOrderResult{}
			hedgeErr := error(nil)
			rec := entity.OrderRecord{
				PlanKey:         plan.PlanKey,
				ExecutionStatus: "hedge_close",
				Phase:           "hedge_close",
				LegRole:         okLeg.role,
				Exchange:        okLeg.exchange,
				Symbol:          plan.Symbol,
				VenueSymbol:     hedgeReq.VenueSymbol,
				ClientOrderID:   hedgeReq.ClientOrderID,
				Side:            hedgeReq.Side,
				OrderType:       hedgeReq.OrderType,
				TimeInForce:     hedgeReq.TimeInForce,
				ReduceOnly:      true,
				RequestedQty:    hedgeReq.Quantity,
				RequestedPrice:  hedgeReq.Price,
				Status:          "PENDING",
			}
			if adapter == nil || !adapter.Enabled() {
				rec.Status = "SKIPPED"
				rec.ErrorMessage = fmt.Sprintf("hedge trade adapter %s disabled", okLeg.exchange)
				errors = append(errors, rec.ErrorMessage)
			} else {
				hedgeResp, hedgeErr = adapter.ClosePosition(ctx, hedgeReq)
				if hedgeErr != nil {
					s.registerAPIFailure(okLeg.exchange)
					rec.Status = "ERROR"
					rec.ErrorMessage = hedgeErr.Error()
					errors = append(errors, fmt.Sprintf("hedge:%s:%s", okLeg.exchange, hedgeErr.Error()))
				} else {
					s.registerAPISuccess(okLeg.exchange)
					rec.Status = pickNonEmpty(hedgeResp.Status, "SUBMITTED")
					rec.VenueOrderID = hedgeResp.VenueOrderID
					rec.ExecutedQty = hedgeResp.ExecutedQty
					rec.AvgPrice = hedgeResp.AveragePrice
					rec.RawResponse = hedgeResp.RawResponse
					rec = s.reconcileOrder(ctx, adapter, rec, hedgeReq)
				}
			}
			_ = s.orderRepo.Create(ctx, &rec)
			results = append(results, rec)
		}
	}

	return results, strings.Join(errors, " | ")
}

func (s *ExecutionService) reconcileOrder(ctx context.Context, adapter exchange.TradeAdapter, rec entity.OrderRecord, req exchange.TradeOrderRequest) entity.OrderRecord {
	attempts := s.cfg.Execution.OrderStatusPollAttempts
	for i := 0; i < attempts; i++ {
		// 第 1 层：优先相信订单状态接口。
		// 如果交易所能明确告诉我们“已成交 / 已撤销 / 仍挂单”，
		// 这通常比单纯看持仓更接近真实的订单生命周期。
		status, err := adapter.GetOrderStatus(ctx, exchange.OrderLookupRequest{
			CanonicalSymbol: req.CanonicalSymbol,
			VenueSymbol:     req.VenueSymbol,
			AssetID:         req.AssetID,
			ClientOrderID:   rec.ClientOrderID,
			VenueOrderID:    rec.VenueOrderID,
		})
		if err == nil {
			rec.Status = pickNonEmpty(status.Status, rec.Status)
			rec.VenueOrderID = pickNonEmpty(status.VenueOrderID, rec.VenueOrderID)
			rec.ExecutedQty = maxFloat(rec.ExecutedQty, status.ExecutedQty)
			rec.AvgPrice = maxFloat(rec.AvgPrice, status.AveragePrice)
			rec.RawResponse = pickNonEmpty(status.RawResponse, rec.RawResponse)
			if isSuccessfulOrderStatus(rec.Status, rec.ExecutedQty) {
				return rec
			}
			if status.Terminal {
				break
			}
		}
		if i < attempts-1 {
			time.Sleep(s.cfg.Execution.OrderStatusPollInterval)
		}
	}
	// 第 2 层：fallback 到仓位查询。
	//
	// 这是 v2 阶段的保守兜底策略：
	// - 如果订单状态接口不稳定、响应慢、或返回不完整，
	// - 但仓位已经明显变化，
	// 系统至少可以把“看起来已经成交”的订单从 PENDING/NEW 拉到更接近真实的状态。
	pos, err := adapter.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err == nil {
		if req.ReduceOnly {
			if closedPositionSatisfiesRequest(pos.Quantity, req.Quantity) {
				rec.Status = "FILLED"
				rec.ExecutedQty = maxFloat(rec.ExecutedQty, req.Quantity)
			}
		} else if openedPositionSatisfiesRequest(pos.Quantity, req.Quantity) {
			rec.Status = "FILLED"
			rec.ExecutedQty = maxFloat(rec.ExecutedQty, req.Quantity)
		}
	}
	if hasMeaningfulExecutedQty(rec.ExecutedQty, req.Quantity) && !isOrderFullySatisfied(rec) {
		// 当系统已经观察到部分成交量，但还没拿到一个明确终态时，
		// 最保守的表达是 PARTIALLY_FILLED，而不是继续保留 NEW/PENDING。
		rec.Status = "PARTIALLY_FILLED"
	}
	return rec
}

func (s *ExecutionService) primaryLegContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := s.cfg.Execution.PrimaryLegTimeout
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (s *ExecutionService) isPrimaryLegTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// reconcileTimedOutPrimaryLeg 处理一种很常见但也很危险的边界：
// “请求方看来超时了，但交易所不一定真的没接到单。”
//
// 对套利腿来说，直接把这种超时当纯失败会有两个问题：
// 1. 可能误触发 hedge，而实际上这条腿已经成交；
// 2. 也可能把真实已下出的订单留在交易所，系统却以为它不存在。
//
// 所以这里会立刻复用原本的 reconcile 逻辑再查一遍：
// - 优先查订单状态；
// - 查不到再看仓位是否已经变化。
//
// 如果 reconcile 证明这笔单其实已经成功，ErrorMessage 会被清空，
// 这样它就能重新回到正常的 execution 汇总路径。
func (s *ExecutionService) reconcileTimedOutPrimaryLeg(ctx context.Context, adapter exchange.TradeAdapter, rec entity.OrderRecord, req exchange.TradeOrderRequest) entity.OrderRecord {
	rec = s.reconcileOrder(ctx, adapter, rec, req)
	if isOrderFullySatisfied(rec) {
		rec.ErrorMessage = ""
	}
	return rec
}

func (s *ExecutionService) enforceRiskControls(ctx context.Context, plan *entity.ExecutionPlan) error {
	// 这里的风控仍然属于“v2 基础安全版”，目标不是穷尽所有风险，
	// 而是在真正发单之前，先把最容易导致明显失控的路径挡住：
	// 1. adapter/venue 当前不可用；
	// 2. 账户权益或可用余额明显不足；
	// 3. 这次开仓会让 symbol / exchange 暴露超过配置上限。
	legs := []struct {
		exchange string
		venue    string
		assetID  string
		qty      float64
		price    float64
	}{
		{exchange: plan.LongExchange, venue: plan.LongVenueSymbol, qty: plan.LongQty, price: plan.LongEntryPrice},
		{exchange: plan.ShortExchange, venue: plan.ShortVenueSymbol, qty: plan.ShortQty, price: plan.ShortEntryPrice},
	}
	var symbolExposure float64
	exchangeExposure := map[string]float64{}
	for _, leg := range legs {
		adapter := s.trades[strings.ToLower(leg.exchange)]
		if adapter == nil || !adapter.Enabled() {
			return fmt.Errorf("risk check: trade adapter %s disabled", leg.exchange)
		}
		if err := s.ensureExchangeAvailable(leg.exchange); err != nil {
			return err
		}
		acct, err := adapter.GetAccountSnapshot(ctx)
		if err != nil {
			s.registerAPIFailure(leg.exchange)
			return fmt.Errorf("risk check account %s failed: %w", leg.exchange, err)
		}
		s.registerAPISuccess(leg.exchange)
		notional := math.Abs(leg.qty * leg.price)
		if s.cfg.Execution.MinAccountEquityUSDT > 0 && acct.Equity > 0 && acct.Equity < s.cfg.Execution.MinAccountEquityUSDT {
			return fmt.Errorf("risk check: %s equity %.4f below minimum %.4f", leg.exchange, acct.Equity, s.cfg.Execution.MinAccountEquityUSDT)
		}
		if acct.Equity > 0 && acct.AvailableBalance/acct.Equity < s.cfg.Execution.MinAvailableBalanceRatio {
			return fmt.Errorf("risk check: %s available ratio %.4f below minimum %.4f", leg.exchange, acct.AvailableBalance/acct.Equity, s.cfg.Execution.MinAvailableBalanceRatio)
		}
		if acct.AvailableBalance > 0 && acct.AvailableBalance < notional/math.Max(s.cfg.Leverage, 1) {
			return fmt.Errorf("risk check: %s available balance %.4f below required margin %.4f", leg.exchange, acct.AvailableBalance, notional/math.Max(s.cfg.Leverage, 1))
		}
		pos, err := adapter.GetPosition(ctx, plan.Symbol, leg.venue, leg.assetID)
		if err == nil {
			ref := firstPositiveFloat(pos.MarkPrice, pos.EntryPrice, leg.price)
			existing := math.Abs(pos.Quantity * ref)
			symbolExposure += existing + notional
			exchangeExposure[strings.ToLower(leg.exchange)] += existing + notional
		} else {
			symbolExposure += notional
			exchangeExposure[strings.ToLower(leg.exchange)] += notional
		}
	}
	if s.cfg.Execution.MaxSingleSymbolExposureUSDT > 0 && symbolExposure > s.cfg.Execution.MaxSingleSymbolExposureUSDT {
		return fmt.Errorf("risk check: symbol exposure %.4f exceeds limit %.4f", symbolExposure, s.cfg.Execution.MaxSingleSymbolExposureUSDT)
	}
	if s.cfg.Execution.MaxSingleExchangeExposureUSDT > 0 {
		for name, exposure := range exchangeExposure {
			if exposure > s.cfg.Execution.MaxSingleExchangeExposureUSDT {
				return fmt.Errorf("risk check: exchange %s exposure %.4f exceeds limit %.4f", name, exposure, s.cfg.Execution.MaxSingleExchangeExposureUSDT)
			}
		}
	}
	return nil
}

func (s *ExecutionService) ensureExchangeAvailable(exchangeName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.exchangeFailure[strings.ToLower(exchangeName)]
	if !state.openUntil.IsZero() && time.Now().Before(state.openUntil) {
		return fmt.Errorf("%s: %s until %s", executionStateCircuitOpen, exchangeName, state.openUntil.UTC().Format(time.RFC3339))
	}
	return nil
}

func (s *ExecutionService) registerAPIFailure(exchangeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(exchangeName)
	state := s.exchangeFailure[key]
	state.consecutive++
	if state.consecutive >= s.cfg.Execution.APIFailureThreshold {
		state.openUntil = time.Now().Add(s.cfg.Execution.APIFailureCooldown)
		state.consecutive = 0
	}
	s.exchangeFailure[key] = state
}

func (s *ExecutionService) registerAPISuccess(exchangeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(exchangeName)
	state := s.exchangeFailure[key]
	state.consecutive = 0
	state.openUntil = time.Time{}
	s.exchangeFailure[key] = state
}

func (s *ExecutionService) buildTradeRequest(plan *entity.ExecutionPlan, phase, legRole, side string, meta entity.Symbol, book entity.BookTopSnapshot, qty, refPrice float64) exchange.TradeOrderRequest {
	// buildTradeRequest 的职责是把“策略/计划语义”翻译成“交易所请求语义”。
	//
	// 这里有两个容易混淆的层次：
	// 1. plan 决定的是“现在应该以 maker / taker / mixed 哪种模式发单”；
	// 2. adapter capabilities 决定的是“某个 venue 的 taker/maker 应该如何落到订单类型和 TIF 上”。
	//
	// 例如：
	// - 某些 venue 的 taker 可以直接用 MARKET；
	// - 某些 venue 更适合 LIMIT + IOC + aggressive price；
	// - maker 的 post-only TIF 也可能不是同一个值。
	//
	// 这样拆开以后，ExecutionService 不再需要知道“Hyperliquid 特判长什么样”，
	// 而是只消费 adapter 对外暴露的统一能力描述。
	mode := plan.EntryMode
	if phase == "close" {
		mode = plan.ExitMode
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	caps := s.tradeCapabilities(meta.Exchange)
	orderType := "LIMIT"
	tif := "GTC"
	price := refPrice

	switch mode {
	case "maker":
		if strings.EqualFold(side, "BUY") {
			if book.BidPrice > 0 {
				price = book.BidPrice
			}
		} else {
			if book.AskPrice > 0 {
				price = book.AskPrice
			}
		}
		tif = pickNonEmpty(caps.MakerLimitTIF, "GTX")
	case "taker":
		if caps.TakerUsesAggressiveIOC {
			orderType = pickNonEmpty(caps.TakerOrderType, "LIMIT")
			tif = pickNonEmpty(caps.TakerTimeInForce, "IOC")
			price = aggressivePrice(side, book, refPrice)
		} else {
			orderType = pickNonEmpty(caps.TakerOrderType, "MARKET")
			tif = caps.TakerTimeInForce
			price = 0
		}
	default:
		orderType = "LIMIT"
		tif = "IOC"
		price = aggressivePrice(side, book, refPrice)
	}

	qty = roundDownStep(qty, meta.StepSize)
	return exchange.TradeOrderRequest{
		CanonicalSymbol: plan.Symbol,
		VenueSymbol:     meta.VenueSymbol,
		AssetID:         meta.VenueAssetID,
		Side:            side,
		OrderType:       orderType,
		TimeInForce:     tif,
		Quantity:        round8(qty),
		Price:           round8(price),
		ReduceOnly:      phase == "close",
		ClientOrderID:   buildClientOrderID(plan, phase, legRole),
		Reason:          phase,
	}
}

func (s *ExecutionService) tradeCapabilities(exchangeName string) exchange.TradeCapabilities {
	if s != nil && s.trades != nil {
		if adapter, ok := s.trades[strings.ToLower(strings.TrimSpace(exchangeName))]; ok && adapter != nil {
			return adapter.Capabilities()
		}
	}
	return exchange.TradeCapabilities{
		MakerLimitTIF:          "GTX",
		TakerOrderType:         "MARKET",
		TakerUsesAggressiveIOC: false,
	}
}

func aggressivePrice(side string, book entity.BookTopSnapshot, fallback float64) float64 {
	ref := fallback
	if strings.EqualFold(side, "BUY") {
		if book.AskPrice > 0 {
			ref = book.AskPrice
		}
		if ref <= 0 {
			ref = 1
		}
		return ref * 1.002
	}
	if book.BidPrice > 0 {
		ref = book.BidPrice
	}
	if ref <= 0 {
		ref = 1
	}
	return ref * 0.998
}

func buildClientOrderID(plan *entity.ExecutionPlan, phase, legRole string) string {
	prefix := plan.PlanKey
	if len(prefix) > 10 {
		prefix = prefix[:10]
	}
	raw := fmt.Sprintf("%s-%s-%s-%s-%d", strings.ToLower(plan.Symbol), phase, legRole, prefix, time.Now().UnixMilli()%1_000_000)
	if len(raw) > 36 {
		return raw[:36]
	}
	return raw
}

func ternarySide(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func reverseSide(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY":
		return "SELL"
	case "SELL":
		return "BUY"
	default:
		return side
	}
}

func pickNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isSuccessfulOrderStatus(status string, executedQty float64) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "NO_POSITION":
		return true
	default:
		return false
	}
}

func isFailedOrderStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ERROR", "REJECTED", "SKIPPED", "EXPIRED", "CANCELED", "CANCELLED", "CIRCUIT_OPEN", "TIMEOUT":
		return true
	default:
		return false
	}
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func countsTowardLivePlanLimit(rec entity.ExecutionRecord) bool {
	if !rec.LiveTrading {
		return false
	}
	switch normalizeExecutionStatus(rec.Status) {
	case executionStatePendingOpen, executionStateOpened, executionStateOpenPartial, executionStateOpenHedging, executionStatePendingClose, executionStateClosePartial, executionStateCloseFailed, executionStateCloseHedging:
		return true
	default:
		return false
	}
}

func (s *ExecutionService) sumAllocatedNotional(ctx context.Context, records []entity.ExecutionRecord) float64 {
	var total float64
	for _, rec := range records {
		total += s.allocatedNotionalForRecord(ctx, rec, nil)
	}
	return total
}

func (s *ExecutionService) allocatedNotionalForRecord(ctx context.Context, rec entity.ExecutionRecord, fallbackPlan *entity.ExecutionPlan) float64 {
	if rec.AllocatedNotionalUSDT > 0 {
		return rec.AllocatedNotionalUSDT
	}
	if fallbackPlan != nil && fallbackPlan.PlanKey == rec.PlanKey {
		return firstPositiveFloat(fallbackPlan.RoundedNotionalUSDT, fallbackPlan.TargetNotionalUSDT)
	}
	plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
	if err == nil && plan != nil {
		return firstPositiveFloat(plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT)
	}
	return 0
}

func (s *ExecutionService) scalePlanForAutoBudget(plan entity.ExecutionPlan, targetNotional float64) (entity.ExecutionPlan, error) {
	if targetNotional <= 0 {
		return entity.ExecutionPlan{}, fmt.Errorf("target notional must be positive")
	}
	longMeta, okLong := s.store.Symbol(plan.LongExchange, plan.Symbol)
	shortMeta, okShort := s.store.Symbol(plan.ShortExchange, plan.Symbol)
	if !okLong || !okShort {
		return entity.ExecutionPlan{}, fmt.Errorf("missing symbol metadata for auto allocation %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	if plan.LongEntryPrice <= 0 || plan.ShortEntryPrice <= 0 {
		return entity.ExecutionPlan{}, fmt.Errorf("missing entry prices for auto allocation %s", plan.Symbol)
	}

	longQty, shortQty, longNotional, shortNotional, ok := computeCommonLegQuantities(
		targetNotional,
		plan.LongEntryPrice,
		plan.ShortEntryPrice,
		longMeta.StepSize,
		shortMeta.StepSize,
	)
	if !ok {
		return entity.ExecutionPlan{}, fmt.Errorf("auto allocation %s cannot compute common quantities for target notional %.2f", plan.Symbol, targetNotional)
	}

	scaled := plan
	scaled.TargetNotionalUSDT = round2(targetNotional)
	scaled.LongQty = round8(longQty)
	scaled.ShortQty = round8(shortQty)
	scaled.RoundedNotionalUSDT = round2(math.Min(longNotional, shortNotional))

	if scaled.LongQty < scaled.LongMinQty || scaled.ShortQty < scaled.ShortMinQty {
		return entity.ExecutionPlan{}, fmt.Errorf(
			"auto allocation %s target notional %.2f drops below min qty long=%.6f/%.6f short=%.6f/%.6f",
			plan.Symbol,
			targetNotional,
			scaled.LongQty,
			scaled.LongMinQty,
			scaled.ShortQty,
			scaled.ShortMinQty,
		)
	}
	if longNotional < scaled.LongMinNotionalUSDT || shortNotional < scaled.ShortMinNotionalUSDT {
		return entity.ExecutionPlan{}, fmt.Errorf(
			"auto allocation %s target notional %.2f drops below min notional long=%.2f/%.2f short=%.2f/%.2f",
			plan.Symbol,
			targetNotional,
			longNotional,
			scaled.LongMinNotionalUSDT,
			shortNotional,
			scaled.ShortMinNotionalUSDT,
		)
	}

	baseNotional := firstPositiveFloat(plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT)
	actualNotional := scaled.RoundedNotionalUSDT
	if baseNotional > 0 && actualNotional > 0 {
		scale := actualNotional / baseNotional
		scaled.FundingCarryPNL = round2(plan.FundingCarryPNL * scale)
		scaled.EntryFeePNL = round2(plan.EntryFeePNL * scale)
		scaled.ExitFeePNL = round2(plan.ExitFeePNL * scale)
		scaled.SlippagePNL = round2(actualNotional * (plan.EntryPenaltyBps + plan.ExitPenaltyBps) / 10000)
		scaled.SafetyBufferPNL = round2(s.cfg.SafetyBufferUSDT + actualNotional*plan.HedgePenaltyBps/10000)
		scaled.NetExpectedPNL = round2(scaled.FundingCarryPNL - scaled.EntryFeePNL - scaled.ExitFeePNL - scaled.SlippagePNL - scaled.SafetyBufferPNL)
		if actualNotional > 0 {
			scaled.NetExpectedPNLBps = round4(scaled.NetExpectedPNL / actualNotional * 10000)
		} else {
			scaled.NetExpectedPNLBps = 0
		}
	}
	return scaled, nil
}

func executionClaimableStatuses(policy executionPhasePolicy) []string {
	out := make([]string, 0, len(policy.startableFrom))
	for status := range policy.startableFrom {
		out = append(out, status)
	}
	return out
}
