package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"strconv"
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

	// Aster 官方文档里把 -4015 明确写成 “Client order id length should not be more than 36 chars”。
	// 这里把 36 作为 Binance-like venue 的统一硬限制，避免执行层分别维护多套几乎相同的约束。
	maxClientOrderIDLen = 36
	// 拆单子单会在基础 id 后追加 `-pN`，因此基础 id 需要预留一小段长度空间。
	// 这里保守预留 6 个字符，足够覆盖 `-p9999` 这类常见拆单后缀。
	maxClientOrderIDBaseLen = 30
)

type exchangeFailureState struct {
	consecutive int
	openUntil   time.Time
}

type ExecutionServiceParams struct {
	fx.In

	Cfg        Config
	Logger     *slog.Logger
	Store      *MarketStore
	MarketRepo repository.MarketDataRepository
	PlanRepo   repository.ExecutionPlanRepository
	ExecRepo   repository.ExecutionRepository
	OrderRepo  repository.OrderRepository
	Trades     []exchange.TradeAdapter `group:"trades"`
}

type ExecutionService struct {
	cfg             Config
	logger          *slog.Logger
	store           *MarketStore
	marketRepo      repository.MarketDataRepository
	planRepo        repository.ExecutionPlanRepository
	execRepo        repository.ExecutionRepository
	orderRepo       repository.OrderRepository
	trades          map[string]exchange.TradeAdapter
	orderEventCh    chan exchange.OrderEvent
	mu              sync.Mutex
	exchangeFailure map[string]exchangeFailureState
	startedAt       time.Time
	autoOpenWarm    bool
	warmupLogged    bool
}

func NewExecutionService(p ExecutionServiceParams) *ExecutionService {
	return &ExecutionService{
		cfg:             p.Cfg.normalize(),
		logger:          p.Logger,
		store:           p.Store,
		marketRepo:      p.MarketRepo,
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
			svc.markStarted(time.Now().UTC())
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
			if _, err := s.ReconcileLivePositions(ctx); err != nil {
				s.logger.Error("execution_live_position_reconcile_loop_failed", slog.Any("err", err))
			}
			if s.cfg.Execution.AutoClose {
				s.runRollingMonitor(ctx)
				s.runActiveReplacement(ctx)
			}
			if s.cfg.Execution.AutoEntry {
				s.runAutoOpen(ctx)
			}
			if s.cfg.Execution.AutoClose {
				s.runAutoClose(ctx)
			}
		}
	}
}

func (s *ExecutionService) markStarted(startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startedAt = startedAt
	s.autoOpenWarm = false
	s.warmupLogged = false
}

func (s *ExecutionService) runAutoOpen(ctx context.Context) {
	plans, err := s.planRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		s.logger.Error("execution_auto_open_list_plans_failed", slog.Any("err", err))
		return
	}
	if !s.ensureFreshPlanBatchAfterStart(plans) {
		return
	}
	activeRecords, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		s.logger.Error("execution_auto_open_list_active_records_failed", slog.Any("err", err))
		return
	}
	activeRollingGroups := collectActiveRollingGroups(activeRecords)

	candidates := make([]entity.ExecutionPlan, 0, len(plans))
	for _, plan := range plans {
		// 先做一轮轻量过滤，把明显不应参与 auto-open 的计划挡掉。
		// 这样后面的预算分配和 open 调用只会面对“当前真的可能开出去”的候选。
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
		if shouldBlockRollingAutoOpen(plan, activeRollingGroups) {
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

			// 自动预算模式不让第一条 candidate 独占全部剩余额度。
			// 这里按“本轮剩余可开目标数”均摊预算，让同一轮里的多个 ready plan
			// 至少有机会各自拿到一份可执行的目标名义。
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
			// 一旦某条 plan 成功占用了 live slot，就立刻回写本轮 loop 的局部统计。
			// 这样后续候选会立即感知：
			// - 剩余预算减少了
			// - 剩余 live slot 变少了
			// - rolling group 可能已经被占住了
			activeLiveCount++
			activeAllocatedNotional += s.allocatedNotionalForRecord(ctx, *rec, &planToOpen)
			if normalizedPlanStrategyMode(planToOpen) == StrategyModeRollingCycleAligned && strings.TrimSpace(planToOpen.RollingGroupKey) != "" {
				activeRollingGroups[planToOpen.RollingGroupKey] = struct{}{}
			}
		}
		if openErr != nil {
			s.logger.Error("execution_auto_open_failed", slog.String("plan_key", planToOpen.PlanKey), slog.Any("err", openErr))
		}
	}
}

