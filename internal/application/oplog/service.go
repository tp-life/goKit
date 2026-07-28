// Package oplog 操作日志用例：异步落库审计记录 + 分页查询
package oplog

import (
	"context"
	"time"

	"goKit/internal/application/shared"
	"goKit/internal/domain/oplog"
)

// OplogResp 操作日志响应
type OplogResp struct {
	ID        uint64    `json:"id"`
	UserID    uint64    `json:"user_id"`
	Username  string    `json:"username"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	IP        string    `json:"ip"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	CreatedAt time.Time `json:"created_at"`
}

type OplogService struct {
	repo oplog.Repository
}

func NewOplogService(repo oplog.Repository) *OplogService {
	return &OplogService{repo: repo}
}

// Record 记录一条操作日志
func (s *OplogService) Record(ctx context.Context, entry *oplog.OperationLog) error {
	return s.repo.Create(ctx, entry)
}

// List 操作日志分页列表（仅按权限点控制，不叠加数据权限）
func (s *OplogService) List(ctx context.Context, page shared.PageReq) (*shared.PageResp[*OplogResp], error) {
	page.Normalize()
	logs, total, err := s.repo.List(ctx, page.Page, page.PageSize)
	if err != nil {
		return nil, err
	}
	list := make([]*OplogResp, 0, len(logs))
	for i := range logs {
		list = append(list, toResp(&logs[i]))
	}
	return &shared.PageResp[*OplogResp]{List: list, Total: total}, nil
}

func toResp(l *oplog.OperationLog) *OplogResp {
	return &OplogResp{
		ID:        l.ID,
		UserID:    l.UserID,
		Username:  l.Username,
		Method:    l.Method,
		Path:      l.Path,
		IP:        l.IP,
		Status:    l.Status,
		LatencyMs: l.LatencyMs,
		CreatedAt: l.CreatedAt,
	}
}
