package exchange

import "testing"

func TestResolveBinanceLikeTradeRESTBaseURL_PortfolioMarginUpgradesDefaultHost(t *testing.T) {
	cfg := ExchangeConfig{
		RestBaseURL: binanceDefaultFuturesRESTBaseURL,
		AdapterOptions: map[string]string{
			"account_mode": binanceLikeAccountModePortfolioMargin,
		},
	}

	if got := resolveBinanceLikeTradeRESTBaseURL("binance", cfg); got != binancePortfolioMarginRESTBaseURL {
		t.Fatalf("expected PM REST base %q, got %q", binancePortfolioMarginRESTBaseURL, got)
	}
}

func TestResolveBinanceLikeTradePrivateWSBaseURL_PortfolioMarginUpgradesDefaultHost(t *testing.T) {
	cfg := ExchangeConfig{
		PublicWSBaseURL:  binanceDefaultFuturesPrivateWSBaseURL,
		PrivateWSBaseURL: binanceDefaultFuturesPrivateWSBaseURL,
		AdapterOptions: map[string]string{
			"account_mode": binanceLikeAccountModePortfolioMargin,
		},
	}

	if got := resolveBinanceLikeTradePrivateWSBaseURL("binance", cfg); got != binancePortfolioMarginPrivateWSBaseURL {
		t.Fatalf("expected PM private WS base %q, got %q", binancePortfolioMarginPrivateWSBaseURL, got)
	}
}

func TestResolveBinanceLikePrivateRoutes_PortfolioMarginUsesPapiUMPaths(t *testing.T) {
	routes := resolveBinanceLikePrivateRoutes("binance", ExchangeConfig{
		AdapterOptions: map[string]string{
			"account_mode": binanceLikeAccountModePortfolioMargin,
		},
	}, binanceLikeTradeAuthLegacyHMAC)

	if routes.orderPath != "/papi/v1/um/order" {
		t.Fatalf("unexpected PM order path %q", routes.orderPath)
	}
	if routes.positionPath != "/papi/v1/um/positionRisk" {
		t.Fatalf("unexpected PM position path %q", routes.positionPath)
	}
	if routes.accountPath != "/papi/v1/account" {
		t.Fatalf("unexpected PM account path %q", routes.accountPath)
	}
	if routes.positionModePath != "/papi/v1/um/positionSide/dual" {
		t.Fatalf("unexpected PM position mode path %q", routes.positionModePath)
	}
	if routes.listenKeyPath != "/papi/v1/listenKey" {
		t.Fatalf("unexpected PM listen key path %q", routes.listenKeyPath)
	}
}
