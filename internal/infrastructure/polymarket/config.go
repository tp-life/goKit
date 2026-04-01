package polymarket

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConditionConfig 表示一条自动交易触发规则。
type ConditionConfig struct {
	Time int
	// Diff 表示绝对价差阈值；主要用于兼容旧版 BTC 单市场配置。
	Diff float64
	// DiffBps 表示相对价差阈值，单位是 bps；大于 0 时优先于 Diff 生效，更适合多市场共用一套规则。
	DiffBps float64
	MinProb float64
	MaxProb float64
	Side    string
}

// MarketTargetConfig 表示一个需要并行跟踪的市场目标。
type MarketTargetConfig struct {
	Key         string
	Label       string
	Symbol      string
	IntervalSec int
}

// Config 汇总了 Polymarket SDK 及其相邻机器人能力的配置项。
type Config struct {
	Enabled              bool
	Host                 string
	RelayerURL           string
	GammaAPI             string
	DataAPI              string
	CryptoPriceAPI       string
	ClobWSURL            string
	RTDSWSURL            string
	ProxyURL             string
	MarketSymbol         string
	MarketIntervalSec    int
	MarketSlugPrefix     string
	MarketSlugInterval   string
	RTDSSymbol           string
	CryptoPriceSymbol    string
	CryptoPriceVariant   string
	ChainID              int64
	PrivateKey           string
	APIKey               string
	APISecret            string
	APIPassphrase        string
	BuilderAPIKey        string
	BuilderSecret        string
	BuilderPassphrase    string
	RelayerAPIKey        string
	RelayerAPIKeyAddress string
	FunderAddress        string
	SignatureType        int
	PolygonRPCURL        string
	AutoTrade            bool
	AutoRedeem           bool
	AutoRedeemHourLocal  int
	AutoRedeemMaxRetry   int
	AutoRedeemRetryDelay int
	AutoRedeemMaxPerRun  int
	AutoRedeemReceiptSec int
	AutoRedeemMinSize    float64
	CTFContract          string
	USDCCollateral       string
	TradeAmount          float64
	OrderTimeoutSec      int
	SlippageThreshold    float64
	MaxRetryPerMarket    int
	BuyRetryStep         float64
	StopLossProbPct      float64
	TakeProfitRR         float64
	TakeProfitCap        float64
	TakeProfitRetryStep  float64
	TakeProfitRetryMax   int
	MarketDataMaxLagSec  float64
	LoopIntervalSec      float64
	MarketMetaRefreshSec int
	PriceRefreshSec      int
	BinanceSymbol        string
	BinanceWSURL         string
	BinancePriceURL      string
	StateFile            string
	DashboardStaticDir   string
	Conditions           []ConditionConfig
	MarketTargets        []MarketTargetConfig
	EnableBalancePolling bool
	EnableAccountPolling bool
	EnableAutoRedeemer   bool
}

