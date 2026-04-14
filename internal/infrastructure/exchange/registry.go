package exchange

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.uber.org/fx"
)

type AdapterFactory struct {
	Kind           string
	BuildMarket    func(name string, cfg ExchangeConfig, logger *slog.Logger) MarketAdapter
	BuildTrade     func(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter
	DefaultAliases []string
}

type AdapterRegistry struct {
	factories map[string]AdapterFactory
	aliases   map[string]string
}

func NewAdapterRegistry(factories ...AdapterFactory) *AdapterRegistry {
	r := &AdapterRegistry{
		factories: make(map[string]AdapterFactory, len(factories)),
		aliases:   make(map[string]string),
	}
	for _, factory := range factories {
		kind := normalizeAdapterKind(factory.Kind)
		if kind == "" {
			continue
		}
		factory.Kind = kind
		r.factories[kind] = factory
		r.aliases[kind] = kind
		for _, alias := range factory.DefaultAliases {
			alias = normalizeAdapterKind(alias)
			if alias != "" {
				r.aliases[alias] = kind
			}
		}
	}
	return r
}

// DefaultAdapterFactories 返回仓库当前内置的一组协议族工厂。
//
// 之所以拆成独立函数，而不是只藏在 `defaultAdapterRegistry()` 里，
// 是为了让 fx / 测试 / 后续自定义模块都能在“复用默认工厂”的同时，
// 继续 append 自己的工厂，而不需要回头修改这个文件。
func DefaultAdapterFactories() []AdapterFactory {
	return []AdapterFactory{
		{
			Kind:        AdapterKindBinanceLike,
			BuildMarket: NewBinanceLikeMarketAdapter,
			BuildTrade:  NewBinanceLikeTradeAdapter,
		},
		{
			Kind:        AdapterKindBinanceSpot,
			BuildMarket: NewBinanceSpotMarketAdapter,
			BuildTrade:  NewBinanceSpotTradeAdapter,
		},
		{
			Kind:           AdapterKindBybitV5,
			BuildMarket:    NewBybitV5MarketAdapter,
			BuildTrade:     NewBybitV5TradeAdapter,
			DefaultAliases: []string{"bybit"},
		},
		{
			Kind:           AdapterKindHyperliquid,
			BuildMarket:    NewHyperliquidMarketAdapter,
			BuildTrade:     NewHyperliquidTradeAdapter,
			DefaultAliases: []string{"hl"},
		},
	}
}

// defaultAdapterRegistry 返回仓库当前内置的协议族注册表。
//
// 这里有一个刻意的设计选择：
// - registry 注册的是“真实可复用的协议族”；
// - 而不是 `cex / dex` 这种过于宽泛的场所分类。
//
// 因此当前只内置：
// 1. `binance_like`：Binance Futures 风格协议族；
// 2. `bybit_v5`：Bybit V5 永续合约协议族；
// 3. `hyperliquid`：Hyperliquid 专属协议族。
func defaultAdapterRegistry() *AdapterRegistry {
	return NewAdapterRegistry(DefaultAdapterFactories()...)
}

func normalizeAdapterKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func (r *AdapterRegistry) Resolve(kind string) (AdapterFactory, bool) {
	if r == nil {
		return AdapterFactory{}, false
	}
	if canonical, ok := r.aliases[normalizeAdapterKind(kind)]; ok {
		factory, ok := r.factories[canonical]
		return factory, ok
	}
	return AdapterFactory{}, false
}

func (r *AdapterRegistry) BuildMarkets(cfg ConfigSet, logger *slog.Logger) ([]MarketAdapter, error) {
	items := cfg.Items()
	names := sortedExchangeNames(items)
	out := make([]MarketAdapter, 0, len(names))
	for _, name := range names {
		exCfg, err := normalizeRegistryExchangeConfig(name, items[name])
		if err != nil {
			return nil, err
		}
		factory, ok := r.Resolve(exCfg.AdapterKind)
		if !ok || factory.BuildMarket == nil {
			return nil, fmt.Errorf("exchange %s uses unsupported market adapter kind %q", name, exCfg.AdapterKind)
		}
		out = append(out, factory.BuildMarket(name, exCfg, logger))
	}
	return out, nil
}

func (r *AdapterRegistry) BuildTrades(cfg ConfigSet, logger *slog.Logger) ([]TradeAdapter, error) {
	items := cfg.Items()
	names := sortedExchangeNames(items)
	out := make([]TradeAdapter, 0, len(names))
	for _, name := range names {
		exCfg, err := normalizeRegistryExchangeConfig(name, items[name])
		if err != nil {
			return nil, err
		}
		factory, ok := r.Resolve(exCfg.AdapterKind)
		if !ok || factory.BuildTrade == nil {
			return nil, fmt.Errorf("exchange %s uses unsupported trade adapter kind %q", name, exCfg.AdapterKind)
		}
		out = append(out, factory.BuildTrade(name, exCfg, logger))
	}
	return out, nil
}

func sortedExchangeNames(items map[string]ExchangeConfig) []string {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, strings.ToLower(strings.TrimSpace(name)))
	}
	sort.Strings(names)
	return names
}

