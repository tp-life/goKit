package entity

type BookTopSnapshot struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Exchange string `gorm:"index:idx_book_symbol_exchange_time,priority:1;size:32" json:"exchange"`
	Symbol   string `gorm:"index:idx_book_symbol_exchange_time,priority:2;size:64" json:"symbol"`

	VenueSymbol string `gorm:"size:64" json:"venue_symbol"`

	BidPrice    float64 `json:"bid_price"`
	BidQty      float64 `json:"bid_qty"`
	AskPrice    float64 `json:"ask_price"`
	AskQty      float64 `json:"ask_qty"`
	EventTimeMs int64   `gorm:"index:idx_book_symbol_exchange_time,priority:3" json:"event_time_ms"`
	TimestampModel
}

func (BookTopSnapshot) TableName() string { return "book_top_snapshots" }
