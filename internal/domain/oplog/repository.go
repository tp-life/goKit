package oplog

import "context"

// Repository 操作日志仓储
type Repository interface {
	Create(ctx context.Context, log *OperationLog) error
	// List 按时间倒序分页查询
	List(ctx context.Context, page, pageSize int) ([]OperationLog, int64, error)
}
