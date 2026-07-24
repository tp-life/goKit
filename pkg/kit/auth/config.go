package auth

import "time"

// Config JWT 配置
type Config struct {
	Secret        string        `mapstructure:"secret"`
	Issuer        string        `mapstructure:"issuer"`
	ExpireMinutes time.Duration `mapstructure:"expire_minutes"` // 令牌有效期（分钟）
}

// AuthzConfig 授权模式配置
// mode=local  单机模式：进程内直接查库校验权限
// mode=remote 微服务模式：通过 gRPC 调用系统服务的 AuthzService
type AuthzConfig struct {
	Mode string `mapstructure:"mode"` // local | remote
	Addr string `mapstructure:"addr"` // remote 模式下系统服务 gRPC 地址，如 "127.0.0.1:9090"
}
