package entity

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
