package exchange

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/subosito/gotenv"
)

const (
	// AdapterKindBinanceLike 表示“Binance Futures 风格”的接入协议族。
	//
	// 这里故意不用宽泛的 `cex` 命名，原因是：
	// 1. CEX 只是交易场所类型，不代表接口协议一致；
	// 2. Binance / Aster 这类 USD-M Futures 接口只是众多 CEX 协议中的一族；
	// 3. 如果继续把它叫 `cex`，会误导人以为 OKX / Bybit / Bitget 都能直接复用。
	AdapterKindBinanceLike = "binance_like"

	// AdapterKindBybitV5 表示 Bybit V5 永续合约协议族。
	//
	// 这里使用 `bybit_v5` 而不是模糊的 `bybit` / `cex`，是为了明确两件事：
	// 1. 当前实现绑定的是 Bybit V5 这套 REST 协议；
	// 2. 它和 Binance-like 属于不同 family，不能共用同一个 market/trade client。
	//
	// 当前实现默认面向 perpetual（尤其是 linear perpetual）主流程；
	// 如果后续要扩到 inverse / option，应优先在这个 family 上继续细分能力，
	// 而不是回头把它塞进通用 `cex` 抽象。
	AdapterKindBybitV5 = "bybit_v5"

	// AdapterKindHyperliquid 表示 Hyperliquid 专属接入协议族。
	AdapterKindHyperliquid = "hyperliquid"
)

type FeeConfig struct {
	// 这里同时保留 mapstructure/json 标签。
	// 原因是同一份结构既会从 YAML 配置反序列化，也会经由 system status API
	// 发给 TUI / Web；如果缺少 json 标签，snake_case 的 maker_bps/taker_bps
	// 在 TUI 侧解码时会落成 0，导致“收益构成”里手续费明细显示错误。
	MakerBps float64 `mapstructure:"maker_bps" json:"maker_bps"`
	TakerBps float64 `mapstructure:"taker_bps" json:"taker_bps"`
}

type ProxyConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
}

type AuthConfig struct {
	APIKeyEnv         string            `mapstructure:"api_key_env"`
	APISecretEnv      string            `mapstructure:"api_secret_env"`
	PassphraseEnv     string            `mapstructure:"passphrase_env"`
	PrivateKeyEnv     string            `mapstructure:"private_key_env"`
	AccountAddressEnv string            `mapstructure:"account_address_env"`
	VaultAddressEnv   string            `mapstructure:"vault_address_env"`
	ExtraEnv          map[string]string `mapstructure:"extra_env"`
}

type ExchangeConfig struct {
	AdapterKind                 string            `mapstructure:"adapter_kind"`
	VenueKind                   string            `mapstructure:"venue_kind"`
	Enabled                     bool              `mapstructure:"enabled"`
	RestBaseURL                 string            `mapstructure:"rest_base_url"`
	MarketWSBaseURL             string            `mapstructure:"market_ws_base_url"`
	PublicWSBaseURL             string            `mapstructure:"public_ws_base_url"`
	PrivateWSBaseURL            string            `mapstructure:"private_ws_base_url"`
	RequestTimeout              time.Duration     `mapstructure:"request_timeout"`
	DefaultFundingIntervalHours int               `mapstructure:"default_funding_interval_hours"`
	Fees                        FeeConfig         `mapstructure:"fees"`
	Proxy                       ProxyConfig       `mapstructure:"proxy"`
	Auth                        AuthConfig        `mapstructure:"auth"`
	SettleAsset                 string            `mapstructure:"settle_asset"`
	Label                       string            `mapstructure:"label"`
	AdapterOptions              map[string]string `mapstructure:"adapter_options"`
}

type ConfigSet struct {
	Binance     ExchangeConfig            `mapstructure:"binance"`
	Aster       ExchangeConfig            `mapstructure:"aster"`
	Hyperliquid ExchangeConfig            `mapstructure:"hyperliquid"`
	Additional  map[string]ExchangeConfig `mapstructure:",remain"`
}

