package exchange

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	bybitDefaultCategory      = "linear"
	bybitDefaultAccountType   = "UNIFIED"
	bybitDefaultRecvWindow    = "5000"
	bybitDefaultPollInterval  = 3 * time.Second
	bybitMinPollInterval      = 500 * time.Millisecond
	bybitOptionCategoryKey    = "category"
	bybitOptionAccountTypeKey = "account_type"
	bybitOptionRecvWindowKey  = "recv_window_ms"
	bybitOptionPositionIdxKey = "position_idx"
	bybitOptionPollMSKey      = "ticker_poll_interval_ms"
)

func bybitPublicMarketWSBaseURL(cfg ExchangeConfig) string {
	switch bybitCategory(cfg) {
	case "spot":
		return "wss://stream.bybit.com/v5/public/spot"
	case "inverse":
		return "wss://stream.bybit.com/v5/public/inverse"
	case "option":
		return "wss://stream.bybit.com/v5/public/option"
	default:
		return "wss://stream.bybit.com/v5/public/linear"
	}
}

// bybitEnvelope 对应 Bybit V5 REST 通用返回包裹。
//
// Bybit 和很多“HTTP status != business status”的交易所一样：
// - HTTP 200 只代表请求成功到达服务端；
// - 真正的业务成败要看 `retCode / retMsg`。
//
// 因此适配器层需要先拆 envelope，再把 `result` 交给上层具体接口解析。
type bybitEnvelope struct {
	RetCode int             `json:"retCode"`
	RetMsg  string          `json:"retMsg"`
	Result  json.RawMessage `json:"result"`
	Time    int64           `json:"time"`
}

func bybitCategory(cfg ExchangeConfig) string {
	return strings.ToLower(firstNonEmpty(cfg.AdapterOption(bybitOptionCategoryKey), bybitDefaultCategory))
}

func bybitAccountType(cfg ExchangeConfig) string {
	return strings.ToUpper(firstNonEmpty(cfg.AdapterOption(bybitOptionAccountTypeKey), bybitDefaultAccountType))
}

func bybitRecvWindow(cfg ExchangeConfig) string {
	value := strings.TrimSpace(cfg.AdapterOption(bybitOptionRecvWindowKey))
	if value == "" {
		return bybitDefaultRecvWindow
	}
	return value
}

func bybitPositionIdx(cfg ExchangeConfig) int {
	value := strings.TrimSpace(cfg.AdapterOption(bybitOptionPositionIdxKey))
	if value == "" {
		return 0
	}
	idx, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return idx
}

func bybitPollInterval(cfg ExchangeConfig) time.Duration {
	value := strings.TrimSpace(cfg.AdapterOption(bybitOptionPollMSKey))
	if value == "" {
		return bybitDefaultPollInterval
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms <= 0 {
		return bybitDefaultPollInterval
	}
	interval := time.Duration(ms) * time.Millisecond
	if interval < bybitMinPollInterval {
		return bybitMinPollInterval
	}
	return interval
}

func decodeBybitEnvelope(body []byte, out any) (bybitEnvelope, error) {
	var envelope bybitEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return bybitEnvelope{}, err
	}
	if envelope.RetCode != 0 {
		return envelope, fmt.Errorf("retCode=%d retMsg=%s", envelope.RetCode, strings.TrimSpace(envelope.RetMsg))
	}
	if out != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, out); err != nil {
			return envelope, err
		}
	}
	return envelope, nil
}

func bybitSide(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "SELL":
		return "Sell"
	default:
		return "Buy"
	}
}

func bybitOrderType(orderType string) string {
	switch strings.ToUpper(strings.TrimSpace(orderType)) {
	case "LIMIT":
		return "Limit"
	default:
		return "Market"
	}
}

func bybitTimeInForce(tif, fallback string) string {
	switch strings.ToUpper(strings.TrimSpace(firstNonEmpty(tif, fallback))) {
	case "IOC":
		return "IOC"
	case "FOK":
		return "FOK"
	case "POSTONLY", "GTX":
		return "PostOnly"
	default:
		return "GTC"
	}
}

func bybitFundingIntervalHours(minutes int, fallback int) int {
	if minutes <= 0 {
		return fallback
	}
	hours := minutes / 60
	if hours <= 0 {
		return fallback
	}
	return hours
}

// bybitNormalizeOrderStatus 把 Bybit 的 mixedCase 订单状态归一成系统内部更稳定的形态。
//
// 这样做的目的不是追求“绝对统一所有交易所状态机”，而是至少保证：
// - 上层日志更容易横向阅读；
// - 执行状态聚合逻辑不需要了解每家交易所的原始大小写风格；
// - terminal / canceled 判断可以基于统一 token 完成。
func bybitNormalizeOrderStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "New":
		return "NEW"
	case "PartiallyFilled":
		return "PARTIALLY_FILLED"
	case "Filled":
		return "FILLED"
	case "Cancelled":
		return "CANCELED"
	case "Rejected":
		return "REJECTED"
	case "PartiallyFilledCanceled":
		return "PARTIALLY_FILLED_CANCELED"
	case "Untriggered":
		return "UNTRIGGERED"
	case "Triggered":
		return "TRIGGERED"
	case "Deactivated":
		return "DEACTIVATED"
	default:
		normalized := strings.ToUpper(strings.TrimSpace(status))
		normalized = strings.ReplaceAll(normalized, " ", "_")
		return normalized
	}
}

func bybitIsTerminalOrderStatus(status string) bool {
	switch bybitNormalizeOrderStatus(status) {
	case "FILLED", "CANCELED", "REJECTED", "PARTIALLY_FILLED_CANCELED", "DEACTIVATED", "NO_POSITION":
		return true
	default:
		return false
	}
}
