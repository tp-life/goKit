// pkg/kit/log/handler.go
package log

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type TraceHandler struct {
	slog.Handler
}

// Handle 重写 Handle 方法，自动注入 TraceID
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	// 假设你的 TraceID Key 是 "trace_id" 或者 "request_id"
	// 这里适配 Fiber 的 requestid 中间件通常使用的 key，或者你自定义的 key
	if traceID, ok := ctx.Value("requestid").(string); ok && traceID != "" {
		r.AddAttrs(slog.String("trace_id", traceID))
	}

	// 如果你有 OpenTelemetry，也可以在这里从 spanContext 获取
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		r.AddAttrs(slog.String("trace_id", span.SpanContext().TraceID().String()))
	}

	return h.Handler.Handle(ctx, r)
}
