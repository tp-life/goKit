package web

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gokit",
		Name:      "http_requests_total",
		Help:      "HTTP 请求总数",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gokit",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP 请求耗时分布",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// MetricsMiddleware 请求计数与耗时直方图。
// path 取路由模板（如 /users/:id），避免路径参数导致标签基数爆炸
func MetricsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		route := c.Route().Path
		if route == "" || route == "/" {
			route = "unknown"
		}
		httpRequestsTotal.WithLabelValues(c.Method(), route, strconv.Itoa(c.Response().StatusCode())).Inc()
		httpRequestDuration.WithLabelValues(c.Method(), route).Observe(time.Since(start).Seconds())
		return err
	}
}

// MetricsHandler Prometheus 暴露端点
func MetricsHandler() fiber.Handler {
	return adaptor.HTTPHandler(promhttp.Handler())
}
