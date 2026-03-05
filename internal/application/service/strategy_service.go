package service

import (
	"context"
	"fmt"

	"goKit/pkg/kit/db"
)

// StrategyService 策略服务，负责从清洗好的基础数据中挖掘交易信号
type StrategyService struct {
	dbClient *db.Client
}

// NewStrategyService 构造函数
func NewStrategyService(dbClient *db.Client) *StrategyService {
	return &StrategyService{
		dbClient: dbClient,
	}
}

// =========================================================================
// 策略 1：寻找“国家队”高控盘重仓股
// 逻辑：扫描最新一期财报，汇总单只股票背后所有“国家队”机构的持股比例，寻找底仓雄厚的标的
// =========================================================================

// GetNationalTeamHeavyHoldings 获取国家队重仓股列表
func (s *StrategyService) GetNationalTeamHeavyHoldings(ctx context.Context, minRatio float64, limit int) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	// 使用严格对应 entity 定义的表名进行关联查询
	err := s.dbClient.GetDB(ctx).Table("stock_holding_record r").
		Select(`
			r.stock_code,
			i.stock_name,
			i.industry,
			SUM(r.hold_ratio) as total_ratio,
			SUM(r.hold_count) as total_shares
		`).
		Joins("INNER JOIN stock_info i ON r.stock_code = i.stock_code").
		Joins("INNER JOIN institution_info inst ON r.inst_id = inst.inst_id").
		Where("inst.inst_type = ?", "国家队").
		Group("r.stock_code, i.stock_name, i.industry").
		Having("SUM(r.hold_ratio) >= ?", minRatio).
		Order("total_ratio DESC").
		Limit(limit).
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("查询国家队重仓股失败: %v", err)
	}

	return results, nil
}

// =========================================================================
// 策略 2：“黄金坑”策略 (国家队护盘 + 股价处于近期低位)
// 逻辑：在国家队重仓（持股 > X%）的前提下，结合腾讯的 K 线数据，
// 寻找当前最新收盘价接近近 250 天最低价（破净或错杀）的标的。
// =========================================================================

// GetNationalTeamGoldenPit 寻找国家队重仓且股价处于低位的“黄金坑”
func (s *StrategyService) GetNationalTeamGoldenPit(ctx context.Context, minRatio float64) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	// 这是一个复杂的量化联表分析 SQL：
	// 1. 算出每只股票的国家队总持仓比例 (CTE 或嵌套)
	// 2. 算出每只股票的近期最低价和最新价
	// 3. 筛选出价格接近最低价的标的
	sql := `
		WITH TeamHoldings AS (
			SELECT
				r.stock_code,
				SUM(r.hold_ratio) as total_ratio
			FROM stock_holding_record r
			INNER JOIN institution_info inst ON r.inst_id = inst.inst_id
			WHERE inst.inst_type = '国家队'
			GROUP BY r.stock_code
			HAVING SUM(r.hold_ratio) >= ?
		),
		PriceStats AS (
			SELECT
				stock_code,
				MIN(low) as period_low,
				MAX(high) as period_high,
				-- 提取最近一天的收盘价作为最新价
				(SUBSTRING_INDEX(GROUP_CONCAT(close ORDER BY trade_date DESC), ',', 1) + 0.0) as latest_price
			FROM stock_daily_quote
			GROUP BY stock_code
		)
		SELECT
			th.stock_code,
			i.stock_name,
			i.industry,
			th.total_ratio,
			ps.latest_price,
			ps.period_low,
			-- 计算当前价格距离最低价的偏离度：(最新价 - 最低价) / 最低价
			ROUND((ps.latest_price - ps.period_low) / ps.period_low * 100, 2) as bounce_from_bottom_pct
		FROM TeamHoldings th
		INNER JOIN stock_info i ON th.stock_code = i.stock_code
		INNER JOIN PriceStats ps ON th.stock_code = ps.stock_code
		-- 筛选条件：当前价格距离近期最低价反弹不超过 10% (真正的黄金坑)
		WHERE ROUND((ps.latest_price - ps.period_low) / ps.period_low * 100, 2) <= 10.00
		ORDER BY th.total_ratio DESC
	`

	err := s.dbClient.GetDB(ctx).Raw(sql, minRatio).Scan(&results).Error
	if err != nil {
		return nil, fmt.Errorf("执行黄金坑策略分析失败: %v", err)
	}

	return results, nil
}
