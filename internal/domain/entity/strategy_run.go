package entity

type StrategyRun struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	RunID          string `gorm:"uniqueIndex" json:"run_id"`
	Watchlist      string `json:"watchlist"`
	ConfigSnapshot string `json:"config_snapshot"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	TimestampModel
}

func (StrategyRun) TableName() string { return "strategy_runs" }