// LoadConfig 读取 Polymarket 相关环境变量，并转换成强类型配置。
func LoadConfig() (Config, error) {
	marketSymbol := normalizeMarketSymbol(getEnv("POLYMARKET_MARKET_SYMBOL", "BTC"))
	marketIntervalSec := getEnvInt("POLYMARKET_MARKET_INTERVAL_SEC", 900)

	cfg := Config{
		// 默认值与当前 BTC 15 分钟市场的运行方式保持一致，同时允许通过环境变量切换资产与周期。
		Enabled:              getEnvBool("POLYMARKET_ENABLED", true),
		Host:                 getEnv("POLYMARKET_HOST", "https://clob.polymarket.com"),
		RelayerURL:           getEnv("POLYMARKET_RELAYER_URL", "https://relayer-v2.polymarket.com"),
		GammaAPI:             getEnv("POLYMARKET_GAMMA_API", "https://gamma-api.polymarket.com"),
		DataAPI:              getEnv("POLYMARKET_DATA_API", "https://data-api.polymarket.com"),
		CryptoPriceAPI:       getEnv("POLYMARKET_CRYPTO_PRICE_API", "https://polymarket.com/api/crypto/crypto-price"),
		ClobWSURL:            getEnv("POLYMARKET_CLOB_WS", "wss://ws-subscriptions-clob.polymarket.com/ws/market"),
		RTDSWSURL:            getEnv("POLYMARKET_RTDS_WS", "wss://ws-live-data.polymarket.com"),
		ProxyURL:             getFirstEnv("POLYMARKET_PROXY_URL", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"),
		MarketSymbol:         marketSymbol,
		MarketIntervalSec:    marketIntervalSec,
		MarketSlugPrefix:     getEnv("POLYMARKET_MARKET_SLUG_PREFIX", defaultMarketSlugPrefix(marketSymbol)),
		MarketSlugInterval:   getEnv("POLYMARKET_MARKET_SLUG_INTERVAL", defaultMarketSlugInterval(marketIntervalSec)),
		RTDSSymbol:           getEnv("POLYMARKET_RTDS_SYMBOL", defaultRTDSSymbol(marketSymbol)),
		CryptoPriceSymbol:    getEnv("POLYMARKET_CRYPTO_PRICE_SYMBOL", marketSymbol),
		CryptoPriceVariant:   getEnv("POLYMARKET_CRYPTO_PRICE_VARIANT", defaultCryptoPriceVariant(marketIntervalSec)),
		ChainID:              getEnvInt64("POLYMARKET_CHAIN_ID", 137),
		PrivateKey:           strings.TrimSpace(getEnv("PRIVATE_KEY", "")),
		APIKey:               strings.TrimSpace(getEnv("POLYMARKET_API_KEY", "")),
		APISecret:            strings.TrimSpace(getEnv("POLYMARKET_API_SECRET", "")),
		APIPassphrase:        strings.TrimSpace(getEnv("POLYMARKET_API_PASSPHRASE", "")),
		BuilderAPIKey:        strings.TrimSpace(getEnv("POLY_BUILDER_API_KEY", "")),
		BuilderSecret:        strings.TrimSpace(getEnv("POLY_BUILDER_SECRET", "")),
		BuilderPassphrase:    strings.TrimSpace(getEnv("POLY_BUILDER_PASSPHRASE", "")),
		RelayerAPIKey:        strings.TrimSpace(getEnv("POLYMARKET_RELAYER_API_KEY", "")),
		RelayerAPIKeyAddress: strings.TrimSpace(getEnv("POLYMARKET_RELAYER_API_KEY_ADDRESS", "")),
		FunderAddress:        strings.TrimSpace(getEnv("FUNDER_ADDRESS", "")),
		SignatureType:        getEnvInt("SIGNATURE_TYPE", 2),
		PolygonRPCURL:        strings.TrimSpace(getEnv("POLYGON_RPC_URL", "")),
		AutoTrade:            getEnvBool("AUTO_TRADE", false),
		AutoRedeem:           getEnvBool("AUTO_REDEEM", false),
		AutoRedeemHourLocal:  getEnvInt("AUTO_REDEEM_HOUR_LOCAL", 3),
		AutoRedeemMaxRetry:   getEnvInt("AUTO_REDEEM_MAX_RETRY", 2),
		AutoRedeemRetryDelay: getEnvInt("AUTO_REDEEM_RETRY_DELAY_SEC", 15),
		AutoRedeemMaxPerRun:  getEnvInt("AUTO_REDEEM_MAX_PER_RUN", 5),
		AutoRedeemReceiptSec: getEnvInt("AUTO_REDEEM_RECEIPT_TIMEOUT_SEC", 90),
		AutoRedeemMinSize:    getEnvFloat("AUTO_REDEEM_MIN_SIZE", 0.01),
		CTFContract:          getEnv("CTF_CONTRACT", "0x4d97dcd97ec945f40cf65f87097ace5ea0476045"),
		USDCCollateral:       getEnv("USDC_E_CONTRACT", "0x2791bca1f2de4661ed88a30c99a7a9449aa84174"),
		TradeAmount:          getEnvFloat("TRADE_AMOUNT", 5),
		OrderTimeoutSec:      getEnvInt("ORDER_TIMEOUT_SEC", 8),
		SlippageThreshold:    getEnvFloat("SLIPPAGE_THRESHOLD", 0.05),
		MaxRetryPerMarket:    getEnvInt("MAX_RETRY_PER_MARKET", 2),
		BuyRetryStep:         getEnvFloat("BUY_RETRY_STEP", 0.01),
		StopLossProbPct:      getEnvFloat("STOP_LOSS_PROB_PCT", 0.15),
		TakeProfitRR:         getEnvFloat("TAKE_PROFIT_RR", 1.0),
		TakeProfitCap:        getEnvFloat("TAKE_PROFIT_CAP", 0.99),
		TakeProfitRetryStep:  getEnvFloat("TAKE_PROFIT_RETRY_STEP", 0.005),
		TakeProfitRetryMax:   getEnvInt("TAKE_PROFIT_RETRY_MAX", 3),
		MarketDataMaxLagSec:  getEnvFloat("MARKET_DATA_MAX_LAG_SEC", 1.2),
		LoopIntervalSec:      getEnvFloat("LOOP_INTERVAL_SEC", 0.25),
		MarketMetaRefreshSec: getEnvInt("MARKET_META_REFRESH_SEC", 5),
		PriceRefreshSec:      getEnvInt("PRICE_REFRESH_SEC", 5),
		BinanceSymbol:        getEnv("BINANCE_SYMBOL", defaultBinanceSymbol(marketSymbol)),
		BinanceWSURL:         getEnv("BINANCE_WS_URL", ""),
		BinancePriceURL:      getEnv("BINANCE_PRICE_URL", "https://api.binance.com/api/v3/ticker/price"),
		// 默认把运行时状态放到 data 目录，把 dashboard 静态资源放到 web 目录，彻底摆脱旧 Python 目录依赖。
		StateFile:            getEnv("POLYMARKET_STATE_FILE", filepath.Join("data", "polymarket", "state.json")),
		DashboardStaticDir:   getEnv("POLYMARKET_DASHBOARD_STATIC_DIR", filepath.Join("web", "polymarket")),
		EnableBalancePolling: true,
		EnableAccountPolling: true,
		EnableAutoRedeemer:   true,
	}

	cfg.Conditions = []ConditionConfig{
		{
			Time:    getEnvInt("CONDITION_1_TIME", 120),
			Diff:    getEnvFloat("CONDITION_1_DIFF", 30),
			DiffBps: getEnvFloat("CONDITION_1_DIFF_BPS", 0),
			MinProb: getEnvFloat("CONDITION_1_MIN_PROB", 0.80),
			MaxProb: getEnvFloat("CONDITION_1_MAX_PROB", 0.92),
			Side:    "UP",
		},
		{
			Time:    getEnvInt("CONDITION_2_TIME", 120),
			Diff:    getEnvFloat("CONDITION_2_DIFF", 30),
			DiffBps: getEnvFloat("CONDITION_2_DIFF_BPS", 0),
			MinProb: getEnvFloat("CONDITION_2_MIN_PROB", 0.80),
			MaxProb: getEnvFloat("CONDITION_2_MAX_PROB", 0.92),
			Side:    "DOWN",
		},
		{
			Time:    getEnvInt("CONDITION_3_TIME", 60),
			Diff:    getEnvFloat("CONDITION_3_DIFF", 50),
			DiffBps: getEnvFloat("CONDITION_3_DIFF_BPS", 0),
			MinProb: getEnvFloat("CONDITION_3_MIN_PROB", 0.80),
			MaxProb: getEnvFloat("CONDITION_3_MAX_PROB", 0.92),
			Side:    "UP",
		},
		{
			Time:    getEnvInt("CONDITION_4_TIME", 60),
			Diff:    getEnvFloat("CONDITION_4_DIFF", 50),
			DiffBps: getEnvFloat("CONDITION_4_DIFF_BPS", 0),
			MinProb: getEnvFloat("CONDITION_4_MIN_PROB", 0.80),
			MaxProb: getEnvFloat("CONDITION_4_MAX_PROB", 0.92),
			Side:    "DOWN",
		},
	}
	cfg.MarketTargets = parseMarketTargets(getEnv("POLYMARKET_MARKETS", ""), cfg.MarketSymbol, cfg.MarketIntervalSec)

	if cfg.PriceRefreshSec <= 0 {
		// 防御非法配置，避免轮询协程因为错误输入而停止工作。
		cfg.PriceRefreshSec = 5
	}
	if cfg.MarketIntervalSec <= 0 {
		cfg.MarketIntervalSec = 900
	}
	if cfg.MarketMetaRefreshSec <= 0 {
		cfg.MarketMetaRefreshSec = 5
	}
	if cfg.LoopIntervalSec <= 0 {
		cfg.LoopIntervalSec = 0.25
	}
	if cfg.SignatureType < 0 {
		cfg.SignatureType = 0
	}
	if cfg.AutoRedeemHourLocal < 0 || cfg.AutoRedeemHourLocal > 23 {
		cfg.AutoRedeemHourLocal = 3
	}
	if cfg.AutoRedeemMaxRetry <= 0 {
		cfg.AutoRedeemMaxRetry = 1
	}
	if cfg.AutoRedeemRetryDelay <= 0 {
		cfg.AutoRedeemRetryDelay = 15
	}
	if cfg.AutoRedeemMaxPerRun <= 0 {
		cfg.AutoRedeemMaxPerRun = 5
	}
	if cfg.AutoRedeemReceiptSec <= 0 {
		cfg.AutoRedeemReceiptSec = 90
	}
	if cfg.AutoRedeemMinSize < 0 {
		cfg.AutoRedeemMinSize = 0.01
	}
	if cfg.TakeProfitRetryMax <= 0 {
		cfg.TakeProfitRetryMax = 1
	}
	if cfg.TakeProfitRetryStep < 0 {
		cfg.TakeProfitRetryStep = 0.005
	}
	for idx := range cfg.Conditions {
		if cfg.Conditions[idx].DiffBps < 0 {
			cfg.Conditions[idx].DiffBps = 0
		}
	}
	if strings.TrimSpace(cfg.MarketSymbol) == "" {
		cfg.MarketSymbol = "BTC"
	}
	if strings.TrimSpace(cfg.RelayerURL) == "" {
		cfg.RelayerURL = "https://relayer-v2.polymarket.com"
	}
	if strings.TrimSpace(cfg.MarketSlugPrefix) == "" {
		cfg.MarketSlugPrefix = defaultMarketSlugPrefix(cfg.MarketSymbol)
	}
	if strings.TrimSpace(cfg.MarketSlugInterval) == "" {
		cfg.MarketSlugInterval = defaultMarketSlugInterval(cfg.MarketIntervalSec)
	}
	if strings.TrimSpace(cfg.RTDSSymbol) == "" {
		cfg.RTDSSymbol = defaultRTDSSymbol(cfg.MarketSymbol)
	}
	if strings.TrimSpace(cfg.CryptoPriceSymbol) == "" {
		cfg.CryptoPriceSymbol = cfg.MarketSymbol
	}
	if strings.TrimSpace(cfg.BinanceSymbol) == "" {
		cfg.BinanceSymbol = defaultBinanceSymbol(cfg.MarketSymbol)
	}
	if len(cfg.MarketTargets) == 0 {
		cfg.MarketTargets = []MarketTargetConfig{
			{
				Key:         buildMarketTargetKey(cfg.MarketSymbol, cfg.MarketIntervalSec),
				Label:       buildMarketTargetLabel(cfg.MarketSymbol, cfg.MarketIntervalSec),
				Symbol:      normalizeMarketSymbol(cfg.MarketSymbol),
				IntervalSec: cfg.ResolvedMarketIntervalSec(),
			},
		}
	}

	if absPath, err := filepath.Abs(cfg.StateFile); err == nil {
		cfg.StateFile = absPath
	}
	if absPath, err := filepath.Abs(cfg.DashboardStaticDir); err == nil {
		cfg.DashboardStaticDir = absPath
	}

	return cfg, nil
}

// ResolvedMarketTargets 返回当前配置最终生效的市场 watchlist。
func (c Config) ResolvedMarketTargets() []MarketTargetConfig {
	if len(c.MarketTargets) == 0 {
		return []MarketTargetConfig{
			{
				Key:         buildMarketTargetKey(c.MarketSymbol, c.MarketIntervalSec),
				Label:       buildMarketTargetLabel(c.MarketSymbol, c.MarketIntervalSec),
				Symbol:      normalizeMarketSymbol(c.MarketSymbol),
				IntervalSec: c.ResolvedMarketIntervalSec(),
			},
		}
	}
	out := make([]MarketTargetConfig, 0, len(c.MarketTargets))
	for _, item := range c.MarketTargets {
		normalized := item
		normalized.Symbol = normalizeMarketSymbol(item.Symbol)
		if normalized.IntervalSec <= 0 {
			normalized.IntervalSec = c.ResolvedMarketIntervalSec()
		}
		if strings.TrimSpace(normalized.Key) == "" {
			normalized.Key = buildMarketTargetKey(normalized.Symbol, normalized.IntervalSec)
		}
		if strings.TrimSpace(normalized.Label) == "" {
			normalized.Label = buildMarketTargetLabel(normalized.Symbol, normalized.IntervalSec)
		}
		out = append(out, normalized)
	}
	return out
}

// CloneForTarget 基于基础配置派生一个单市场 worker 配置。
func (c Config) CloneForTarget(target MarketTargetConfig, workerIndex int, enableSharedTasks bool) Config {
	out := c
	out.MarketSymbol = normalizeMarketSymbol(target.Symbol)
	out.MarketIntervalSec = target.IntervalSec
	out.MarketSlugPrefix = defaultMarketSlugPrefix(out.MarketSymbol)
	out.MarketSlugInterval = defaultMarketSlugInterval(out.MarketIntervalSec)
	out.RTDSSymbol = defaultRTDSSymbol(out.MarketSymbol)
	out.CryptoPriceSymbol = out.MarketSymbol
	out.CryptoPriceVariant = defaultCryptoPriceVariant(out.MarketIntervalSec)
	out.BinanceSymbol = defaultBinanceSymbol(out.MarketSymbol)
	// 多市场模式下，如果 BINANCE_WS_URL 只是沿用了基础市场的默认值，
	// 这里清空后让子 worker 按各自 symbol 重新派生 websocket 地址。
	if shouldDeriveBinanceWSURLForTarget(c.BinanceWSURL, c.ResolvedBinanceSymbol()) {
		out.BinanceWSURL = ""
	}
	out.MarketTargets = nil
	out.EnableBalancePolling = enableSharedTasks
	out.EnableAccountPolling = enableSharedTasks
	out.EnableAutoRedeemer = enableSharedTasks

	// 多市场模式下为每个 worker 分配独立状态文件，避免相互覆盖。
	if len(c.ResolvedMarketTargets()) > 1 {
		out.StateFile = targetStateFilePath(c.StateFile, target)
	}
	if absPath, err := filepath.Abs(out.StateFile); err == nil {
		out.StateFile = absPath
	}
	return out
}

// ResolvedMarketSlugPrefix 返回市场 slug 前缀，默认形如 `btc-updown`。
func (c Config) ResolvedMarketSlugPrefix() string {
	if value := strings.TrimSpace(c.MarketSlugPrefix); value != "" {
		return strings.ToLower(value)
	}
	return defaultMarketSlugPrefix(c.MarketSymbol)
}

// ResolvedMarketSlugInterval 返回市场 slug 中的周期标识，默认按秒数换算。
func (c Config) ResolvedMarketSlugInterval() string {
	if value := strings.TrimSpace(c.MarketSlugInterval); value != "" {
		return strings.ToLower(value)
	}
	return defaultMarketSlugInterval(c.MarketIntervalSec)
}

// ResolvedMarketIntervalSec 返回市场周期秒数，并在非法配置时回退到 15 分钟。
func (c Config) ResolvedMarketIntervalSec() int {
	if c.MarketIntervalSec > 0 {
		return c.MarketIntervalSec
	}
	return 900
}

// ResolvedRTDSSymbol 返回 RTDS 订阅用的参考价符号。
func (c Config) ResolvedRTDSSymbol() string {
	if value := strings.TrimSpace(c.RTDSSymbol); value != "" {
		return strings.ToLower(value)
	}
	return defaultRTDSSymbol(c.MarketSymbol)
}

// ResolvedCryptoPriceSymbol 返回 PTB 接口使用的资产符号。
func (c Config) ResolvedCryptoPriceSymbol() string {
	if value := strings.TrimSpace(c.CryptoPriceSymbol); value != "" {
		return strings.ToUpper(value)
	}
	return normalizeMarketSymbol(c.MarketSymbol)
}

// ResolvedCryptoPriceVariant 返回 PTB 接口使用的周期变体；15 分钟默认是 fifteen。
func (c Config) ResolvedCryptoPriceVariant() string {
	if value := strings.TrimSpace(c.CryptoPriceVariant); value != "" {
		return value
	}
	return defaultCryptoPriceVariant(c.MarketIntervalSec)
}

// ResolvedBinanceSymbol 返回 Binance 用于参考价查询的交易对。
func (c Config) ResolvedBinanceSymbol() string {
	if value := strings.TrimSpace(c.BinanceSymbol); value != "" {
		return strings.ToUpper(value)
	}
	return defaultBinanceSymbol(c.MarketSymbol)
}

// ResolvedBinanceWSURL 返回 Binance websocket 地址；未显式配置时按交易对动态拼装。
func (c Config) ResolvedBinanceWSURL() string {
	if value := strings.TrimSpace(c.BinanceWSURL); value != "" {
		return value
	}
	return "wss://stream.binance.com:9443/ws/" + strings.ToLower(c.ResolvedBinanceSymbol()) + "@trade"
}

// shouldDeriveBinanceWSURLForTarget 判断子市场 worker 是否应重新按 symbol 派生 Binance WS 地址。
func shouldDeriveBinanceWSURLForTarget(rawURL string, baseSymbol string) bool {
	normalized := strings.TrimSpace(rawURL)
	if normalized == "" {
		return true
	}
	expected := "wss://stream.binance.com:9443/ws/" + strings.ToLower(strings.TrimSpace(baseSymbol)) + "@trade"
	return strings.EqualFold(normalized, expected)
}

// HasBuilderRelayerAuth 判断是否完整配置了 Builder API 凭证。
func (c Config) HasBuilderRelayerAuth() bool {
	return strings.TrimSpace(c.BuilderAPIKey) != "" &&
		strings.TrimSpace(c.BuilderSecret) != "" &&
		strings.TrimSpace(c.BuilderPassphrase) != ""
}

// HasRelayerAPIKeyAuth 判断是否配置了 Relayer API Key。
func (c Config) HasRelayerAPIKeyAuth() bool {
	return strings.TrimSpace(c.RelayerAPIKey) != ""
}

// HasAnyRelayerAuth 判断是否具备任一种 relayer 鉴权方式。
func (c Config) HasAnyRelayerAuth() bool {
	return c.HasBuilderRelayerAuth() || c.HasRelayerAPIKeyAuth()
}

// normalizeMarketSymbol 把市场资产符号统一成大写形式。
func normalizeMarketSymbol(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return "BTC"
	}
	return normalized
}

