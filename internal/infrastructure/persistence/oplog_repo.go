package persistence

import (
	"context"

	"goKit/internal/domain/oplog"
	"goKit/pkg/kit/db"
)

type oplogRepo struct {
	client *db.Client
}

func NewOplogRepository(client *db.Client) oplog.Repository {
	return &oplogRepo{client: client}
}

func (r *oplogRepo) Create(ctx context.Context, log *oplog.OperationLog) error {
	return r.client.GetDB(ctx).Create(log).Error
}

func (r *oplogRepo) List(ctx context.Context, q oplog.Query, page, pageSize int) ([]oplog.OperationLog, int64, error) {
	tx := r.client.GetDB(ctx).Model(&oplog.OperationLog{})
	if q.Username != "" {
		tx = tx.Where("username LIKE ?", "%"+q.Username+"%")
	}
	if q.Method != "" {
		tx = tx.Where("method = ?", q.Method)
	}
	if q.Path != "" {
		tx = tx.Where("path LIKE ?", "%"+q.Path+"%")
	}
	if q.Status != nil {
		tx = tx.Where("status = ?", *q.Status)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []oplog.OperationLog
	err := tx.Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&logs).Error
	return logs, total, err
}
