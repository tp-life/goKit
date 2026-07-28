// Package oplog 操作日志聚合：记录管理端写操作（登录、增删改）的审计信息
package oplog

import "time"

// OperationLog 操作日志
type OperationLog struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	UserID    uint64    `gorm:"index"`          // 操作人（未登录为 0，如登录失败）
	Username  string    `gorm:"size:64"`        // 操作人账号（登录接口取请求体中的账号）
	Method    string    `gorm:"size:10"`        // HTTP 方法
	Path      string    `gorm:"size:256;index"` // 路由模板，如 /api/v1/users/:id
	IP        string    `gorm:"size:64"`        // 客户端 IP
	Status    int       `gorm:"index"`          // HTTP 状态码
	LatencyMs int64     // 耗时（毫秒）
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (OperationLog) TableName() string { return "sys_operation_logs" }
