package repository

import (
	"context"
	"time"
)

// OrderRequest 订单请求
type OrderRequest struct {
	Exchange  string
	Symbol    string
	Side      string // buy/sell
	Type      string // market/limit
	Price     *float64
	Quantity  float64
	ClientID  string
}

// Order 订单
type Order struct {
	ID          string
	Exchange    string
	Symbol      string
	Side        string
	Type        string
	Price       float64
	Quantity    float64
	Status      string // pending/filled/cancelled/failed
	FilledQty   float64
	FilledPrice float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Balance 账户余额
type Balance struct {
	Exchange string
	Asset    string
	Free     float64
	Locked   float64
	Total    float64
}

// TradingRepository 交易仓库接口（预留）
type TradingRepository interface {
	// 下单
	PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error)
	
	// 查询订单
	GetOrder(ctx context.Context, exchange, orderID string) (*Order, error)
	
	// 取消订单
	CancelOrder(ctx context.Context, exchange, orderID string) error
	
	// 查询余额
	GetBalance(ctx context.Context, exchange, asset string) (*Balance, error)
	
	// 查询所有余额
	GetAllBalances(ctx context.Context, exchange string) ([]*Balance, error)
}
