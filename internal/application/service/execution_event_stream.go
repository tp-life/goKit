package service

import (
	"context"
	"log/slog"
	"strings"

	"goKit/internal/infrastructure/exchange"
)

// executionOrderEventSink 是 ExecutionService 暴露给交易所事件流的桥接器。
//
// 它的职责非常单一：
// 1. 从适配器侧接收 `exchange.OrderEvent`；
// 2. 把事件尽量快地写入 service 内部 channel；
// 3. 如果 channel 已满，则记录日志而不是阻塞适配器的读循环。
//
// 这样可以避免一个慢处理器把整个 websocket/user-stream goroutine 卡死。
type executionOrderEventSink struct {
	logger *slog.Logger
	ch     chan<- exchange.OrderEvent
}

func (s executionOrderEventSink) PublishOrderEvent(event exchange.OrderEvent) {
	select {
	case s.ch <- event:
	default:
		if s.logger != nil {
			s.logger.Warn("execution_order_event_dropped",
				slog.String("exchange", event.Exchange),
				slog.String("client_order_id", event.ClientOrderID),
				slog.String("venue_order_id", event.VenueOrderID),
				slog.String("status", event.Status),
			)
		}
	}
}

// startTradeOrderStreams 启动所有已实现订单事件流能力的交易所适配器。
//
// 当前这一步的设计重点是“把总线打通”，而不是要求所有适配器立即实现私有流。
// 因此这里会：
// 1. 遍历所有 trade adapters；
// 2. 只对实现了 `TradeOrderEventStreamer` 的适配器启动事件流；
// 3. 对未实现的适配器直接跳过，不影响现有同步执行逻辑。
func (s *ExecutionService) startTradeOrderStreams(ctx context.Context) {
	if s == nil {
		return
	}
	sink := executionOrderEventSink{
		logger: s.logger,
		ch:     s.orderEventCh,
	}
	for name, adapter := range s.trades {
		streamer, ok := adapter.(exchange.TradeOrderEventStreamer)
		if !ok || streamer == nil {
			continue
		}
		if adapter == nil || !adapter.Enabled() {
			continue
		}
		if !adapter.Capabilities().SupportsOrderEventStream {
			continue
		}

		go func(exchangeName string, streamer exchange.TradeOrderEventStreamer) {
			if s.logger != nil {
				s.logger.Info("execution_order_event_stream_start", slog.String("exchange", exchangeName))
			}
			if err := streamer.StartOrderEventStream(ctx, sink); err != nil && ctx.Err() == nil {
				if s.logger != nil {
					s.logger.Error("execution_order_event_stream_failed",
						slog.String("exchange", exchangeName),
						slog.Any("err", err),
					)
				}
			}
		}(name, streamer)
	}
}

// orderEventLoop 是 ExecutionService 消费外部订单事件的主循环。
//
// 它的职责是把 exchange 层的通用订单事件转成 service 层的 `ExternalOrderEvent`，
// 然后交给 `ApplyExternalOrderEvent` 做真正的 order/execution 更新。
//
// 这里刻意保持逻辑很薄，原因是：
// - 适配器负责“如何读到事件”；
// - `ApplyExternalOrderEvent` 负责“事件如何合并进领域模型”；
// - 这个 loop 只负责“搬运和记录结果”。
func (s *ExecutionService) orderEventLoop(ctx context.Context) {
	if s == nil || s.orderEventCh == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.orderEventCh:
			if _, err := s.ApplyExternalOrderEvent(ctx, s.mapExchangeOrderEvent(event)); err != nil {
				if s.logger != nil {
					s.logger.Error("execution_order_event_apply_failed",
						slog.String("exchange", event.Exchange),
						slog.String("client_order_id", event.ClientOrderID),
						slog.String("venue_order_id", event.VenueOrderID),
						slog.Any("err", err),
					)
				}
				continue
			}
			if s.logger != nil {
				s.logger.Info("execution_order_event_applied",
					slog.String("exchange", event.Exchange),
					slog.String("client_order_id", event.ClientOrderID),
					slog.String("venue_order_id", event.VenueOrderID),
					slog.String("status", strings.ToUpper(strings.TrimSpace(event.Status))),
				)
			}
		}
	}
}

func (s *ExecutionService) mapExchangeOrderEvent(event exchange.OrderEvent) ExternalOrderEvent {
	return ExternalOrderEvent{
		Source:        event.Source,
		Exchange:      event.Exchange,
		ClientOrderID: event.ClientOrderID,
		VenueOrderID:  event.VenueOrderID,
		Status:        event.Status,
		ExecutedQty:   event.ExecutedQty,
		AveragePrice:  event.AveragePrice,
		Terminal:      event.Terminal,
		Canceled:      event.Canceled,
		ErrorMessage:  event.ErrorMessage,
		RawPayload:    event.RawPayload,
		OccurredAtMs:  event.OccurredAtMs,
	}
}
