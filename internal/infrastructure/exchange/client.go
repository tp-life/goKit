package exchange

import (
	"context"
	"errors"

	"goKit/internal/domain/entity"
)

// ExchangeClient 交易所客户端接口
type ExchangeClient interface {
	// 市场数据
	GetMarketData(ctx context.Context, symbol string) (*entity.MarketData, error)
	SubscribeMarketData(ctx context.Context, symbols []string) (<-chan *entity.MarketData, error)
	
	// 资金费率
	GetFundingRate(ctx context.Context, symbol string) (*entity.FundingRate, error)
	
	// 连接管理
	Connect(ctx context.Context) error
	Close() error
	IsConnected() bool
	
	// 交易所名称
	Name() string
}

// ExchangeFactory 交易所工厂
type ExchangeFactory struct {
	clients map[string]ExchangeClient
}

func NewExchangeFactory() *ExchangeFactory {
	return &ExchangeFactory{
		clients: make(map[string]ExchangeClient),
	}
}

func (f *ExchangeFactory) Register(name string, client ExchangeClient) {
	f.clients[name] = client
}

func (f *ExchangeFactory) GetClient(name string) (ExchangeClient, error) {
	client, ok := f.clients[name]
	if !ok {
		return nil, ErrExchangeNotFound
	}
	return client, nil
}

func (f *ExchangeFactory) GetAllClients() map[string]ExchangeClient {
	return f.clients
}

var ErrExchangeNotFound = errors.New("exchange not found")