// defaultMarketSlugPrefix 根据资产符号生成 Polymarket 常见的 slug 前缀。
func defaultMarketSlugPrefix(symbol string) string {
	return strings.ToLower(normalizeMarketSymbol(symbol)) + "-updown"
}

// defaultMarketSlugInterval 根据秒数生成 slug 周期标记，例如 900 -> 15m。
func defaultMarketSlugInterval(intervalSec int) string {
	if intervalSec <= 0 {
		intervalSec = 900
	}
	if intervalSec%3600 == 0 {
		return fmt.Sprintf("%dh", intervalSec/3600)
	}
	return fmt.Sprintf("%dm", intervalSec/60)
}

// defaultRTDSSymbol 根据资产符号生成 RTDS 默认订阅符号。
func defaultRTDSSymbol(symbol string) string {
	return strings.ToLower(normalizeMarketSymbol(symbol)) + "/usd"
}

// defaultBinanceSymbol 根据资产符号生成 Binance 默认参考交易对。
func defaultBinanceSymbol(symbol string) string {
	return normalizeMarketSymbol(symbol) + "USDT"
}

// defaultCryptoPriceVariant 为常见市场周期提供 PTB 变体默认值。
func defaultCryptoPriceVariant(intervalSec int) string {
	switch intervalSec {
	case 900:
		return "fifteen"
	default:
		return ""
	}
}

