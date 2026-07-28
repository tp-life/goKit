package rpc

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcHandledTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gokit",
		Name:      "grpc_server_handled_total",
		Help:      "gRPC 请求总数（按方法与状态码）",
	}, []string{"method", "code"})

	grpcHandlingSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gokit",
		Name:      "grpc_server_handling_seconds",
		Help:      "gRPC 请求耗时分布",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})
)

// MetricsUnaryInterceptor gRPC 请求计数与耗时直方图
func MetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		start := time.Now()
		resp, err = handler(ctx, req)
		grpcHandledTotal.WithLabelValues(info.FullMethod, status.Code(err).String()).Inc()
		grpcHandlingSeconds.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
		return resp, err
	}
}
