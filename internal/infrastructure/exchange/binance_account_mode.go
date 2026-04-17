package exchange

import "strings"

const (
	binanceLikeAccountModeOption           = "account_mode"
	binanceLikeAccountModeStandardFutures  = "standard_futures"
	binanceLikeAccountModePortfolioMargin  = "portfolio_margin"
	binanceDefaultFuturesRESTBaseURL       = "https://fapi.binance.com"
	binancePortfolioMarginRESTBaseURL      = "https://papi.binance.com"
	binanceDefaultFuturesPrivateWSBaseURL  = "wss://fstream.binance.com"
	binancePortfolioMarginPrivateWSBaseURL = "wss://fstream.binance.com/pm"
	asterDefaultFuturesRESTBaseURL         = "https://fapi.asterdex.com"
	asterDefaultFuturesPrivateWSBaseURL    = "wss://fstream.asterdex.com"
)

type binanceLikePrivateRoutes struct {
	orderPath        string
	positionPath     string
	accountPath      string
	positionModePath string
	listenKeyPath    string
}

// binanceLikeAccountMode 返回 Binance-like 私有交易链路应使用的账户模式。
//
// 当前只有 Binance 会消费这个选项；Aster 等其他复用同一协议族的交易所仍保持原来的
// standard futures 语义，避免把“Binance 的账户模型”误投射到整个 family。
func binanceLikeAccountMode(name string, cfg ExchangeConfig) string {
	if normalizeExchangeName(name) != "binance" {
		return binanceLikeAccountModeStandardFutures
	}

	switch strings.ToLower(strings.TrimSpace(cfg.AdapterOption(binanceLikeAccountModeOption))) {
	case "", "standard", "futures", "standard_futures", "um_futures":
		return binanceLikeAccountModeStandardFutures
	case "portfolio_margin", "portfolio-margin", "portfolio", "pm", "unified", "unified_account", "unified-account":
		return binanceLikeAccountModePortfolioMargin
	default:
		return binanceLikeAccountModeStandardFutures
	}
}

func resolveBinanceLikeTradeRESTBaseURL(name string, cfg ExchangeConfig) string {
	restBaseURL := strings.TrimSpace(cfg.RestBaseURL)
	switch normalizeExchangeName(name) {
	case "binance":
		if binanceLikeAccountMode(name, cfg) == binanceLikeAccountModePortfolioMargin {
			if restBaseURL == "" || normalizeBaseURL(restBaseURL) == normalizeBaseURL(binanceDefaultFuturesRESTBaseURL) {
				return binancePortfolioMarginRESTBaseURL
			}
			return restBaseURL
		}
		if restBaseURL != "" {
			return restBaseURL
		}
		return binanceDefaultFuturesRESTBaseURL
	case "aster":
		if restBaseURL != "" {
			return restBaseURL
		}
		return asterDefaultFuturesRESTBaseURL
	default:
		if restBaseURL != "" {
			return restBaseURL
		}
		return binanceDefaultFuturesRESTBaseURL
	}
}

func resolveBinanceLikeTradePrivateWSBaseURL(name string, cfg ExchangeConfig) string {
	privateWSBaseURL := strings.TrimSpace(cfg.PrivateWSBaseURL)
	publicWSBaseURL := strings.TrimSpace(cfg.PublicWSBaseURL)

	switch normalizeExchangeName(name) {
	case "binance":
		if binanceLikeAccountMode(name, cfg) == binanceLikeAccountModePortfolioMargin {
			if privateWSBaseURL == "" || normalizeBaseURL(privateWSBaseURL) == normalizeBaseURL(binanceDefaultFuturesPrivateWSBaseURL) {
				return binancePortfolioMarginPrivateWSBaseURL
			}
			return privateWSBaseURL
		}
		if privateWSBaseURL != "" {
			return privateWSBaseURL
		}
		if publicWSBaseURL != "" {
			return publicWSBaseURL
		}
		return binanceDefaultFuturesPrivateWSBaseURL
	case "aster":
		if privateWSBaseURL != "" {
			return privateWSBaseURL
		}
		if publicWSBaseURL != "" {
			return publicWSBaseURL
		}
		return asterDefaultFuturesPrivateWSBaseURL
	default:
		if privateWSBaseURL != "" {
			return privateWSBaseURL
		}
		if publicWSBaseURL != "" {
			return publicWSBaseURL
		}
		return binanceDefaultFuturesPrivateWSBaseURL
	}
}

func resolveBinanceLikePrivateRoutes(name string, cfg ExchangeConfig, authMode string) binanceLikePrivateRoutes {
	routes := binanceLikePrivateRoutes{
		orderPath:        "/fapi/v1/order",
		positionPath:     "/fapi/v2/positionRisk",
		accountPath:      "/fapi/v2/account",
		positionModePath: "/fapi/v1/positionSide/dual",
		listenKeyPath:    "/fapi/v1/listenKey",
	}

	switch normalizeExchangeName(name) {
	case "binance":
		if binanceLikeAccountMode(name, cfg) == binanceLikeAccountModePortfolioMargin {
			return binanceLikePrivateRoutes{
				orderPath:        "/papi/v1/um/order",
				positionPath:     "/papi/v1/um/positionRisk",
				accountPath:      "/papi/v1/account",
				positionModePath: "/papi/v1/um/positionSide/dual",
				listenKeyPath:    "/papi/v1/listenKey",
			}
		}
		routes.positionPath = "/fapi/v3/positionRisk"
		routes.accountPath = "/fapi/v3/account"
	case "aster":
		if authMode == asterTradeAuthV3Signer {
			routes.orderPath = "/fapi/v3/order"
			routes.positionPath = "/fapi/v3/positionRisk"
			routes.accountPath = "/fapi/v3/account"
		} else {
			routes.accountPath = "/fapi/v4/account"
		}
	}

	return routes
}

func normalizeBaseURL(value string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(value)), "/")
}
