package auth

import "context"

// UserInfo 跨模块共享的用户视图（不含密码等敏感字段）
type UserInfo struct {
	ID       uint64
	Username string
	Nickname string
	Email    string
	DeptID   uint64
	Status   int8 // 1 正常 0 停用
	IsSuper  bool
}

// UserProvider 用户信息端口：跨模块查询用户的抽象。
// 其他模块需要用户信息时只依赖该接口，无需 import system 模块内部。
// 单机模式注入本地实现（查库），微服务模式注入 gRPC 远程实现。
type UserProvider interface {
	// GetUser 按 ID 查询用户，不存在返回 (nil, nil)
	GetUser(ctx context.Context, id uint64) (*UserInfo, error)
	// GetUsers 批量查询用户，返回以用户 ID 为键的映射（不存在的 ID 不出现）
	GetUsers(ctx context.Context, ids []uint64) (map[uint64]*UserInfo, error)
}
