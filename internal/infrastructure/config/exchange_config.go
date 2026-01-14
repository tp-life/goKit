package config

import (
	"time"

	"goKit/internal/infrastructure/exchange"
	"goKit/internal/infrastructure/exchange/binance"
	"goKit/internal/infrastructure/exchange/hyperliquid"
	"goKit/internal/infrastructure/exchange/lighter"
)

type ExchangeConfig struct {
	Binance     ExchangeItemConfig `mapstructure:"binance"`
	Lighter     ExchangeItemConfig `mapstructure:"lighter"`
	Hyperliquid ExchangeItemConfig `mapstructure:"hyperliquid"`
}

type ExchangeItemConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	SpotWSURL         string        `mapstructure:"spot_ws_url"`
	FuturesWSURL      string        `mapstructure:"futures_ws_url"`
	WSURL             string        `mapstructure:"ws_url"`
	RESTURL           string        `mapstructure:"rest_url"`
	ReconnectInterval time.Duration `mapstructure:"reconnect_interval"`
	RateLimit         int           `mapstructure:"rate_limit"`
	ProxyURL          string        `mapstructure:"proxy_url"`
}

type ArbitrageConfig struct {
	DefaultHighThreshold   float64        `mapstructure:"default_high_threshold"`
	DefaultMediumThreshold float64        `mapstructure:"default_medium_threshold"`
	MinProfitability       float64        `mapstructure:"min_profitability"`
	MaxSinglePosition      float64        `mapstructure:"max_single_position"`
	MaxTotalPosition       float64        `mapstructure:"max_total_position"`
	MaxExecutionTime       time.Duration  `mapstructure:"max_execution_time"`
	PriceUpdateInterval    time.Duration  `mapstructure:"price_update_interval"`
	RateUpdateInterval     time.Duration  `mapstructure:"rate_update_interval"`
	Symbols                []SymbolConfig `mapstructure:"symbols"`
}

type SymbolConfig struct {
	Symbol            string `mapstructure:"symbol"`
	BinanceMarket     string `mapstructure:"binance_market"`
	LighterMarketID   int    `mapstructure:"lighter_market_id"`
	HyperliquidMarket string `mapstructure:"hyperliquid_market"`
	Enabled           bool   `mapstructure:"enabled"`
}

type TradingConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type FullAppConfig struct {
	App       map[string]interface{} `mapstructure:"app"`
	Web       interface{}            `mapstructure:"web"`
	RPC       interface{}            `mapstructure:"rpc"`
	Database  interface{}            `mapstructure:"database"`
	Exchanges ExchangeConfig         `mapstructure:"exchanges"`
	Arbitrage ArbitrageConfig        `mapstructure:"arbitrage"`
	Trading   TradingConfig          `mapstructure:"trading"`
}

func (c *ExchangeItemConfig) ToBinanceConfig(symbolMapper *exchange.SymbolMapper) binance.Config {
	return binance.Config{
		SpotWSURL:         c.SpotWSURL,
		FuturesWSURL:      c.FuturesWSURL,
		RESTURL:           c.RESTURL,
		ReconnectInterval: c.ReconnectInterval,
		RateLimit:         c.RateLimit,
		ProxyURL:          c.ProxyURL,
		SymbolMapper:      symbolMapper,
	}
}

func (c *ExchangeItemConfig) ToLighterConfig(symbolMapper *exchange.SymbolMapper) lighter.Config {
	return lighter.Config{
		WSURL:             c.WSURL,
		RESTURL:           c.RESTURL,
		ReconnectInterval: c.ReconnectInterval,
		RateLimit:         c.RateLimit,
		ProxyURL:          c.ProxyURL,
		SymbolMapper:      symbolMapper,
	}
}

func (c *ExchangeItemConfig) ToHyperliquidConfig(symbolMapper *exchange.SymbolMapper) hyperliquid.Config {
	return hyperliquid.Config{
		WSURL:             c.WSURL,
		RESTURL:           c.RESTURL,
		ReconnectInterval: c.ReconnectInterval,
		RateLimit:         c.RateLimit,
		ProxyURL:          c.ProxyURL,
		SymbolMapper:      symbolMapper,
	}
}
