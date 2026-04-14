package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

var (
	// ErrExternalOrderEventInvalid 表示“外部订单事件本身不完整”，
	// 例如缺少 exchange 或者完全没有订单标识。
	//
	// 把这类错误显式做成 sentinel 的好处是：
	// HTTP / CLI / 回放工具等不同入口都可以稳定地区分
	// “请求体不合法” 和 “系统内部处理失败”。
	ErrExternalOrderEventInvalid = errors.New("external order event invalid")

	// ErrExternalOrderTargetNotFound 表示“事件格式合法，但系统里找不到它要更新的目标”。
	//
	// 这通常意味着：
	// 1. 本地还没落下对应 OrderRecord；
	// 2. 或者已落单，但 execution record 尚未建立；
	// 3. 或者调用方带错了 client_order_id / venue_order_id。
	//
	// 上层可以用 errors.Is(err, ErrExternalOrderTargetNotFound) 统一映射成 404，
	// 而不再依赖脆弱的字符串 contains 判断。
	ErrExternalOrderTargetNotFound = errors.New("external order target not found")
)

// ExternalOrderEvent 描述“从执行服务外部进入系统的一条订单事件”。
//
// 这里的“外部”主要指未来会接入的：
// - 用户流 websocket
// - 交易所私有订单回报
// - 恢复阶段的事件回放
// - 甚至人工补录/调试注入
//
// 它和交易所 adapter 返回的 TradeOrderResult 不同：
// 1. TradeOrderResult 更像“本次同步请求的即时响应”；
// 2. ExternalOrderEvent 更像“某笔订单后来又发生了一个新事实”。
//
// 因此这里故意把 source、occurredAt、terminal/canceled 这些事件语义也带进来，
// 为后续真正接 websocket 留出稳定接口。
type ExternalOrderEvent struct {
	Source        string  `json:"source"`
	Exchange      string  `json:"exchange"`
	ClientOrderID string  `json:"client_order_id"`
	VenueOrderID  string  `json:"venue_order_id"`
	Status        string  `json:"status"`
	ExecutedQty   float64 `json:"executed_qty"`
	AveragePrice  float64 `json:"average_price"`
	Terminal      bool    `json:"terminal"`
	Canceled      bool    `json:"canceled"`
	ErrorMessage  string  `json:"error_message"`
	RawPayload    string  `json:"raw_payload"`
	OccurredAtMs  int64   `json:"occurred_at_ms"`
}

// normalizeExternalOrderEvent 把外部事件统一成系统内部更稳定的格式。
//
// 当前主要做三件事：
// 1. trim/大小写归一；
// 2. 当调用方没带 source 时，给一个默认值 `external`；
// 3. 当调用方只带了 canceled/terminal 语义但没带 status 时，补一个保守 status。
func normalizeExternalOrderEvent(event ExternalOrderEvent) ExternalOrderEvent {
	event.Source = normalizeExecutionEventName(pickNonEmpty(event.Source, "external"))
	event.Exchange = normalizeVenueName(event.Exchange)
	event.ClientOrderID = strings.TrimSpace(event.ClientOrderID)
	event.VenueOrderID = strings.TrimSpace(event.VenueOrderID)
	event.ErrorMessage = strings.TrimSpace(event.ErrorMessage)
	event.RawPayload = strings.TrimSpace(event.RawPayload)
	event.Status = normalizeExternalOrderStatus(event.Status, event.Canceled)
	if event.OccurredAtMs <= 0 {
		event.OccurredAtMs = time.Now().UnixMilli()
	}
	return event
}

func normalizeExternalOrderStatus(status string, canceled bool) string {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "" {
		return status
	}
	if canceled {
		return "CANCELED"
	}
	return ""
}

