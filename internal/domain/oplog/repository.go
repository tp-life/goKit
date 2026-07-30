package oplog

import "context"

// Query 操作日志列表过滤条件（零值表示不过滤）
type Query struct {
	Username string // 操作人模糊匹配
	Method   string // HTTP 方法精确匹配
	Path     string // 路径模糊匹配
	Status   *int   // 状态码精确匹配
}

// Repository 操作日志仓储
type Repository interface {
	Create(ctx context.Context, log *OperationLog) error
	// List 按时间倒序分页查询
	List(ctx context.Context, q Query, page, pageSize int) ([]OperationLog, int64, error)
}