// parseMarketTargets 解析形如 `BTC:15m,ETH:15m,SOL:5m` 的市场 watchlist。
func parseMarketTargets(raw string, fallbackSymbol string, fallbackIntervalSec int) []MarketTargetConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	items := strings.Split(raw, ",")
	targets := make([]MarketTargetConfig, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		symbol, intervalSec, ok := parseMarketTarget(strings.TrimSpace(item), fallbackSymbol, fallbackIntervalSec)
		if !ok {
			continue
		}
		key := buildMarketTargetKey(symbol, intervalSec)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, MarketTargetConfig{
			Key:         key,
			Label:       buildMarketTargetLabel(symbol, intervalSec),
			Symbol:      symbol,
			IntervalSec: intervalSec,
		})
	}
	return targets
}

// parseMarketTarget 解析单个 watchlist 项，支持 `BTC:15m`、`ETH:900` 这类格式。
func parseMarketTarget(raw string, fallbackSymbol string, fallbackIntervalSec int) (string, int, bool) {
	if raw == "" {
		return "", 0, false
	}

	symbol := normalizeMarketSymbol(fallbackSymbol)
	intervalSec := fallbackIntervalSec
	parts := strings.Split(raw, ":")
	if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
		symbol = normalizeMarketSymbol(parts[0])
	}
	if len(parts) >= 2 {
		parsedInterval, ok := parseMarketIntervalSpec(parts[1])
		if !ok {
			return "", 0, false
		}
		intervalSec = parsedInterval
	}
	if intervalSec <= 0 {
		intervalSec = 900
	}
	return symbol, intervalSec, true
}

