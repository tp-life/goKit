package entity

type StrategyRun struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	RunID          string `gorm:"uniqueIndex;size:160" json:"run_id"`
	Watchlist      string `gorm:"type:text" json:"watchlist"`
	ConfigSnapshot string `gorm:"type:text" json:"config_snapshot"`
	Status         string `gorm:"size:32" json:"status"`
	Message        string `gorm:"type:text" json:"message"`
	TimestampModel
}

func (StrategyRun) TableName() string { return "strategy_runs" }
