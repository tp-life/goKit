package entity

import (
	"encoding/json"
	"strings"
)

type Symbol struct {
	ID uint `gorm:"primaryKey" json:"id"`

	// Exchange 表示交易所名称，例如 binance / aster / hyperliquid。
	Exchange string `gorm:"index:idx_symbol_exchange_symbol,unique;size:32" json:"exchange"`

	// Symbol 统一保存“规范化交易对标识”，当前策略里使用基础币作为 canonical symbol，
	// 例如 Binance 的 BTCUSDT、Hyperliquid 的 BTC 都会映射成 BTC。
	Symbol string `gorm:"index:idx_symbol_exchange_symbol,unique;index:idx_symbol_name;size:64" json:"symbol"`

	// VenueSymbol 保存交易所原始交易对名称。
	VenueSymbol string `gorm:"size:64" json:"venue_symbol"`

	BaseAsset   string `gorm:"size:32" json:"base_asset"`
	QuoteAsset  string `gorm:"size:32" json:"quote_asset"`
	SettleAsset string `gorm:"size:32" json:"settle_asset"`

	Status       string `gorm:"size:32" json:"status"`
	ContractType string `gorm:"size:32" json:"contract_type"`

	TickSize    string `gorm:"size:64" json:"tick_size"`
	StepSize    string `gorm:"size:64" json:"step_size"`
	MinQty      string `gorm:"size:64" json:"min_qty"`
	MinNotional string `gorm:"size:64" json:"min_notional"`

	FundingIntervalHours int    `json:"funding_interval_hours"`
	VenueAssetID         string `gorm:"size:64" json:"venue_asset_id"`
	ExtraMetaJSON        string `gorm:"type:text" json:"extra_meta_json,omitempty"`

	Enabled bool `gorm:"default:true" json:"enabled"`
	Watched bool `gorm:"default:false" json:"watched"`
	TimestampModel
}

func (Symbol) TableName() string { return "symbols" }

// SymbolExecutionMeta 保存执行阶段会用到、但不适合展开成顶层列的交易所细粒度约束。
//
// 当前主要用于解决：
// - 某些 venue 的 LIMIT / MARKET 单，单笔允许的 maxQty 并不相同；
// - 策略层如果不知道这个上限，就可能一次下出“总仓位数量”，直接被交易所拒绝。
type SymbolExecutionMeta struct {
	LimitMaxQty  float64 `json:"limit_max_qty,omitempty"`
	MarketMaxQty float64 `json:"market_max_qty,omitempty"`
}

func (s Symbol) ExecutionMeta() SymbolExecutionMeta {
	if strings.TrimSpace(s.ExtraMetaJSON) == "" {
		return SymbolExecutionMeta{}
	}
	var meta SymbolExecutionMeta
	if err := json.Unmarshal([]byte(s.ExtraMetaJSON), &meta); err != nil {
		return SymbolExecutionMeta{}
	}
	return meta
}

func (s *Symbol) SetExecutionMeta(meta SymbolExecutionMeta) error {
	if s == nil {
		return nil
	}
	if meta.LimitMaxQty <= 0 && meta.MarketMaxQty <= 0 {
		s.ExtraMetaJSON = ""
		return nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	s.ExtraMetaJSON = string(raw)
	return nil
}

func (m SymbolExecutionMeta) MaxQtyForOrderType(orderType string) float64 {
	if strings.EqualFold(strings.TrimSpace(orderType), "MARKET") && m.MarketMaxQty > 0 {
		return m.MarketMaxQty
	}
	if m.LimitMaxQty > 0 {
		return m.LimitMaxQty
	}
	return m.MarketMaxQty
}
