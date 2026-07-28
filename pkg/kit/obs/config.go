// Package obs 可观测性：OTel 链路追踪初始化（指标见 web/rpc 各自包内）
package obs

import "time"

// TraceConfig 链路追踪配置。
// Endpoint 为空则不启用（全局 Noop Provider，零开销）；
// 非空时通过 OTLP gRPC 上报（如 "otel-collector:4317"，内网明文）
type TraceConfig struct {
	Endpoint    string        `mapstructure:"endpoint"`
	ServiceName string        `mapstructure:"service_name"`
	SampleRatio float64       `mapstructure:"sample_ratio"` // 采样率 0~1，默认 1
	Timeout     time.Duration `mapstructure:"timeout"`      // 导出超时，默认 5s
}