// ApplyExternalOrderEvent 是未来 websocket / 用户流接入时的统一入口。
//
// 当前它负责两层事情：
// 1. 把外部事件合并到对应的 OrderRecord；
// 2. 如果这条订单属于某个正在推进中的 execution，再尝试把 execution 状态机往前推一步。
//
// 这里的设计重点不是“一次性把完整 websocket 流做完”，而是先把输入口稳定下来。
// 这样后面真正接私有流时，只需要负责“把原始 payload 翻译成 ExternalOrderEvent”，
// 而不必在 websocket handler 里直接改 execution/order 数据模型。
func (s *ExecutionService) ApplyExternalOrderEvent(ctx context.Context, event ExternalOrderEvent) (*entity.ExecutionRecord, error) {
	event = normalizeExternalOrderEvent(event)
	if event.Exchange == "" {
		return nil, fmt.Errorf("%w: missing exchange", ErrExternalOrderEventInvalid)
	}
	if event.ClientOrderID == "" && event.VenueOrderID == "" {
		return nil, fmt.Errorf("%w: missing order identifier", ErrExternalOrderEventInvalid)
	}

	order, err := s.orderRepo.FindByExternalOrderID(ctx, event.Exchange, event.ClientOrderID, event.VenueOrderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, fmt.Errorf("%w: order record exchange=%s client_order_id=%s venue_order_id=%s",
			ErrExternalOrderTargetNotFound,
			event.Exchange,
			event.ClientOrderID,
			event.VenueOrderID,
		)
	}

	mergeOrderRecordFromExternalEvent(order, event)
	if err := s.orderRepo.Update(ctx, order); err != nil {
		return nil, err
	}

	rec, err := s.execRepo.FindByPlanKey(ctx, order.PlanKey)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("%w: execution record plan_key=%s", ErrExternalOrderTargetNotFound, order.PlanKey)
	}
	if normalizeExecutionStatus(order.Phase) == sameExchangeProtectPhase {
		return rec, nil
	}

	return s.applyExecutionUpdateFromExternalOrderEvent(ctx, rec, *order, event)
}

// mergeOrderRecordFromExternalEvent 把外部订单事件合并进现有 OrderRecord。
//
// 这里采用“保守增量覆盖”策略：
// - 有新状态就更新状态；
// - executed qty 只会向上取更大的值，避免被旧事件回退；
// - average price 有值时才覆盖；
// - error/raw payload 有值时才覆盖。
//
// 这样做可以降低“乱序事件把订单状态倒退回去”的风险。
func mergeOrderRecordFromExternalEvent(order *entity.OrderRecord, event ExternalOrderEvent) {
	if order == nil {
		return
	}
	order.Exchange = pickNonEmpty(event.Exchange, order.Exchange)
	order.ClientOrderID = pickNonEmpty(event.ClientOrderID, order.ClientOrderID)
	order.VenueOrderID = pickNonEmpty(event.VenueOrderID, order.VenueOrderID)
	order.Status = pickNonEmpty(event.Status, order.Status)
	order.ExecutedQty = maxFloat(order.ExecutedQty, event.ExecutedQty)
	if event.AveragePrice > 0 {
		order.AvgPrice = event.AveragePrice
	}
	order.RawResponse = pickNonEmpty(event.RawPayload, order.RawResponse)
	order.ErrorMessage = pickNonEmpty(event.ErrorMessage, order.ErrorMessage)
}