// normalizeRegistryExchangeConfig 是 AdapterRegistry 在真正实例化适配器前的最后一道配置收口。
//
// 它和单纯的 normalizeExchangeConfig 不同，额外承担了一个“防误接”的职责：
// - 已知内置交易所可以继续使用默认 adapter_kind；
// - 未知交易所如果没显式写 `adapter_kind`，这里直接报错，而不是偷偷塞进某个默认协议族。
//
// 这样做是为了让接入层的表达和真实能力一致：
// 当前仓库只内置了 `binance_like` 与 `hyperliquid` 两种协议族，
// 现在新增 `bybit_v5` 后，原则保持不变：
// - 只要不是仓库已经明确支持的协议族，就必须显式声明；
// - 不允许因为“名字看起来像 CEX”就自动兜底到某个现成实现。
//
// 因此新交易所必须明确说明“我复用哪一族”，否则就是高风险猜测。
func normalizeRegistryExchangeConfig(name string, cfg ExchangeConfig) (ExchangeConfig, error) {
	originalKind := strings.TrimSpace(cfg.AdapterKind)
	cfg = normalizeExchangeConfig(name, cfg)
	if strings.TrimSpace(cfg.AdapterKind) == "" {
		return ExchangeConfig{}, fmt.Errorf(
			"exchange %s must explicitly configure adapter_kind; built-in adapter kinds are %q, %q, %q and %q",
			name,
			AdapterKindBinanceLike,
			AdapterKindBinanceSpot,
			AdapterKindBybitV5,
			AdapterKindHyperliquid,
		)
	}
	if originalKind == "" && normalizeExchangeName(name) != "" && normalizeExchangeName(name) != "binance" && normalizeExchangeName(name) != "aster" && normalizeExchangeName(name) != "hyperliquid" {
		return ExchangeConfig{}, fmt.Errorf(
			"exchange %s must explicitly configure adapter_kind; no default adapter family is assumed for custom exchanges",
			name,
		)
	}
	return cfg, nil
}

func normalizeExchangeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

type AdapterRegistryParams struct {
	fx.In

	Factories []AdapterFactory `group:"exchange_adapter_factories"`
}

// NewProvidedAdapterRegistry 用于 fx 场景下组装运行时注册表。
//
// 这样后续如果某个新模块想提供 `bybit_v5` 或 `okx_v5_swap` 协议族，
// 它只需要：
// 1. 实现自己的 `AdapterFactory`
// 2. 通过同一个 fx group 注入进来
//
// 而不必再修改中心化的 `defaultAdapterRegistry()`。
func NewProvidedAdapterRegistry(p AdapterRegistryParams) *AdapterRegistry {
	if len(p.Factories) == 0 {
		return defaultAdapterRegistry()
	}
	return NewAdapterRegistry(p.Factories...)
}

func ProvideMarketAdapters(registry *AdapterRegistry, cfg ConfigSet, logger *slog.Logger) ([]MarketAdapter, error) {
	if registry == nil {
		registry = defaultAdapterRegistry()
	}
	return registry.BuildMarkets(cfg, logger)
}

func ProvideTradeAdapters(registry *AdapterRegistry, cfg ConfigSet, logger *slog.Logger) ([]TradeAdapter, error) {
	if registry == nil {
		registry = defaultAdapterRegistry()
	}
	return registry.BuildTrades(cfg, logger)
}
