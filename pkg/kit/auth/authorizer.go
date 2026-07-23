package auth

import "context"

// Authorizer 授权端口：功能权限校验的抽象。
// 单机模式注入本地实现（查库+缓存），微服务模式注入 gRPC 远程实现。
// 业务代码只依赖该接口，切换部署模式零改动。
type Authorizer interface {
	// CheckPerm 校验用户是否拥有指定权限点（如 "system:user:list"）
	CheckPerm(ctx context.Context, userID uint64, permCode string) (bool, error)
}
