package entity

type OrderRecord struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PlanKey         string  `gorm:"index;size:160" json:"plan_key"`
	ExecutionStatus string  `gorm:"size:32;index" json:"execution_status"`
	Phase           string  `gorm:"size:16;index" json:"phase"`
	LegRole         string  `gorm:"size:32;index" json:"leg_role"`
	Exchange        string  `gorm:"size:32;index" json:"exchange"`
	Symbol          string  `gorm:"size:64;index" json:"symbol"`
	VenueSymbol     string  `gorm:"size:64" json:"venue_symbol"`
	ClientOrderID   string  `gorm:"size:128;index" json:"client_order_id"`
	VenueOrderID    string  `gorm:"size:128;index" json:"venue_order_id"`
	Side            string  `gorm:"size:16" json:"side"`
	OrderType       string  `gorm:"size:32" json:"order_type"`
	TimeInForce     string  `gorm:"size:16" json:"time_in_force"`
	ReduceOnly      bool    `json:"reduce_only"`
	RequestedQty    float64 `json:"requested_qty"`
	RequestedPrice  float64 `json:"requested_price"`
	ExecutedQty     float64 `json:"executed_qty"`
	AvgPrice        float64 `json:"avg_price"`
	Status          string  `gorm:"size:32;index" json:"status"`
	RawResponse     string  `gorm:"type:text" json:"raw_response,omitempty"`
	ErrorMessage    string  `gorm:"type:text" json:"error_message,omitempty"`
	TimestampModel
}

func (OrderRecord) TableName() string { return "order_records" }
