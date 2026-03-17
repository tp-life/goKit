package entity

type ExecutionRecord struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PlanKey            string `gorm:"uniqueIndex;size:160" json:"plan_key"`
	BatchID            string `gorm:"size:64;index" json:"batch_id"`
	OpportunityBatchID string `gorm:"size:64;index" json:"opportunity_batch_id"`
	Symbol             string `gorm:"size:64;index" json:"symbol"`
	LongExchange       string `gorm:"size:32" json:"long_exchange"`
	ShortExchange      string `gorm:"size:32" json:"short_exchange"`
	Status             string `gorm:"size:32;index" json:"status"`
	LiveTrading        bool   `json:"live_trading"`
	AutoClose          bool   `json:"auto_close"`
	TargetCloseTimeMs  int64  `json:"target_close_time_ms"`
	OpenedAtMs         int64  `json:"opened_at_ms"`
	ClosedAtMs         int64  `json:"closed_at_ms"`
	LastError          string `gorm:"type:text" json:"last_error,omitempty"`
	OpenOrderCount     int    `json:"open_order_count"`
	CloseOrderCount    int    `json:"close_order_count"`
	TimestampModel
}

func (ExecutionRecord) TableName() string { return "execution_records" }
