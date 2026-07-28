package obs

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/fx"
)

// InitTracing 按配置初始化 OTel TracerProvider。
// Endpoint 为空时跳过（HTTP/gRPC 侧的埋点走全局 Noop Provider，零开销）；
// 非空时安装 OTLP gRPC 导出器，并在服务停止时优雅关闭。
func InitTracing(lc fx.Lifecycle, cfg TraceConfig, l *slog.Logger) {
	if cfg.Endpoint == "" {
		l.Info("tracing_disabled")
		return
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "gokit"
	}
	if cfg.SampleRatio <= 0 || cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	cancel()
	if err != nil {
		l.Error("tracing_init_failed", slog.Any("err", err))
		return
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	l.Info("tracing_enabled", slog.String("endpoint", cfg.Endpoint), slog.String("service", cfg.ServiceName))

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		},
	})
}