func (c ConfigSet) Items() map[string]ExchangeConfig {
	items := map[string]ExchangeConfig{
		"binance":     c.Binance,
		"aster":       c.Aster,
		"hyperliquid": c.Hyperliquid,
	}
	for name, cfg := range c.Additional {
		items[strings.ToLower(strings.TrimSpace(name))] = cfg
	}
	return items
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

var (
	dotEnvValuesOnce sync.Once
	dotEnvValues     map[string]string
)

func normalizeExchangeConfig(name string, c ExchangeConfig) ExchangeConfig {
	// adapter_kind 只对“仓库内置的已知交易所”提供默认值。
	//
	// 这里刻意不再给未知交易所兜底成某个通用 kind，原因是：
	// - 过去把未知交易所默认归到 `cex`，会制造“所有 CEX 接口都差不多”的错觉；
	// - 实际上 OKX / Bybit / Bitget / Gate 等接口、签名、symbol、订单语义都可能明显不同；
	// - 因此新增交易所时，必须显式声明自己复用哪一个协议族，或者新增一个新的协议族实现。
	if strings.TrimSpace(c.AdapterKind) == "" {
		switch name {
		case "binance", "aster":
			c.AdapterKind = AdapterKindBinanceLike
		case "hyperliquid":
			c.AdapterKind = AdapterKindHyperliquid
		}
	}
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
	if value := readProcessEnvByName(name); value != "" {
		return value
	}
	return readDotEnvByName(name)
}

func readProcessEnvByName(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(name))
}

func readDotEnvByName(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	values := loadDotEnvValues()
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[strings.TrimSpace(name)])
}

func loadDotEnvValues() map[string]string {
	dotEnvValuesOnce.Do(func() {
		values, err := gotenv.Read(".env")
		if err != nil {
			dotEnvValues = map[string]string{}
			return
		}
		dotEnvValues = values
	})
	return dotEnvValues
}

func readCredentialPair(primaryEnvName, secondaryEnvName string) (string, string) {
	primaryProcess := readProcessEnvByName(primaryEnvName)
	secondaryProcess := readProcessEnvByName(secondaryEnvName)
	if primaryProcess != "" && secondaryProcess != "" {
		return primaryProcess, secondaryProcess
	}

	primaryDotEnv := readDotEnvByName(primaryEnvName)
	secondaryDotEnv := readDotEnvByName(secondaryEnvName)
	if primaryDotEnv != "" && secondaryDotEnv != "" {
		return primaryDotEnv, secondaryDotEnv
	}

	return firstNonEmpty(primaryProcess, primaryDotEnv), firstNonEmpty(secondaryProcess, secondaryDotEnv)
}

// AdapterOption 返回某个协议族私有配置项的值。
//
// 这层扩展位的目的，是给未来的 Bybit / OKX / Bitget / Gate 等协议族留出空间，
// 避免每新增一个交易所，就把 ExchangeConfig 顶层再塞进一组只对单一家族有意义的字段。
func (c ExchangeConfig) AdapterOption(key string) string {
	if len(c.AdapterOptions) == 0 {
		return ""
	}
	return strings.TrimSpace(c.AdapterOptions[strings.TrimSpace(key)])
}

// ResolveExtraEnv 把 `auth.extra_env` 中声明的环境变量名解析成真实值。
//
// 例如未来某个协议族需要：
// - OKX passphrase 以外的 broker/project header
// - 某些 venue 的 subaccount key
// - 其他只对单个家族有意义的敏感配置
//
// 就可以先通过这层扩展位接入，而不是立刻把 AuthConfig 继续全局膨胀。
func (a AuthConfig) ResolveExtraEnv() map[string]string {
	if len(a.ExtraEnv) == 0 {
		return nil
	}
	out := make(map[string]string, len(a.ExtraEnv))
	for key, envName := range a.ExtraEnv {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = readEnvByName(envName)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
