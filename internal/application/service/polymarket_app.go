package service

import (
	"context"

	"goKit/internal/application/dto"
	"goKit/internal/domain/entity"
)

// PolymarketApp 定义 TUI、HTTP 与生命周期层依赖的统一能力。
type PolymarketApp interface {
	Snapshot() entity.DashboardState
	Logs() []entity.ActivityLog
	History() any
	Subscribe() (int, <-chan entity.DashboardState)
	Unsubscribe(id int)
	DefaultTradeAmount() float64
	SubmitTUIQuickOrder(ctx context.Context, action, outcome string) (*dto.ManualOrderResp, error)
	CancelActiveOrder(ctx context.Context) error
	SubmitManualOrder(ctx context.Context, req dto.ManualOrderReq) (*dto.ManualOrderResp, error)
	SelectNextMarket() string
	SelectPrevMarket() string
	CurrentMarketKey() string
}