// applyExecutionUpdateFromExternalOrderEvent 尝试把一条外部订单事件折算成 execution 层状态推进。
//
// 当前这个函数刻意保持保守：
// 1. 只有当 execution 已经处于某个 open/close 相关状态时才尝试推进；
// 2. 推进目标仍然来自“当前整组 order records 的汇总”，而不是只看这一条 event；
// 3. 如果汇总后的 target status 和当前相同，就只更新 order，不强行写 execution。
//
// 这种做法的好处是：
// - 外部事件不会单条地、武断地改 execution；
// - execution 始终还是基于整组腿级事实来推进；
// - 但状态机已经具备了“后续由异步事件继续修正”的能力。
func (s *ExecutionService) applyExecutionUpdateFromExternalOrderEvent(ctx context.Context, rec *entity.ExecutionRecord, updatedOrder entity.OrderRecord, event ExternalOrderEvent) (*entity.ExecutionRecord, error) {
	policy, graph, ok := resolveExecutionTrackingContext(rec, updatedOrder)
	if !ok {
		return rec, nil
	}

	orders, err := s.orderRepo.ListByPlanKey(ctx, updatedOrder.PlanKey)
	if err != nil {
		return rec, err
	}

	targetStatus := summarizeExecutionStatus(orders, policy.phase)
	if normalizeExecutionStatus(targetStatus) == normalizeExecutionStatus(rec.Status) {
		return rec, nil
	}

	if err := applyExecutionEvent(rec, executionEvent{
		Name:         executionEventNameFromExternalOrderEvent(event),
		TargetStatus: targetStatus,
		Reason:       summarizeExternalOrderEventReason(updatedOrder, event, targetStatus),
		OccurredAtMs: event.OccurredAtMs,
	}, graph); err != nil {
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}

	if shouldRecordOpenedAt(policy.phase) && normalizeExecutionStatus(targetStatus) == normalizeExecutionStatus(policy.successStatus) && rec.OpenedAtMs == 0 {
		rec.OpenedAtMs = event.OccurredAtMs
	}
	if shouldRecordClosedAt(policy.phase) && normalizeExecutionStatus(targetStatus) == normalizeExecutionStatus(policy.successStatus) && rec.ClosedAtMs == 0 {
		rec.ClosedAtMs = event.OccurredAtMs
	}
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// resolveExecutionTrackingContext 根据 execution 当前状态和订单 phase，
// 判断这条外部订单事件应该落到 open 侧还是 close 侧状态机。
//
// 当前优先级是：
// 1. 先看 execution 当前 status 家族；
// 2. 再 fallback 到订单 phase。
//
// 这样做是为了兼容未来一些“订单 phase 比 execution 当前状态更滞后或更超前”的场景。
func resolveExecutionTrackingContext(rec *entity.ExecutionRecord, order entity.OrderRecord) (executionPhasePolicy, executionTransitionGraph, bool) {
	if rec == nil {
		return executionPhasePolicy{}, executionTransitionGraph{}, false
	}

	switch {
	case belongsToOpenExecutionFamily(rec.Status):
		return openExecutionPolicy, openExecutionTransitions, true
	case belongsToCloseExecutionFamily(rec.Status):
		return closeExecutionPolicy, closeExecutionTransitions, true
	}

	switch normalizeExecutionStatus(order.Phase) {
	case "open", "hedge_close":
		return openExecutionPolicy, openExecutionTransitions, true
	case "close":
		return closeExecutionPolicy, closeExecutionTransitions, true
	default:
		return executionPhasePolicy{}, executionTransitionGraph{}, false
	}
}

func belongsToOpenExecutionFamily(status string) bool {
	switch normalizeExecutionStatus(status) {
	case executionStatePendingOpen, executionStateOpened, executionStateOpenPartial, executionStateOpenFailed, executionStateOpenHedging, executionStateRiskBlocked, executionStateCircuitOpen, "dry_run_opened":
		return true
	default:
		return false
	}
}

func belongsToCloseExecutionFamily(status string) bool {
	switch normalizeExecutionStatus(status) {
	case executionStatePendingClose, executionStateClosed, executionStateClosePartial, executionStateCloseFailed, executionStateCloseHedging, "dry_run_closed":
		return true
	default:
		return false
	}
}

func executionEventNameFromExternalOrderEvent(event ExternalOrderEvent) string {
	status := strings.ToLower(strings.TrimSpace(event.Status))
	status = strings.ReplaceAll(status, " ", "_")
	if status == "" {
		status = "update"
	}
	return normalizeExecutionEventName(pickNonEmpty(event.Source, "external") + "_order_" + status)
}

func summarizeExternalOrderEventReason(order entity.OrderRecord, event ExternalOrderEvent, targetStatus string) string {
	return fmt.Sprintf(
		"external order event source=%s phase=%s leg=%s exchange=%s order_status=%s executed_qty=%.8f target_execution_status=%s",
		pickNonEmpty(event.Source, "external"),
		order.Phase,
		order.LegRole,
		order.Exchange,
		pickNonEmpty(event.Status, order.Status),
		maxFloat(order.ExecutedQty, event.ExecutedQty),
		normalizeExecutionStatus(targetStatus),
	)
}
