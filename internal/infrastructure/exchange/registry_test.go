package exchange

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"goKit/internal/domain/entity"
)

type stubMarketAdapter struct {
	name string
	cfg  ExchangeConfig
}

func (a stubMarketAdapter) Name() string           { return a.name }
func (a stubMarketAdapter) Enabled() bool          { return a.cfg.Enabled }
func (a stubMarketAdapter) Fees() FeeConfig        { return a.cfg.Fees }
func (a stubMarketAdapter) Config() ExchangeConfig { return a.cfg }
func (a stubMarketAdapter) FetchTradableSymbols(context.Context, string, map[string]struct{}) ([]entity.Symbol, error) {
	return nil, nil
}
func (a stubMarketAdapter) Start(context.Context, MarketSubscriptionProvider, MarketSink) {}

type stubTradeAdapter struct {
	name string
	cfg  ExchangeConfig
}

func (a stubTradeAdapter) Name() string                    { return a.name }
func (a stubTradeAdapter) Enabled() bool                   { return a.cfg.Enabled }
func (a stubTradeAdapter) Capabilities() TradeCapabilities { return TradeCapabilities{} }
func (a stubTradeAdapter) PlaceOrder(context.Context, TradeOrderRequest) (TradeOrderResult, error) {
	return TradeOrderResult{Exchange: a.name}, nil
}
func (a stubTradeAdapter) ClosePosition(context.Context, TradeOrderRequest) (TradeOrderResult, error) {
	return TradeOrderResult{Exchange: a.name}, nil
}
func (a stubTradeAdapter) GetPosition(context.Context, string, string, string) (Position, error) {
	return Position{Exchange: a.name}, nil
}
func (a stubTradeAdapter) GetOrderStatus(context.Context, OrderLookupRequest) (OrderStatus, error) {
	return OrderStatus{Exchange: a.name}, nil
}
func (a stubTradeAdapter) GetAccountSnapshot(context.Context) (AccountSnapshot, error) {
	return AccountSnapshot{Exchange: a.name}, nil
}

func TestConfigSetItemsIncludeAdditionalExchanges(t *testing.T) {
	cfg := ConfigSet{
		Binance: ExchangeConfig{Enabled: true},
		Additional: map[string]ExchangeConfig{
			"Bybit": {Enabled: true, AdapterKind: AdapterKindBybitV5},
		},
	}
	items := cfg.Items()
	if _, ok := items["bybit"]; !ok {
		t.Fatalf("expected additional exchange to be included in items")
	}
}

func TestNormalizeExchangeConfig_DefaultAdapterKindOnlyForBuiltins(t *testing.T) {
	if got := normalizeExchangeConfig("binance", ExchangeConfig{}).AdapterKind; got != AdapterKindBinanceLike {
		t.Fatalf("expected binance default adapter kind %q, got %q", AdapterKindBinanceLike, got)
	}
	if got := normalizeExchangeConfig("aster", ExchangeConfig{}).AdapterKind; got != AdapterKindBinanceLike {
		t.Fatalf("expected aster default adapter kind %q, got %q", AdapterKindBinanceLike, got)
	}
	if got := normalizeExchangeConfig("hyperliquid", ExchangeConfig{}).AdapterKind; got != AdapterKindHyperliquid {
		t.Fatalf("expected hyperliquid default adapter kind %q, got %q", AdapterKindHyperliquid, got)
	}
	if got := normalizeExchangeConfig("bybit", ExchangeConfig{}).AdapterKind; got != "" {
		t.Fatalf("expected custom exchange to keep empty adapter kind until explicitly configured, got %q", got)
	}
}

