package entity

type ExecutionRecord struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PlanKey               string  `gorm:"uniqueIndex;size:160" json:"plan_key"`
	BatchID               string  `gorm:"size:64;index" json:"batch_id"`
	OpportunityBatchID    string  `gorm:"size:64;index" json:"opportunity_batch_id"`
	RollingGroupKey       string  `gorm:"size:160;index" json:"rolling_group_key"`
	Symbol                string  `gorm:"size:64;index" json:"symbol"`
	LongExchange          string  `gorm:"size:32" json:"long_exchange"`
	ShortExchange         string  `gorm:"size:32" json:"short_exchange"`
	StrategyMode          string  `gorm:"size:64" json:"strategy_mode"`
	Status                string  `gorm:"size:32;index" json:"status"`
	LiveTrading           bool    `json:"live_trading"`
	AutoClose             bool    `json:"auto_close"`
	AllocatedNotionalUSDT float64 `json:"allocated_notional_usdt"`
	TargetCloseTimeMs     int64   `json:"target_close_time_ms"`
	NextReviewTimeMs      int64   `json:"next_review_time_ms"`
	CurrentSyncBoundaryMs int64   `json:"current_sync_boundary_ms"`
	OpenedAtMs            int64   `json:"opened_at_ms"`
	ClosedAtMs            int64   `json:"closed_at_ms"`
	ReviewCount           int     `json:"review_count"`
	LastReviewAtMs        int64   `json:"last_review_at_ms"`
	LastReviewReason      string  `gorm:"type:text" json:"last_review_reason,omitempty"`
	PredecessorPlanKey    string  `gorm:"size:160" json:"predecessor_plan_key,omitempty"`
	SuccessorPlanKey      string  `gorm:"size:160" json:"successor_plan_key,omitempty"`

	// LastTransitionAtMs 记录最近一次 execution 状态迁移发生的时间。
	// 后续若引入 websocket 事件或事件回放，这个字段可以帮助快速定位
	// “record 最近一次是何时被推进到当前状态的”。
	LastTransitionAtMs int64 `json:"last_transition_at_ms"`

	// LastTransitionEvent 记录最近一次驱动状态变化的事件名，
	// 例如 `open_requested` / `open_results_applied` / `open_circuit_blocked`。
	// 它是 execution 状态机与未来事件驱动模型之间的一层轻量连接点。
	LastTransitionEvent string `gorm:"size:64" json:"last_transition_event,omitempty"`

	// StatusReason 保存最近一次状态变化的人类可读摘要，
	// 用于排障、前端展示和后续状态回放时快速理解“为什么变成这样”。
	StatusReason    string `gorm:"type:text" json:"status_reason,omitempty"`
	LastError       string `gorm:"type:text" json:"last_error,omitempty"`
	OpenOrderCount  int    `json:"open_order_count"`
	CloseOrderCount int    `json:"close_order_count"`
	TimestampModel
}

func (ExecutionRecord) TableName() string { return "execution_records" }
