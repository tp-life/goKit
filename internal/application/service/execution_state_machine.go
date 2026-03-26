package service

import (
	"fmt"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

// executionPhasePolicy 把“某个执行阶段应该如何推进 execution status”集中到一个地方。
//
// 这里故意不用零散的字符串拼接或 scattered if/switch，而是把开仓、平仓两种 phase
// 的状态推进规则明确写成一个小型 policy：
// 1. 进入该 phase 前，record 应该切到哪个 pending 状态；
// 2. dry-run 时应该落哪个终态；
// 3. 根据 order results 汇总后，该 phase 的成功/失败/对冲中间态各是什么。
//
// 这样做的目的不是“已经做成完整状态机”，而是先把最核心的状态推进入口收束起来。
// 后续如果要接 websocket 事件、恢复逻辑、显式 transition graph，就可以继续沿着这层扩展，
// 而不必再回到 openPlan/closePlan 里找散落的字符串常量。
type executionPhasePolicy struct {
	phase         string
	pendingStatus string
	dryRunStatus  string
	successStatus string
	partialStatus string
	failedStatus  string
	hedgingStatus string
	startableFrom map[string]struct{}
}

// executionTransitionGraph 用来描述“某个 execution phase 允许发生哪些状态迁移”。
//
// 和 startableFrom 相比，这里关注的是更细的一层：
// 1. startableFrom 只回答“这个动作能不能开始”；
// 2. transition graph 回答“动作一旦开始，record 具体可以从哪个状态走到哪个状态”。
//
// 例如 open 动作里：
// - `open_failed -> pending_open` 是允许的重试；
// - `pending_open -> opened` 是正常成功；
// - `pending_open -> open_hedging` 是部分失败后的补救中间态；
// - 但 `opened -> pending_open` 不应再被允许。
//
// 把这些边显式写出来后，后续如果再接 websocket、恢复逻辑、人工干预入口，
// 都可以复用同一份 transition graph，而不是各自再发明一套“隐式约定”。
type executionTransitionGraph struct {
	phase string
	edges map[string]map[string]struct{}
}

// executionEvent 是 execution 状态机面向“未来事件驱动”演进的最小语义单元。
//
// 当前系统虽然还没有把 websocket 用户流真正接进来，但我们已经先把内部状态更新
// 改成“通过事件推进”而不是“直接改 Status”：
// 1. event.Name 记录这次迁移在业务语义上是什么，例如 `open_requested`；
// 2. event.TargetStatus 记录这次事件希望把 record 推到哪个 execution status；
// 3. event.Reason 记录对人类友好的上下文说明，方便排障和后续 UI 展示；
// 4. event.OccurredAtMs 记录事件时间，后续若接入 websocket / replay，也能复用同一字段。
//
// 这层设计的核心价值是：
// - 让“事件源”和“状态机”解耦；
// - 先把本地同步流程也写成事件推进；
// - 为之后接私有订单流留下自然扩展点。
type executionEvent struct {
	Name         string
	TargetStatus string
	Reason       string
	OccurredAtMs int64
}

var (
	openExecutionPolicy = executionPhasePolicy{
		phase:         "open",
		pendingStatus: executionStatePendingOpen,
		dryRunStatus:  "dry_run_opened",
		successStatus: executionStateOpened,
		partialStatus: executionStateOpenPartial,
		failedStatus:  executionStateOpenFailed,
		hedgingStatus: executionStateOpenHedging,
		startableFrom: statusSet(
			"",
			executionStateOpenFailed,
			executionStateRiskBlocked,
			executionStateCircuitOpen,
		),
	}
	closeExecutionPolicy = executionPhasePolicy{
		phase:         "close",
		pendingStatus: executionStatePendingClose,
		dryRunStatus:  "dry_run_closed",
		successStatus: executionStateClosed,
		partialStatus: executionStateClosePartial,
		failedStatus:  executionStateCloseFailed,
		hedgingStatus: executionStateCloseHedging,
		startableFrom: statusSet(
			executionStateOpened,
			executionStateOpenPartial,
			executionStateOpenHedging,
			"dry_run_opened",
			executionStateCloseFailed,
			executionStateClosePartial,
			executionStateCloseHedging,
		),
	}
)

var (
	openExecutionTransitions = newExecutionTransitionGraph(
		openExecutionPolicy.phase,
		transitionRule("", openExecutionPolicy.pendingStatus, openExecutionPolicy.dryRunStatus, executionStateRiskBlocked, executionStateCircuitOpen),
		transitionRule(executionStateOpenFailed, openExecutionPolicy.pendingStatus, openExecutionPolicy.dryRunStatus, executionStateRiskBlocked, executionStateCircuitOpen),
		transitionRule(executionStateRiskBlocked, openExecutionPolicy.pendingStatus, openExecutionPolicy.dryRunStatus, executionStateRiskBlocked, executionStateCircuitOpen),
		transitionRule(executionStateCircuitOpen, openExecutionPolicy.pendingStatus, openExecutionPolicy.dryRunStatus, executionStateRiskBlocked, executionStateCircuitOpen),
		transitionRule(openExecutionPolicy.pendingStatus, openExecutionPolicy.successStatus, openExecutionPolicy.partialStatus, openExecutionPolicy.failedStatus, openExecutionPolicy.hedgingStatus),
		transitionRule(openExecutionPolicy.partialStatus, openExecutionPolicy.successStatus, openExecutionPolicy.partialStatus, openExecutionPolicy.failedStatus, openExecutionPolicy.hedgingStatus),
		transitionRule(openExecutionPolicy.failedStatus, openExecutionPolicy.successStatus, openExecutionPolicy.partialStatus, openExecutionPolicy.failedStatus, openExecutionPolicy.hedgingStatus),
		transitionRule(openExecutionPolicy.hedgingStatus, openExecutionPolicy.hedgingStatus, openExecutionPolicy.failedStatus),
	)
	closeExecutionTransitions = newExecutionTransitionGraph(
		closeExecutionPolicy.phase,
		transitionRule(executionStateOpened, closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule(executionStateOpenPartial, closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule(executionStateOpenHedging, closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule("dry_run_opened", closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule(closeExecutionPolicy.failedStatus, closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule(closeExecutionPolicy.partialStatus, closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule(closeExecutionPolicy.hedgingStatus, closeExecutionPolicy.pendingStatus, closeExecutionPolicy.dryRunStatus),
		transitionRule(closeExecutionPolicy.pendingStatus, closeExecutionPolicy.successStatus, closeExecutionPolicy.partialStatus, closeExecutionPolicy.failedStatus, closeExecutionPolicy.hedgingStatus),
		transitionRule(closeExecutionPolicy.partialStatus, closeExecutionPolicy.successStatus, closeExecutionPolicy.partialStatus, closeExecutionPolicy.failedStatus, closeExecutionPolicy.hedgingStatus),
		transitionRule(closeExecutionPolicy.failedStatus, closeExecutionPolicy.successStatus, closeExecutionPolicy.partialStatus, closeExecutionPolicy.failedStatus, closeExecutionPolicy.hedgingStatus),
		transitionRule(closeExecutionPolicy.hedgingStatus, closeExecutionPolicy.hedgingStatus, closeExecutionPolicy.failedStatus),
	)
	externalFlatCloseTransitions = newExecutionTransitionGraph(
		"external_close_reconcile",
		transitionRule(executionStateOpened, closeExecutionPolicy.successStatus),
		transitionRule(executionStateOpenPartial, closeExecutionPolicy.successStatus),
		transitionRule(executionStateOpenHedging, closeExecutionPolicy.successStatus),
		transitionRule(executionStatePendingClose, closeExecutionPolicy.successStatus),
		transitionRule(closeExecutionPolicy.partialStatus, closeExecutionPolicy.successStatus),
		transitionRule(closeExecutionPolicy.failedStatus, closeExecutionPolicy.successStatus),
		transitionRule(closeExecutionPolicy.hedgingStatus, closeExecutionPolicy.successStatus),
	)
)

// statusSet 只是一个小工具，用来让 policy 初始化更易读。
//
// 相比在多个地方手写 map literal，这种写法更接近“把一组允许状态列出来”，
// 读代码时能更快看出状态机边界。
func statusSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[normalizeExecutionStatus(value)] = struct{}{}
	}
	return out
}

// executionTransitionRule 只是 transition graph 初始化时的轻量描述结构。
//
// 使用它的原因是，代码层更适合写成：
// `transitionRule(from, to1, to2, ...)`
// 而不是在 graph 初始化处直接堆多层 map literal。
type executionTransitionRule struct {
	from string
	to   []string
}

func transitionRule(from string, to ...string) executionTransitionRule {
	return executionTransitionRule{from: from, to: to}
}

func newExecutionTransitionGraph(phase string, rules ...executionTransitionRule) executionTransitionGraph {
	graph := executionTransitionGraph{
		phase: phase,
		edges: make(map[string]map[string]struct{}, len(rules)),
	}
	for _, rule := range rules {
		from := normalizeExecutionStatus(rule.from)
		if _, ok := graph.edges[from]; !ok {
			graph.edges[from] = make(map[string]struct{}, len(rule.to))
		}
		for _, candidate := range rule.to {
			graph.edges[from][normalizeExecutionStatus(candidate)] = struct{}{}
		}
	}
	return graph
}

// normalizeExecutionStatus 统一 execution status 的读法。
//
// 仓库里很多地方都只关心“逻辑上的状态值”，不希望被大小写、首尾空格影响。
// 这层小 helper 看起来简单，但它能避免很多“状态明明一样、比较却失败”的隐性问题。
func normalizeExecutionStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

// normalizeExecutionEventName 统一 event 名称的读写形式。
//
// 和 status 一样，事件名也会进入数据库与测试断言。
// 统一做小写 + trim，可以减少未来接入不同来源事件时的比较歧义。
func normalizeExecutionEventName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// shouldShortCircuitExecutionAction 用来回答：
// “如果当前 record 已经处在某个终态，当前动作是否应该直接返回，不再重复推进？”
//
// 例如：
// - 对 open 来说，已经 opened / dry_run_opened / closed / dry_run_closed 就不应再次 open；
// - 对 close 来说，已经 closed / dry_run_closed 就不应再次重复 close。
//
// 这一步是当前 execution 幂等性的核心保护之一。
func shouldShortCircuitExecutionAction(status, phase string) bool {
	status = normalizeExecutionStatus(status)
	phase = normalizeExecutionStatus(phase)

	switch phase {
	case "open":
		switch status {
		case executionStateOpened, "dry_run_opened", executionStateClosed, "dry_run_closed":
			return true
		default:
			return false
		}
	case "close":
		switch status {
		case executionStateClosed, "dry_run_closed":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func isExecutionActionPending(status, phase string) bool {
	status = normalizeExecutionStatus(status)
	phase = normalizeExecutionStatus(phase)

	switch phase {
	case openExecutionPolicy.phase:
		return status == executionStatePendingOpen
	case closeExecutionPolicy.phase:
		return status == executionStatePendingClose
	default:
		return false
	}
}

// canStartExecutionAction 明确回答：
// “当前 execution status 是否允许进入某个新动作（open / close）？”
//
// 这和 short-circuit 是两个不同层次的判断：
// 1. short-circuit 解决的是“已经在终态，不需要重复做”；
// 2. canStartExecutionAction 解决的是“即使还没终态，但从这个状态继续做该动作是否安全”。
//
// 例如：
// - `open_partial_failed` 不应该再次直接 open，因为系统可能残留单腿仓位；
// - `pending_open` 也不应该盲目重复 open，因为原请求可能仍在途中；
// - `close` 则应只从明确已打开或可重试的 close 异常态进入。
func canStartExecutionAction(status string, policy executionPhasePolicy) bool {
	status = normalizeExecutionStatus(status)
	_, ok := policy.startableFrom[status]
	return ok
}

// validateExecutionAction 把“当前 record 是否允许执行这个动作”的判定收敛成统一入口。
//
// 当前返回 error 而不是 bool，原因是执行层属于高风险路径：
// 当动作被拒绝时，调用方需要一条对排障友好的原因，而不是只有 true/false。
func validateExecutionAction(recordExists bool, status string, policy executionPhasePolicy) error {
	status = normalizeExecutionStatus(status)

	if shouldShortCircuitExecutionAction(status, policy.phase) {
		return nil
	}
	if !recordExists && policy.phase == openExecutionPolicy.phase {
		return nil
	}
	if !recordExists {
		return fmt.Errorf("execution %s requires existing execution record", policy.phase)
	}
	if canStartExecutionAction(status, policy) {
		return nil
	}
	if status == "" {
		return fmt.Errorf("execution %s requires explicit source status", policy.phase)
	}
	return fmt.Errorf("execution %s not allowed from status %s", policy.phase, status)
}

// canTransitionExecutionStatus 判断一条具体状态边是否存在于 transition graph 中。
//
// 这层 helper 保持只做一件事：纯粹回答“from -> to 是否被允许”。
// 真正把错误包装成可读消息，则放在 validateExecutionTransition 里。
func canTransitionExecutionStatus(from, to string, graph executionTransitionGraph) bool {
	from = normalizeExecutionStatus(from)
	to = normalizeExecutionStatus(to)
	candidates, ok := graph.edges[from]
	if !ok {
		return false
	}
	_, ok = candidates[to]
	return ok
}

// validateExecutionTransition 用于在真正写 execution record 之前，检查状态迁移是否合法。
//
// 这一步的价值在于：
// 1. 即使调用方未来变多，所有 status 写入仍会走同一套边检查；
// 2. 当出现非法迁移时，错误信息会明确指出 from / to / phase；
// 3. 状态机规则可以被测试直接覆盖，而不是只能通过高层集成测试间接验证。
func validateExecutionTransition(from, to string, graph executionTransitionGraph) error {
	if canTransitionExecutionStatus(from, to, graph) {
		return nil
	}
	return fmt.Errorf("execution %s transition not allowed: %s -> %s", graph.phase, normalizeExecutionStatus(from), normalizeExecutionStatus(to))
}

// applyExecutionTransition 是 execution record 状态写入的统一入口。
//
// 这里刻意不让 openPlan/closePlan 直接做 `rec.Status = xxx`，而是统一走这层：
// 1. 先按 graph 校验这条状态边是否合法；
// 2. 合法才真正写入 record。
//
// 这样代码层面就能保证：状态机规则不是“写在文档里”，而是“写进了运行时入口”。
func applyExecutionTransition(rec *entity.ExecutionRecord, to string, graph executionTransitionGraph) error {
	if rec == nil {
		return fmt.Errorf("nil execution record")
	}
	if err := validateExecutionTransition(rec.Status, to, graph); err != nil {
		return err
	}
	rec.Status = normalizeExecutionStatus(to)
	return nil
}

// applyExecutionEvent 是 execution record 状态推进的更高层入口。
//
// 它在 `applyExecutionTransition` 基础上再做三件事：
// 1. 把迁移动作和 event 语义绑定起来；
// 2. 记录最近一次 transition event 与发生时间；
// 3. 保存对人类可读的 status reason，方便排障与未来 UI 展示。
//
// 当前 open/close 流程虽然还是同步触发这些 event，
// 但只要后续 websocket 用户流进来，完全可以复用同一个入口推进 record。
func applyExecutionEvent(rec *entity.ExecutionRecord, event executionEvent, graph executionTransitionGraph) error {
	if rec == nil {
		return fmt.Errorf("nil execution record")
	}
	if err := applyExecutionTransition(rec, event.TargetStatus, graph); err != nil {
		return err
	}
	rec.LastTransitionEvent = normalizeExecutionEventName(event.Name)
	rec.StatusReason = strings.TrimSpace(event.Reason)
	if event.OccurredAtMs > 0 {
		rec.LastTransitionAtMs = event.OccurredAtMs
	} else {
		rec.LastTransitionAtMs = time.Now().UnixMilli()
	}
	return nil
}

// pickExecutionStatus 是一个很小的 nil-safe helper。
//
// openPlan / closePlan 在读取 execution record 后，经常需要：
// 1. 如果 record 存在，则读取它的 status；
// 2. 如果 record 不存在，则把状态视为空字符串。
//
// 这个 helper 让调用处不用重复写 nil 判断，减少状态机入口周围的噪音。
func pickExecutionStatus(rec *entity.ExecutionRecord) string {
	if rec == nil {
		return ""
	}
	return rec.Status
}

// requiresRecoveryClose 用来标识“虽然还没进入正常 opened，但已经值得立刻尝试 reduce-only close”的状态。
//
// 当前这类状态主要是 open 阶段的异常尾部：
// - `open_partial_failed`
// - `open_hedging`
//
// 它们的共同点是：
// 1. 系统已经尝试开仓；
// 2. 至少存在残留单腿仓位的可能；
// 3. 与其把它们长期挂着，不如尽快进入一轮保守的 recovery close。
func requiresRecoveryClose(status string) bool {
	switch normalizeExecutionStatus(status) {
	case executionStateOpenPartial, executionStateOpenHedging:
		return true
	default:
		return false
	}
}

// requiresRetryClose 用来标识“close 已经尝试过，但仍然残留 live 风险”的状态。
//
// 这类状态和 open 阶段的 recovery close 风险不同，但本质上也不该静置：
// - `close_failed`
// - `close_partial_failed`
// - `close_hedging`
//
// 它们说明系统已经判断“应该退出”，只是上一轮 close 没有完全成功。
// 对这类 live record，后续自动平仓扫描应允许继续重试 close。
func requiresRetryClose(status string) bool {
	switch normalizeExecutionStatus(status) {
	case executionStateCloseFailed, executionStateClosePartial, executionStateCloseHedging:
		return true
	default:
		return false
	}
}

// shouldRecordOpenedAt / shouldRecordClosedAt 用来约束 record 上时间戳的写法。
//
// 这两个 helper 的作用是把“某个 phase 完成后应该记录哪个时间”写清楚，
// 避免以后在 open/close 不同分支里重复写条件。
func shouldRecordOpenedAt(phase string) bool {
	return normalizeExecutionStatus(phase) == "open"
}

func shouldRecordClosedAt(phase string) bool {
	return normalizeExecutionStatus(phase) == "close"
}

// summarizeExecutionStatusWithPolicy 根据某个 phase policy 汇总 order 级结果，
// 产出 plan 级 execution status。
//
// 汇总规则保持与现有行为一致，但这里把语义写得更明确：
// 1. 只统计 primary legs 的成功/失败；
// 2. hedge_* phase 单独标识为 hedging，而不是混到 primary success 里；
// 3. open/close 各自返回属于自己的状态集合。
func summarizeExecutionStatusWithPolicy(results []entity.OrderRecord, policy executionPhasePolicy) string {
	if len(results) == 0 {
		return policy.failedStatus
	}

	totalPrimary := 0
	successPrimary := 0
	hasHedge := false
	hasErr := false

	for _, item := range results {
		if strings.HasPrefix(normalizeExecutionStatus(item.Phase), "hedge") {
			hasHedge = true
			if item.ErrorMessage != "" || isFailedOrderStatus(item.Status) {
				hasErr = true
			}
			continue
		}

		totalPrimary++
		if item.ErrorMessage != "" || isFailedOrderStatus(item.Status) {
			hasErr = true
			continue
		}
		if isOrderFullySatisfied(item) {
			successPrimary++
			continue
		}
		hasErr = true
	}

	switch {
	case hasHedge:
		return policy.hedgingStatus
	case successPrimary == totalPrimary && !hasErr:
		return policy.successStatus
	case successPrimary > 0:
		return policy.partialStatus
	default:
		return policy.failedStatus
	}
}

// executionErrorStatus 把某些可识别的错误类型映射成 execution status。
//
// 当前我们至少区分两类：
// 1. `api_circuit_open`：代表 venue 熔断拦截；
// 2. 其他风控错误：统一落到 `risk_blocked`。
//
// 这样 execution record 至少能表达“被风控拦了”和“被熔断拦了”的区别，
// 后续前端或运维面板也更容易据此做分类展示。
func executionErrorStatus(err error, fallback string) string {
	if err == nil {
		return normalizeExecutionStatus(fallback)
	}
	message := normalizeExecutionStatus(err.Error())
	if strings.HasPrefix(message, executionStateCircuitOpen) {
		return executionStateCircuitOpen
	}
	return normalizeExecutionStatus(fallback)
}

// executionErrorEvent 把错误场景映射到更细的 execution event 名称。
//
// 当前先区分：
// - 风控阻断：`*_risk_blocked`
// - 熔断阻断：`*_circuit_blocked`
//
// 这样数据库里就不只有 status，还能看到“这次是因为什么事件变成这个 status”。
func executionErrorEvent(phase string, err error) string {
	phase = normalizeExecutionStatus(phase)
	if strings.HasPrefix(normalizeExecutionStatus(pickErrorMessage(err)), executionStateCircuitOpen) {
		return phase + "_circuit_blocked"
	}
	return phase + "_risk_blocked"
}

func pickErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
