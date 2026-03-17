package exchange

import (
	"os"
	"strings"
	"time"
)

type FeeConfig struct {
	MakerBps float64 `mapstructure:"maker_bps"`
	TakerBps float64 `mapstructure:"taker_bps"`
}

type ProxyConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
}

type AuthConfig struct {
	APIKeyEnv         string `mapstructure:"api_key_env"`
	APISecretEnv      string `mapstructure:"api_secret_env"`
	PrivateKeyEnv     string `mapstructure:"private_key_env"`
	AccountAddressEnv string `mapstructure:"account_address_env"`
	VaultAddressEnv   string `mapstructure:"vault_address_env"`
}

type ExchangeConfig struct {
	Enabled                     bool          `mapstructure:"enabled"`
	RestBaseURL                 string        `mapstructure:"rest_base_url"`
	MarketWSBaseURL             string        `mapstructure:"market_ws_base_url"`
	PublicWSBaseURL             string        `mapstructure:"public_ws_base_url"`
	RequestTimeout              time.Duration `mapstructure:"request_timeout"`
	DefaultFundingIntervalHours int           `mapstructure:"default_funding_interval_hours"`
	Fees                        FeeConfig     `mapstructure:"fees"`
	Proxy                       ProxyConfig   `mapstructure:"proxy"`
	Auth                        AuthConfig    `mapstructure:"auth"`
	SettleAsset                 string        `mapstructure:"settle_asset"`
	Label                       string        `mapstructure:"label"`
}

type ConfigSet struct {
	Binance     ExchangeConfig `mapstructure:"binance"`
	Aster       ExchangeConfig `mapstructure:"aster"`
	Hyperliquid ExchangeConfig `mapstructure:"hyperliquid"`
}

func (c ConfigSet) Items() map[string]ExchangeConfig {
	return map[string]ExchangeConfig{
		"binance":     c.Binance,
		"aster":       c.Aster,
		"hyperliquid": c.Hyperliquid,
	}
}

type ConnectorStatus struct {
	Exchange            string    `json:"exchange"`
	MarkPriceConnected  bool      `json:"mark_price_connected"`
	BookTickerConnected bool      `json:"book_ticker_connected"`
	LastMarketEventAt   time.Time `json:"last_market_event_at"`
	LastBookEventAt     time.Time `json:"last_book_event_at"`
	LastError           string    `json:"last_error,omitempty"`
}

type AppConfig struct {
	Debug bool `mapstructure:"debug"`
}

func normalizeExchangeConfig(name string, c ExchangeConfig) ExchangeConfig {
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.DefaultFundingIntervalHours <= 0 {
		if name == "hyperliquid" {
			c.DefaultFundingIntervalHours = 1
		} else {
			c.DefaultFundingIntervalHours = 8
		}
	}
	if strings.TrimSpace(c.SettleAsset) == "" {
		switch name {
		case "hyperliquid":
			c.SettleAsset = "USDC"
		default:
			c.SettleAsset = "USDT"
		}
	}
	if strings.TrimSpace(c.Label) == "" {
		c.Label = strings.Title(name)
	}
	return c
}

func readEnvByName(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(name))
}