func TestExchangeConfigAdapterOptionAndAuthExtraEnv(t *testing.T) {
	t.Setenv("OKX_PROJECT_ENV", "project-1")
	cfg := ExchangeConfig{
		AdapterOptions: map[string]string{
			"inst_type": "SWAP",
		},
		Auth: AuthConfig{
			ExtraEnv: map[string]string{
				"project": "OKX_PROJECT_ENV",
			},
		},
	}

	if got := cfg.AdapterOption("inst_type"); got != "SWAP" {
		t.Fatalf("expected adapter option SWAP, got %q", got)
	}
	values := cfg.Auth.ResolveExtraEnv()
	if values["project"] != "project-1" {
		t.Fatalf("expected resolved extra env project-1, got %#v", values)
	}
}

func TestAdapterRegistryBuildMarketsAndTrades(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := ConfigSet{
		Additional: map[string]ExchangeConfig{
			"bybit": {
				Enabled:     true,
				AdapterKind: AdapterKindBybitV5,
				Auth: AuthConfig{
					APIKeyEnv:    "MISSING_KEY",
					APISecretEnv: "MISSING_SECRET",
				},
			},
		},
	}
	registry := defaultAdapterRegistry()
	markets, err := registry.BuildMarkets(cfg, logger)
	if err != nil {
		t.Fatalf("BuildMarkets error = %v", err)
	}
	if len(markets) != 4 {
		t.Fatalf("expected 4 market adapters, got %d", len(markets))
	}
	found := false
	for _, item := range markets {
		if item.Name() == "bybit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dynamic exchange market adapter to be built")
	}

	trades, err := registry.BuildTrades(cfg, logger)
	if err != nil {
		t.Fatalf("BuildTrades error = %v", err)
	}
	if len(trades) != 4 {
		t.Fatalf("expected 4 trade adapters, got %d", len(trades))
	}
	for _, item := range trades {
		if item.Name() == "bybit" && item.Enabled() {
			t.Fatalf("expected bybit trade adapter to stay disabled without credentials")
		}
	}
}

func TestAdapterRegistryRejectsCustomExchangeWithoutExplicitAdapterKind(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := ConfigSet{
		Additional: map[string]ExchangeConfig{
			"bybit": {
				Enabled: true,
			},
		},
	}

	_, err := defaultAdapterRegistry().BuildMarkets(cfg, logger)
	if err == nil {
		t.Fatalf("expected custom exchange without adapter_kind to be rejected")
	}
	if got := err.Error(); got == "" || !containsAll(got, "bybit", "adapter_kind") {
		t.Fatalf("expected explicit adapter_kind error, got %v", err)
	}
}

func TestNewProvidedAdapterRegistry_AllowsExternalProtocolFamilyInjection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	customFactory := AdapterFactory{
		Kind: "okx_v5_swap",
		BuildMarket: func(name string, cfg ExchangeConfig, _ *slog.Logger) MarketAdapter {
			return stubMarketAdapter{name: name, cfg: cfg}
		},
		BuildTrade: func(name string, cfg ExchangeConfig, _ *slog.Logger) TradeAdapter {
			return stubTradeAdapter{name: name, cfg: cfg}
		},
	}

	registry := NewProvidedAdapterRegistry(AdapterRegistryParams{
		Factories: append(DefaultAdapterFactories(), customFactory),
	})
	cfg := ConfigSet{
		Additional: map[string]ExchangeConfig{
			"okx": {
				Enabled:     true,
				AdapterKind: "okx_v5_swap",
			},
		},
	}

	markets, err := registry.BuildMarkets(cfg, logger)
	if err != nil {
		t.Fatalf("expected custom market factory to build successfully, got %v", err)
	}
	trades, err := registry.BuildTrades(cfg, logger)
	if err != nil {
		t.Fatalf("expected custom trade factory to build successfully, got %v", err)
	}

	foundMarket := false
	for _, item := range markets {
		if item.Name() == "okx" {
			foundMarket = true
			break
		}
	}
	if !foundMarket {
		t.Fatalf("expected okx market adapter to be injected from external factory")
	}

	foundTrade := false
	for _, item := range trades {
		if item.Name() == "okx" {
			foundTrade = true
			break
		}
	}
	if !foundTrade {
		t.Fatalf("expected okx trade adapter to be injected from external factory")
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