// ensureFreshPlanBatchAfterStart 防止服务刚重启时，自动开仓继续消费“重启前残留的旧批次计划”。
//
// 背景：
//   - StrategyRunner 会按 opportunity_calc_interval 周期生成新的 opportunity / plan batch；
//   - ExecutionService 的 auto-open 循环则会持续读取 execution_plans 最新批次；
//   - 如果服务刚重启，而数据库里恰好还躺着一批旧模式/旧参数生成的 ready plan，
//     就可能在“新批次尚未生成”的几秒内被误开出去。
//
// 这里的策略是：
// - 只有当最新 plan batch 的 AsOfTimeMs 已经晚于本次服务启动时间，才允许 auto-open；
// - 一旦看到过一批“启动后生成”的 plan，后续本次进程内就不再重复卡这道门。
func (s *ExecutionService) ensureFreshPlanBatchAfterStart(plans []entity.ExecutionPlan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.autoOpenWarm {
		return true
	}
	if s.startedAt.IsZero() {
		s.autoOpenWarm = true
		return true
	}

	startedAtMs := s.startedAt.UnixMilli()
	latestAsOfMs := int64(0)
	for _, plan := range plans {
		if plan.AsOfTimeMs > latestAsOfMs {
			latestAsOfMs = plan.AsOfTimeMs
		}
	}
	if latestAsOfMs >= startedAtMs {
		s.autoOpenWarm = true
		s.warmupLogged = false
		s.logger.Info(
			"execution_auto_open_fresh_plan_batch_ready",
			slog.Time("started_at", s.startedAt),
			slog.Int64("latest_plan_as_of_ms", latestAsOfMs),
		)
		return true
	}
	if !s.warmupLogged {
		s.warmupLogged = true
		s.logger.Info(
			"execution_auto_open_waiting_for_fresh_plan_batch",
			slog.Time("started_at", s.startedAt),
			slog.Int64("latest_plan_as_of_ms", latestAsOfMs),
		)
	}
	return false
}

// collectActiveRollingGroups 提前把“当前仍然活跃的 rolling 仓位组”收集出来。
//
// auto-open 在同一轮扫描里会读取一批 ready plan。
// 如果不先做这层分组去重，就可能出现：
// - 老仓位还没平；
// - 新方向 plan 已经 ready；
// - 系统又把同一组的 successor 提前开出来；
// 最终同一币种 / 同一交易所对出现双持仓。
func collectActiveRollingGroups(records []entity.ExecutionRecord) map[string]struct{} {
	out := make(map[string]struct{}, len(records))
	for _, rec := range records {
		if normalizeStrategyMode(rec.StrategyMode, "") != StrategyModeRollingCycleAligned {
			continue
		}
		key := strings.TrimSpace(rec.RollingGroupKey)
		if key == "" {
			continue
		}
		out[key] = struct{}{}
	}
	return out
}

func shouldBlockRollingAutoOpen(plan entity.ExecutionPlan, activeRollingGroups map[string]struct{}) bool {
	if normalizedPlanStrategyMode(plan) != StrategyModeRollingCycleAligned {
		return false
	}
	key := strings.TrimSpace(plan.RollingGroupKey)
	if key == "" {
		return false
	}
	_, blocked := activeRollingGroups[key]
	return blocked
}

func normalizedPlanStrategyMode(plan entity.ExecutionPlan) string {
	return normalizeStrategyMode(plan.StrategyMode, StrategyModeLegacyProjection)
}

func (s *ExecutionService) runAutoClose(ctx context.Context) {
	records, err := s.listAutoCloseCandidates(ctx)
	if err != nil {
		s.logger.Error("execution_auto_close_list_records_failed", slog.Any("err", err))
		return
	}
	now := time.Now().UTC()
	for _, rec := range records {
		candidate := s.evaluateAutoCloseCandidate(ctx, now, rec)
		if candidate.Decision.Error != "" {
			s.logger.Error(
				"execution_auto_close_decision_failed",
				slog.String("plan_key", rec.PlanKey),
				slog.String("err", candidate.Decision.Error),
			)
			continue
		}
		if !candidate.Decision.ShouldClose || candidate.Plan == nil {
			continue
		}
		// 这里使用 record 自身的 LiveTrading，而不是当前全局 execution.enabled。
		// 原因是：如果一条仓位已经真实打开，即便后面把“允许新开仓”关掉，
		// 自动平仓也仍然必须对这条 live 仓位执行真实 close，不能退化成 dry-run。
		if _, err := s.closePlan(ctx, candidate.Plan, candidate.Decision.Trigger, rec.LiveTrading); err != nil {
			s.logger.Error("execution_auto_close_failed", slog.String("plan_key", rec.PlanKey), slog.Any("err", err))
			continue
		}
		s.logger.Info(
			"execution_auto_close_triggered",
			slog.String("plan_key", rec.PlanKey),
			slog.String("trigger", candidate.Decision.Trigger),
			slog.String("reason", candidate.Decision.Reason),
		)
	}
}