// parseMarketIntervalSpec 把 `5m`、`15m`、`1h` 或纯秒数转成秒。
func parseMarketIntervalSpec(raw string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return 0, false
	}

	if strings.HasSuffix(value, "m") {
		minutes, err := strconv.Atoi(strings.TrimSuffix(value, "m"))
		if err != nil || minutes <= 0 {
			return 0, false
		}
		return minutes * 60, true
	}
	if strings.HasSuffix(value, "h") {
		hours, err := strconv.Atoi(strings.TrimSuffix(value, "h"))
		if err != nil || hours <= 0 {
			return 0, false
		}
		return hours * 3600, true
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

// buildMarketTargetKey 生成稳定的 watchlist 键名，例如 `btc-15m`。
func buildMarketTargetKey(symbol string, intervalSec int) string {
	return strings.ToLower(normalizeMarketSymbol(symbol)) + "-" + defaultMarketSlugInterval(intervalSec)
}

// buildMarketTargetLabel 生成终端与日志中展示的短标签。
func buildMarketTargetLabel(symbol string, intervalSec int) string {
	return normalizeMarketSymbol(symbol) + " " + defaultMarketSlugInterval(intervalSec)
}

// targetStateFilePath 为多市场 worker 派生独立的状态文件路径。
func targetStateFilePath(basePath string, target MarketTargetConfig) string {
	dir := filepath.Dir(basePath)
	ext := filepath.Ext(basePath)
	name := strings.TrimSuffix(filepath.Base(basePath), ext)
	if ext == "" {
		ext = ".json"
	}
	suffix := strings.ReplaceAll(strings.ToLower(target.Key), "-", "_")
	return filepath.Join(dir, name+"_"+suffix+ext)
}

// getFirstEnv 返回多个候选环境变量里第一个非空值。
func getFirstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

// getEnv 读取字符串环境变量；为空时回退到默认值。
func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// getEnvBool 读取布尔环境变量；解析失败时使用默认值。
func getEnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

// getEnvInt 读取整型环境变量；解析失败时使用默认值。
func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

// getEnvInt64 读取 64 位整型环境变量；解析失败时使用默认值。
func getEnvInt64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

// getEnvFloat 读取浮点环境变量；解析失败时使用默认值。
func getEnvFloat(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" || strings.EqualFold(raw, "xx") {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}
