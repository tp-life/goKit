package polymarket

import "context"

// PlaceOrderOptions 描述提交限价单时可选的执行属性。
type PlaceOrderOptions struct {
	OrderType    string
	ExpirationTS int64
	PostOnly     bool
}

// Authenticator 定义 Polymarket API 凭证引导相关能力。
type Authenticator interface {
	CreateOrDeriveAPIKey(ctx context.Context, nonce int) (*APIKeyCreds, error)
}

// OrderClient 定义下单、撤单和订单元数据查询能力。
type OrderClient interface {
	PlaceLimitOrder(ctx context.Context, tokenID string, action string, price float64, sizeShares float64) (string, float64, error)
	PlaceLimitOrderWithOptions(ctx context.Context, tokenID string, action string, price float64, sizeShares float64, opts PlaceOrderOptions) (string, float64, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error)
	GetTickSize(ctx context.Context, tokenID string) (string, error)
	GetNegRisk(ctx context.Context, tokenID string) (bool, error)
	GetFeeRateBps(ctx context.Context, tokenID string) (int, error)
}

// MarketClient 定义市场发现和盘口数据相关能力。
type MarketClient interface {
	GetActiveMarket(ctx context.Context) (*ActiveMarketView, error)
	GetOrderBook(ctx context.Context, tokenID string) (*OrderBookSummary, error)
	GetOrderBookHash(orderbook *OrderBookSummary) (string, error)
	ValidateOrderBookHash(orderbook *OrderBookSummary) (bool, string, error)
	SubscribeMarket(ctx context.Context, upToken, downToken string, onUpdate func(assetID string, bid, ask, mid float64)) error
}

// WalletClient 定义外部服务常用的钱包辅助能力。
type WalletClient interface {
	AddressHex() string
	FunderHex() string
	HasPrivateKey() bool
	GetERC20Balance(ctx context.Context, account string) (*float64, error)
	GetWalletPositions(ctx context.Context, user string) ([]DataPositionResponse, error)
	GetWalletClosedPositions(ctx context.Context, user string) ([]DataClosedPositionResponse, error)
	GetTradeActivity(ctx context.Context, user string, limit int) ([]DataActivityResponse, error)
}

// SDK 表示上层服务消费的完整 Polymarket 基础设施能力集合。
type SDK interface {
	Authenticator
	OrderClient
	MarketClient
	WalletClient
	RedeemClient
}

// RedeemClient 定义兑奖相关的查询与执行能力。
type RedeemClient interface {
	GetRedeemableConditions(ctx context.Context, user string) ([]string, int, error)
	RedeemCondition(ctx context.Context, conditionID string) (*RedeemResult, error)
}