func (s *ExecutionService) listAutoCloseCandidates(ctx context.Context) ([]entity.ExecutionRecord, error) {
	latest, err := s.execRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		return nil, err
	}
	activeLive, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		return nil, err
	}

	merged := make([]entity.ExecutionRecord, 0, len(latest)+len(activeLive))
	seen := make(map[string]struct{}, len(latest)+len(activeLive))
	for _, item := range latest {
		merged = append(merged, item)
		seen[item.PlanKey] = struct{}{}
	}
	for _, item := range activeLive {
		if _, ok := seen[item.PlanKey]; ok {
			continue
		}
		merged = append(merged, item)
		seen[item.PlanKey] = struct{}{}
	}
	return merged, nil
}

// shouldAutoCloseRecord 明确约束“哪些 execution record 可以进入自动平仓扫描”。
//
// 当前允许三类记录进入 auto-close：
// 1. 正常已打开完成的 `opened` / `dry_run_opened`；
// 2. open 阶段的异常尾部 `open_partial_failed` / `open_hedging`。
// 3. close 阶段仍然带 live 风险的 `close_failed` / `close_partial_failed` / `close_hedging`。
//
// 第 2 类会交给 evaluateCloseDecision() 作为 recovery close；
// 第 3 类会交给 evaluateCloseDecision() 作为 retry close。
//
// 这么做的前提，是 trade adapter 的 ClosePosition 已改成“最多按 req.Quantity reduce-only 平仓”，
// 不再粗暴整仓 flatten。这样异常恢复可以更保守地推进，而不是无限期躺在数据库里。
func shouldAutoCloseRecord(rec entity.ExecutionRecord) bool {
	switch strings.ToLower(strings.TrimSpace(rec.Status)) {
	case executionStateOpened, "dry_run_opened", executionStateOpenPartial, executionStateOpenHedging, executionStateClosePartial, executionStateCloseFailed, executionStateCloseHedging:
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
	if err := s.revalidatePlanBeforeOpen(time.Now().UTC(), plan, trigger); err != nil {
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
	if finalStatus == executionStateOpened {
		s.armSameExchangeProtection(ctx, plan, rec)
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
	if finalStatus == executionStateClosed {
		s.cancelSameExchangeProtection(ctx, plan)
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
		RollingGroupKey:       plan.RollingGroupKey,
		Symbol:                plan.Symbol,
		LongExchange:          plan.LongExchange,
		ShortExchange:         plan.ShortExchange,
		ArbitrageMode:         normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange)),
		StrategyMode:          normalizedPlanStrategyMode(*plan),
		LiveTrading:           live,
		AutoClose:             s.cfg.Execution.AutoClose,
		AllocatedNotionalUSDT: firstPositiveFloat(plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT),
		TargetCloseTimeMs:     plan.TargetCloseTimeMs,
		NextReviewTimeMs:      plan.NextReviewTimeMs,
		CurrentSyncBoundaryMs: plan.SyncBoundaryTimeMs,
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
	buildOrderRecord := func(execStatus, phaseName, legRole, exchangeName string, reduceOnly bool, req exchange.TradeOrderRequest) entity.OrderRecord {
		return entity.OrderRecord{
			PlanKey:         plan.PlanKey,
			ExecutionStatus: execStatus,
			Phase:           phaseName,
			LegRole:         legRole,
			Exchange:        exchangeName,
			Symbol:          plan.Symbol,
			VenueSymbol:     req.VenueSymbol,
			ClientOrderID:   req.ClientOrderID,
			Side:            req.Side,
			OrderType:       req.OrderType,
			TimeInForce:     req.TimeInForce,
			ReduceOnly:      reduceOnly,
			RequestedQty:    req.Quantity,
			RequestedPrice:  req.Price,
			Status:          "PENDING",
		}
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
		records      []entity.OrderRecord
		errTexts     []string
		exposureLegs []exposedLeg
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
			orderRecord := buildOrderRecord(phase, phase, leg.role, leg.exchange, req.ReduceOnly, req)

			if adapter == nil || !adapter.Enabled() {
				orderRecord.Status = "SKIPPED"
				orderRecord.ErrorMessage = fmt.Sprintf("trade adapter %s disabled or missing credentials", leg.exchange)
				legResults[index] = primaryLegResult{
					records:  []entity.OrderRecord{orderRecord},
					errTexts: []string{orderRecord.ErrorMessage},
				}
				return
			}
			if err := s.ensureExchangeAvailable(leg.exchange); err != nil {
				orderRecord.Status = "CIRCUIT_OPEN"
				orderRecord.ErrorMessage = err.Error()
				legResults[index] = primaryLegResult{
					records:  []entity.OrderRecord{orderRecord},
					errTexts: []string{orderRecord.ErrorMessage},
				}
				return
			}

			// 交易所如果声明了单笔 maxQty，这里必须先拆单再发。
			//
			// 背后的原因是：
			// 1. 执行层目标是“完成整条套利腿”，不是随便打一笔能过的单；
			// 2. 如果只把请求数量截断到 maxQty，系统会误以为整条腿都已经提交，
			//    实际上却只开出一部分，单腿风险更高；
			// 3. 因此这里会把一条腿拆成多笔子单，并让每笔子单都进入原有的
			//    reconcile / 落库 / hedge rollback 路径。
			splitReqs, splitErr := splitTradeRequestsByVenueLimit(meta, req)
			if splitErr != nil {
				orderRecord.Status = "ERROR"
				orderRecord.ErrorMessage = splitErr.Error()
				legResults[index] = primaryLegResult{
					records:  []entity.OrderRecord{orderRecord},
					errTexts: []string{orderRecord.ErrorMessage},
				}
				return
			}

			records := make([]entity.OrderRecord, 0, len(splitReqs))
			errTexts := make([]string, 0, len(splitReqs))
			exposureLegs := make([]exposedLeg, 0, len(splitReqs))
			stopOnAttention := phase == "open"
			for _, childReq := range splitReqs {
				orderRecord = buildOrderRecord(phase, phase, leg.role, leg.exchange, childReq.ReduceOnly, childReq)

				var (
					resp exchange.TradeOrderResult
					err  error
				)
				legCtx, cancel := s.primaryLegContext(ctx)
				if phase == "open" {
					resp, err = adapter.PlaceOrder(legCtx, childReq)
				} else {
					resp, err = adapter.ClosePosition(legCtx, childReq)
				}
				cancel()
				if err != nil {
					if s.isPrimaryLegUncertain(err) {
						// 子单超时、或者交易所明确告诉我们“执行结果未知”，都属于同一类高风险边界：
						// 本地现在不能确认这笔单到底有没有落到交易所。
						//
						// 这时直接当纯失败会非常危险，所以仍然要立刻走一次 reconcile。
						orderRecord.Status = "TIMEOUT"
						orderRecord.ErrorMessage = err.Error()
						orderRecord = s.reconcileUncertainPrimaryLeg(ctx, adapter, orderRecord, childReq)
						if phase == "open" && !isOrderFullySatisfied(orderRecord) {
							orderRecord = s.bestEffortCancelOpenOrder(ctx, adapter, orderRecord, childReq)
						}
						if isOrderFullySatisfied(orderRecord) {
							s.registerAPISuccess(leg.exchange)
						}
					} else {
						orderRecord.Status = "ERROR"
						orderRecord.ErrorMessage = err.Error()
					}
					if !isOrderFullySatisfied(orderRecord) {
						s.registerAPIFailure(leg.exchange)
					}
				} else {
					s.registerAPISuccess(leg.exchange)
					orderRecord.Status = pickNonEmpty(resp.Status, "SUBMITTED")
					orderRecord.VenueOrderID = resp.VenueOrderID
					orderRecord.ExecutedQty = resp.ExecutedQty
					orderRecord.AvgPrice = resp.AveragePrice
					orderRecord.RawResponse = resp.RawResponse
					orderRecord = s.reconcileOrder(ctx, adapter, orderRecord, childReq)
					if phase == "open" && !isOrderFullySatisfied(orderRecord) {
						orderRecord = s.bestEffortCancelOpenOrder(ctx, adapter, orderRecord, childReq)
					}
				}

				if phase == "open" && orderHasOpenExposure(orderRecord) {
					exposureReq := childReq
					if hasMeaningfulExecutedQty(orderRecord.ExecutedQty, childReq.Quantity) {
						exposureReq.Quantity = round8(math.Min(orderRecord.ExecutedQty, childReq.Quantity))
					}
					exposureLegs = append(exposureLegs, exposedLeg{role: leg.role, exchange: leg.exchange, req: exposureReq})
				}

				records = append(records, orderRecord)
				if !isOrderFullySatisfied(orderRecord) {
					errTexts = append(errTexts, pickNonEmpty(orderRecord.ErrorMessage, describeOrderAttention(orderRecord)))
					if stopOnAttention {
						break
					}
				} else if orderRecord.ErrorMessage != "" {
					errTexts = append(errTexts, orderRecord.ErrorMessage)
				}
			}
			legResults[index] = primaryLegResult{
				records:      records,
				errTexts:     errTexts,
				exposureLegs: exposureLegs,
			}
		}(idx, leg)
	}
	wg.Wait()

	results := make([]entity.OrderRecord, 0, 4)
	errors := make([]string, 0, 4)
	exposures := make([]exposedLeg, 0, 2)
	primaryNeedsRecovery := false
	for _, legResult := range legResults {
		legFullySatisfied := len(legResult.records) > 0
		for _, orderRecord := range legResult.records {
			if !isOrderFullySatisfied(orderRecord) {
				legFullySatisfied = false
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
		if !legFullySatisfied {
			primaryNeedsRecovery = true
		}
		errors = append(errors, legResult.errTexts...)
		exposures = append(exposures, legResult.exposureLegs...)
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
			meta, _ := s.store.Symbol(okLeg.exchange, plan.Symbol)
			if strings.TrimSpace(meta.Exchange) == "" {
				meta.Exchange = okLeg.exchange
			}
			if strings.TrimSpace(meta.VenueSymbol) == "" {
				meta.VenueSymbol = pickNonEmpty(okLeg.req.VenueSymbol, venueSymbolForLeg(plan, okLeg.role))
			}
			book, _ := s.store.LatestBookTop(okLeg.exchange, plan.Symbol)
			hedgeReq := s.buildRecoveryCloseRequest(
				plan,
				okLeg.role,
				reverseSide(okLeg.req.Side),
				meta,
				book,
				okLeg.req.Quantity,
				firstPositiveFloat(okLeg.req.Price, referencePriceForLeg(plan, okLeg.role)),
			)
			if adapter == nil || !adapter.Enabled() {
				rec := buildOrderRecord("hedge_close", "hedge_close", okLeg.role, okLeg.exchange, true, hedgeReq)
				rec.Status = "SKIPPED"
				rec.ErrorMessage = fmt.Sprintf("hedge trade adapter %s disabled", okLeg.exchange)
				errors = append(errors, rec.ErrorMessage)
				_ = s.orderRepo.Create(ctx, &rec)
				results = append(results, rec)
			} else {
				hedgeReqs, hedgeSplitErr := splitTradeRequestsByVenueLimit(meta, hedgeReq)
				if hedgeSplitErr != nil {
					rec := buildOrderRecord("hedge_close", "hedge_close", okLeg.role, okLeg.exchange, true, hedgeReq)
					rec.Status = "ERROR"
					rec.ErrorMessage = hedgeSplitErr.Error()
					errors = append(errors, fmt.Sprintf("hedge:%s:%s", okLeg.exchange, hedgeSplitErr.Error()))
					_ = s.orderRepo.Create(ctx, &rec)
					results = append(results, rec)
					continue
				}
				for _, hedgeChildReq := range hedgeReqs {
					hedgeResp := exchange.TradeOrderResult{}
					hedgeErr := error(nil)
					rec := buildOrderRecord("hedge_close", "hedge_close", okLeg.role, okLeg.exchange, true, hedgeChildReq)
					hedgeResp, hedgeErr = adapter.ClosePosition(ctx, hedgeChildReq)
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
						rec = s.reconcileOrder(ctx, adapter, rec, hedgeChildReq)
						if !isOrderFullySatisfied(rec) {
							errors = append(errors, describeOrderAttention(rec))
						}
					}
					_ = s.orderRepo.Create(ctx, &rec)
					results = append(results, rec)
				}
			}
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

func (s *ExecutionService) bestEffortCancelOpenOrder(ctx context.Context, adapter exchange.TradeAdapter, rec entity.OrderRecord, req exchange.TradeOrderRequest) entity.OrderRecord {
	if req.ReduceOnly || isOrderFullySatisfied(rec) || isFailedOrderStatus(rec.Status) {
		return rec
	}
	canceler, ok := adapter.(exchange.TradeOrderCanceler)
	if !ok || canceler == nil {
		return rec
	}
	lookup := exchange.OrderLookupRequest{
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		AssetID:         req.AssetID,
		ClientOrderID:   rec.ClientOrderID,
		VenueOrderID:    rec.VenueOrderID,
	}
	if strings.TrimSpace(lookup.ClientOrderID) == "" && strings.TrimSpace(lookup.VenueOrderID) == "" {
		return rec
	}
	if err := canceler.CancelOrder(ctx, lookup); err != nil {
		if s.logger != nil {
			s.logger.Warn("execution_open_order_cancel_failed",
				slog.String("exchange", rec.Exchange),
				slog.String("client_order_id", rec.ClientOrderID),
				slog.String("venue_order_id", rec.VenueOrderID),
				slog.Any("err", err),
			)
		}
		return rec
	}
	if s.logger != nil {
		s.logger.Info("execution_open_order_cancel_requested",
			slog.String("exchange", rec.Exchange),
			slog.String("client_order_id", rec.ClientOrderID),
			slog.String("venue_order_id", rec.VenueOrderID),
			slog.String("status_before_cancel", rec.Status),
		)
	}
	return s.reconcileOrder(ctx, adapter, rec, req)
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

func (s *ExecutionService) isPrimaryLegUncertain(err error) bool {
	if s.isPrimaryLegTimeout(err) {
		return true
	}
	var unknown *exchange.UnknownExecutionOutcomeError
	return errors.As(err, &unknown)
}

// reconcileUncertainPrimaryLeg 处理一种很常见但也很危险的边界：
// “请求方看来失败了，但交易所不一定真的没接到单。”
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
func (s *ExecutionService) reconcileUncertainPrimaryLeg(ctx context.Context, adapter exchange.TradeAdapter, rec entity.OrderRecord, req exchange.TradeOrderRequest) entity.OrderRecord {
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
	longMeta, _ := s.store.Symbol(plan.LongExchange, plan.Symbol)
	shortMeta, _ := s.store.Symbol(plan.ShortExchange, plan.Symbol)
	legs := []struct {
		exchange string
		venue    string
		assetID  string
		meta     entity.Symbol
		qty      float64
		price    float64
	}{
		{exchange: plan.LongExchange, venue: plan.LongVenueSymbol, assetID: longMeta.VenueAssetID, meta: longMeta, qty: plan.LongQty, price: plan.LongEntryPrice},
		{exchange: plan.ShortExchange, venue: plan.ShortVenueSymbol, assetID: shortMeta.VenueAssetID, meta: shortMeta, qty: plan.ShortQty, price: plan.ShortEntryPrice},
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
		requiredCapital := notional / math.Max(effectivePlanLeverage(s.cfg, plan, leg.meta), 1)
		if isSpotSymbol(leg.meta) {
			requiredCapital = notional
		}
		if !isSpotSymbol(leg.meta) && s.cfg.Execution.MinAccountEquityUSDT > 0 && acct.Equity > 0 && acct.Equity < s.cfg.Execution.MinAccountEquityUSDT {
			return fmt.Errorf("risk check: %s equity %.4f below minimum %.4f", leg.exchange, acct.Equity, s.cfg.Execution.MinAccountEquityUSDT)
		}
		if !isSpotSymbol(leg.meta) && acct.Equity > 0 && acct.AvailableBalance/acct.Equity < s.cfg.Execution.MinAvailableBalanceRatio {
			return fmt.Errorf("risk check: %s available ratio %.4f below minimum %.4f", leg.exchange, acct.AvailableBalance/acct.Equity, s.cfg.Execution.MinAvailableBalanceRatio)
		}
		if acct.AvailableBalance > 0 && acct.AvailableBalance < requiredCapital {
			return fmt.Errorf("risk check: %s available balance %.4f below required capital %.4f", leg.exchange, acct.AvailableBalance, requiredCapital)
		}
		pos, err := adapter.GetPosition(ctx, plan.Symbol, leg.venue, leg.assetID)
		if err == nil {
			if normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange)) == ArbitrageModeSameExchangeSpotPerp &&
				!isSpotSymbol(leg.meta) &&
				s.cfg.SameExchangeMinLiqDistanceRatio > 0 &&
				math.Abs(pos.Quantity) > 1e-9 &&
				pos.LiquidationPrice > 0 {
				if liqDistance := positionLiquidationDistanceRatio(pos); liqDistance > 0 && liqDistance < s.cfg.SameExchangeMinLiqDistanceRatio {
					return fmt.Errorf("risk check: %s liquidation distance %.4f below minimum %.4f", leg.exchange, liqDistance, s.cfg.SameExchangeMinLiqDistanceRatio)
				}
			}
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
	if orderType == "LIMIT" {
		price = roundPriceToTick(price, meta.TickSize, side)
	}
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
		ClientOrderID:   buildClientOrderIDForExchange(meta.Exchange, plan, phase, legRole),
		Reason:          phase,
	}
}

func (s *ExecutionService) buildRecoveryCloseRequest(plan *entity.ExecutionPlan, legRole, side string, meta entity.Symbol, book entity.BookTopSnapshot, qty, refPrice float64) exchange.TradeOrderRequest {
	if strings.TrimSpace(meta.Exchange) == "" {
		meta.Exchange = exchangeForLeg(plan, legRole)
	}
	if strings.TrimSpace(meta.VenueSymbol) == "" {
		meta.VenueSymbol = venueSymbolForLeg(plan, legRole)
	}
	qty = roundDownStep(qty, meta.StepSize)
	price := roundPriceToTick(aggressivePrice(side, book, refPrice), meta.TickSize, side)
	return exchange.TradeOrderRequest{
		CanonicalSymbol: plan.Symbol,
		VenueSymbol:     meta.VenueSymbol,
		AssetID:         meta.VenueAssetID,
		Side:            side,
		OrderType:       "LIMIT",
		TimeInForce:     "IOC",
		Quantity:        round8(qty),
		Price:           round8(price),
		ReduceOnly:      true,
		ClientOrderID:   buildClientOrderIDForExchange(meta.Exchange, plan, "hedge", legRole),
		Reason:          "open_leg_failed_hedge",
	}
}

func splitClientOrderID(base string, part int) string {
	// 拆单后的子单 id 必须继续满足交易所长度限制。
	// 因此不能直接在原字符串末尾无脑拼接 `-pN`，而是要先为后缀留足空间。
	base = clampClientOrderID(base, maxClientOrderIDLen)
	if base == "" || part <= 1 {
		return base
	}
	return appendClientOrderIDSuffix(base, fmt.Sprintf("-p%d", part))
}

func splitClientOrderIDForExchange(exchangeName, base string, part int) string {
	if strings.EqualFold(strings.TrimSpace(exchangeName), "hyperliquid") {
		return splitHyperliquidClientOrderID(base, part)
	}
	return splitClientOrderID(base, part)
}

// splitTradeRequestsByVenueLimit 会在交易所声明了单笔 maxQty 时，自动把一笔大单拆成多笔子单。
//
// 设计取舍：
// 1. 不简单把数量截断到 maxQty，因为那会留下“只开出一半”的单腿风险；
// 2. 也不在 adapter 内部偷偷聚合，因为执行层需要把每一笔子单都落成 OrderRecord；
// 3. 因此拆单发生在执行层，请求仍按原来的风控/落库/回补路径逐笔处理。
func splitTradeRequestsByVenueLimit(meta entity.Symbol, req exchange.TradeOrderRequest) ([]exchange.TradeOrderRequest, error) {
	maxQty := meta.ExecutionMeta().MaxQtyForOrderType(req.OrderType)
	if maxQty <= 0 || req.Quantity <= maxQty+1e-9 {
		return []exchange.TradeOrderRequest{req}, nil
	}

	minQty := parseFloat(meta.MinQty)
	remaining := roundDownStep(req.Quantity, meta.StepSize)
	if remaining <= 0 {
		return nil, fmt.Errorf("invalid order quantity %.8f after step rounding", req.Quantity)
	}

	out := make([]exchange.TradeOrderRequest, 0, int(math.Ceil(remaining/math.Max(maxQty, 1))))
	for part := 1; remaining > 1e-9; part++ {
		chunkQty := math.Min(remaining, maxQty)
		chunkQty = roundDownStep(chunkQty, meta.StepSize)
		if chunkQty <= 0 {
			return nil, fmt.Errorf("unable to split quantity %.8f with max_qty %.8f and step_size %s", req.Quantity, maxQty, meta.StepSize)
		}
		nextRemaining := roundDownStep(remaining-chunkQty, meta.StepSize)
		if nextRemaining > 1e-9 && minQty > 0 && nextRemaining < minQty {
			mergedQty := roundDownStep(remaining, meta.StepSize)
			if mergedQty > 0 && mergedQty <= maxQty+1e-9 {
				chunkQty = mergedQty
				nextRemaining = 0
			} else {
				return nil, fmt.Errorf("unable to split quantity %.8f into valid chunks under max_qty %.8f and min_qty %.8f", req.Quantity, maxQty, minQty)
			}
		}

		chunkReq := req
		chunkReq.Quantity = round8(chunkQty)
		chunkReq.ClientOrderID = splitClientOrderIDForExchange(meta.Exchange, req.ClientOrderID, part)
		out = append(out, chunkReq)
		remaining = nextRemaining
	}
	return out, nil
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

func exchangeForLeg(plan *entity.ExecutionPlan, legRole string) string {
	if strings.EqualFold(legRole, "short_leg") {
		return plan.ShortExchange
	}
	return plan.LongExchange
}

func venueSymbolForLeg(plan *entity.ExecutionPlan, legRole string) string {
	if strings.EqualFold(legRole, "short_leg") {
		return plan.ShortVenueSymbol
	}
	return plan.LongVenueSymbol
}

func referencePriceForLeg(plan *entity.ExecutionPlan, legRole string) float64 {
	if strings.EqualFold(legRole, "short_leg") {
		return plan.ShortEntryPrice
	}
	return plan.LongEntryPrice
}

func buildClientOrderID(plan *entity.ExecutionPlan, phase, legRole string) string {
	// buildClientOrderID 既要保留最基本的可读性，又必须满足 Binance-like venue
	// 对 `newClientOrderId` 的长度限制，尤其是 Aster 的 36 字符上限。
	//
	// 旧实现的问题有两个：
	// 1. `open/close/hedge + long_leg/short_leg + planKey` 组合后，基础 id 常常已经被截到 36；
	// 2. 拆单再追加 `-p2/-p3` 时会进一步越界，最终触发 Aster -4015。
	//
	// 新实现采用“短标签 + 时间 nonce + 短哈希”的结构：
	// - symbol/phase/leg 仍然保留少量可读信息，方便排查；
	// - 哈希承载大部分唯一性，避免继续把 planKey 整段塞进来；
	// - 基础 id 主动控制在 30 字符以内，为拆单后缀预留空间。
	now := time.Now()
	symbolTag := sanitizeClientOrderIDToken(plan.Symbol, 8)
	if symbolTag == "" {
		symbolTag = "ord"
	}
	phaseTag := abbreviateClientOrderPhase(phase)
	legTag := abbreviateClientOrderLeg(legRole)
	nonceTag := strconv.FormatInt(now.UnixMilli()%2_176_782_336, 36)
	hashTag := shortClientOrderHash(fmt.Sprintf("%s|%s|%s|%s|%d", plan.PlanKey, plan.Symbol, phase, legRole, now.UnixNano()))
	raw := fmt.Sprintf("%s-%s-%s-%s-%s", symbolTag, phaseTag, legTag, nonceTag, hashTag)
	return clampClientOrderID(raw, maxClientOrderIDBaseLen)
}

func buildClientOrderIDForExchange(exchangeName string, plan *entity.ExecutionPlan, phase, legRole string) string {
	// 不同交易所对 client order id 的约束差异很大：
	// - Binance / Aster / Bybit 基本都接受 36 字符以内的人类可读 token；
	// - Hyperliquid 则要求 `cloid` 是 16-byte hex，并带 `0x` 前缀。
	//
	// 因此这里不能继续用“一套字符串打天下”的做法，而是按交易所协议族生成。
	if strings.EqualFold(strings.TrimSpace(exchangeName), "hyperliquid") {
		return buildHyperliquidClientOrderID(plan, phase, legRole)
	}
	return buildClientOrderID(plan, phase, legRole)
}

func appendClientOrderIDSuffix(base, suffix string) string {
	base = strings.TrimSpace(base)
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return clampClientOrderID(base, maxClientOrderIDLen)
	}
	if len(suffix) >= maxClientOrderIDLen {
		return suffix[len(suffix)-maxClientOrderIDLen:]
	}
	maxBaseLen := maxClientOrderIDLen - len(suffix)
	if len(base) > maxBaseLen {
		base = base[:maxBaseLen]
	}
	return base + suffix
}

func buildHyperliquidClientOrderID(plan *entity.ExecutionPlan, phase, legRole string) string {
	// Hyperliquid 官方文档要求 `cloid` 为 128-bit hex string，并带 `0x` 前缀。
	//
	// 这里用 sha256 的前 16 个字节构造稳定长度的 hex id：
	// 1. 满足交易所格式约束；
	// 2. 避免把 Binance-like 的短字符串格式误发到 Hyperliquid；
	// 3. 即使后续继续拆单，也可以基于这个 hex cloid 再派生出新的合法 cloid。
	now := time.Now()
	return hyperliquidClientOrderIDFromSeed(
		fmt.Sprintf("%s|%s|%s|%s|%d", plan.PlanKey, plan.Symbol, phase, legRole, now.UnixNano()),
	)
}

func splitHyperliquidClientOrderID(base string, part int) string {
	base = strings.TrimSpace(base)
	if base == "" || part <= 1 {
		return base
	}
	// Hyperliquid 的 cloid 不能像 Binance-like 一样直接拼 `-pN`。
	// 因此拆单时改成“基于原 cloid + part 再派生一个新的 16-byte hex cloid”。
	return hyperliquidClientOrderIDFromSeed(fmt.Sprintf("%s|part|%d", base, part))
}

func hyperliquidClientOrderIDFromSeed(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return "0x" + hex.EncodeToString(digest[:16])
}

func clampClientOrderID(raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if maxLen <= 0 || len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen]
}

func sanitizeClientOrderIDToken(raw string, maxLen int) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || maxLen <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(minInt(len(raw), maxLen))
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() >= maxLen {
				break
			}
		}
	}
	return b.String()
}

func abbreviateClientOrderPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "open":
		return "op"
	case "close":
		return "cl"
	case "hedge":
		return "hg"
	default:
		token := sanitizeClientOrderIDToken(phase, 2)
		if token == "" {
			return "na"
		}
		return token
	}
}

func abbreviateClientOrderLeg(legRole string) string {
	role := strings.ToLower(strings.TrimSpace(legRole))
	switch {
	case strings.Contains(role, "short"):
		return "s"
	case strings.Contains(role, "long"):
		return "l"
	default:
		token := sanitizeClientOrderIDToken(role, 1)
		if token == "" {
			return "x"
		}
		return token
	}
}

func shortClientOrderHash(raw string) string {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(raw))
	encoded := strings.ToLower(strconv.FormatUint(hasher.Sum64(), 36))
	if len(encoded) > 8 {
		return encoded[:8]
	}
	return encoded
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
