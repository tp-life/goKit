package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

// autoTradeRiskRequest 表示单市场 worker 在自动开仓前提交给全局风控层的一次检查请求。
type autoTradeRiskRequest struct {
	MarketSlug   string
	MarketKey    string
	Side         string
	TradeAmount  float64
	CurrentPrice float64
	WindowSec    int
	StrategyKey  string
}

type autoTradeRiskGuard func(req autoTradeRiskRequest) error

type autoTradeSizeRequest struct {
	MarketSlug      string
	MarketKey       string
	Side            string
	BaseTradeAmount float64
	WindowSec       int
	StrategyKey     string
}

type autoTradeSizeAllocator func(req autoTradeSizeRequest) float64

type globalExposure struct {
	openMarkets   int
	openNotional  float64
	sameSideCount int
}

// SetAutoTradeRiskGuard 为单市场 worker 注入一个全局风控闸门。
func (s *PolymarketService) SetAutoTradeRiskGuard(guard autoTradeRiskGuard) {
	s.autoTradeGuard = guard
}

// SetAutoTradeSizeAllocator 为单市场 worker 注入自动仓位调整器。
func (s *PolymarketService) SetAutoTradeSizeAllocator(allocator autoTradeSizeAllocator) {
	s.autoTradeSizer = allocator
}

// checkAutoTradeRisk 在 manager 层统一执行跨市场的仓位与连亏风控。
func (m *PolymarketManager) checkAutoTradeRisk(req autoTradeRiskRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.refreshLossCooldownLocked(now)
	if !m.lossCooldownUntil.IsZero() && now.Before(m.lossCooldownUntil) {
		reason := fmt.Sprintf("连续亏损停机中，恢复时间 %s", m.lossCooldownUntil.Format("2006-01-02 15:04:05"))
		m.snapshot.GlobalRisk = m.buildGlobalRiskStatusLocked(reason, req.Side)
		return errors.New(reason)
	}

	exposure := m.currentGlobalExposureLocked(strings.ToUpper(strings.TrimSpace(req.Side)))
	if m.cfg.MaxConcurrentMarkets > 0 && exposure.openMarkets >= m.cfg.MaxConcurrentMarkets {
		reason := fmt.Sprintf("全局风控限制：当前已有 %d 个市场持仓/挂单，达到上限 %d", exposure.openMarkets, m.cfg.MaxConcurrentMarkets)
		m.snapshot.GlobalRisk = m.buildGlobalRiskStatusLocked(reason, req.Side)
		return errors.New(reason)
	}
	if m.cfg.MaxTotalOpenNotional > 0 && exposure.openNotional+req.TradeAmount > m.cfg.MaxTotalOpenNotional {
		reason := fmt.Sprintf(
			"全局风控限制：当前总敞口 %.4f USDC，加上本次 %.4f 后超过上限 %.4f",
			exposure.openNotional,
			req.TradeAmount,
			m.cfg.MaxTotalOpenNotional,
		)
		m.snapshot.GlobalRisk = m.buildGlobalRiskStatusLocked(reason, req.Side)
		return errors.New(reason)
	}
	if m.cfg.MaxSameSideMarkets > 0 && exposure.sameSideCount >= m.cfg.MaxSameSideMarkets {
		reason := fmt.Sprintf("全局风控限制：%s 方向当前已有 %d 个市场暴露，达到上限 %d", req.Side, exposure.sameSideCount, m.cfg.MaxSameSideMarkets)
		m.snapshot.GlobalRisk = m.buildGlobalRiskStatusLocked(reason, req.Side)
		return errors.New(reason)
	}
	if m.cfg.AutoDisableNegative {
		if reason := m.negativeStrategyBlockReasonLocked(req); reason != "" {
			m.snapshot.GlobalRisk = m.buildGlobalRiskStatusLocked(reason, req.Side)
			return errors.New(reason)
		}
	}
	m.snapshot.GlobalRisk = m.buildGlobalRiskStatusLocked("", req.Side)
	return nil
}

// currentGlobalExposureLocked 汇总当前所有 worker 的已开仓与待开仓风险敞口。
func (m *PolymarketManager) currentGlobalExposureLocked(targetSide string) globalExposure {
	out := globalExposure{}
	targetSide = strings.ToUpper(strings.TrimSpace(targetSide))
	for _, key := range m.order {
		worker := m.workers[key]
		if worker == nil {
			continue
		}
		active, side, amount := snapshotExposure(worker.snapshot)
		if !active {
			continue
		}
		out.openMarkets++
		out.openNotional += amount
		if targetSide != "" && side == targetSide {
			out.sameSideCount++
		}
	}
	return out
}

// snapshotExposure 从单个市场快照中提取会占用账户风险预算的暴露。
func snapshotExposure(snapshot entity.DashboardState) (bool, string, float64) {
	if snapshot.Position != nil {
		amount := snapshot.Position.Amount
		if amount <= 0 && snapshot.Position.EntryPrice > 0 && snapshot.Position.Size > 0 {
			amount = snapshot.Position.EntryPrice * snapshot.Position.Size
		}
		return true, strings.ToUpper(strings.TrimSpace(snapshot.Position.Side)), amount
	}
	if snapshot.PendingOrder != nil && strings.EqualFold(strings.TrimSpace(snapshot.PendingOrder.Action), "BUY") {
		return true, strings.ToUpper(strings.TrimSpace(snapshot.PendingOrder.Side)), maxFloat(snapshot.PendingOrder.Amount, 0)
	}
	return false, "", 0
}

// refreshLossCooldownLocked 基于最近关闭交易的连亏结果，决定是否启动全局冷却期。
func (m *PolymarketManager) refreshLossCooldownLocked(now time.Time) {
	if !m.lossCooldownUntil.IsZero() && now.After(m.lossCooldownUntil) {
		m.lossCooldownUntil = time.Time{}
	}
	if m.cfg.LossStreakLimit <= 0 || m.cfg.LossStreakCooldownMin <= 0 {
		return
	}

	streak, triggerKey := computeLossStreak(m.snapshot.LiveTrades)
	if streak == 0 {
		m.lossCooldownTrigger = ""
		return
	}
	if streak < m.cfg.LossStreakLimit || triggerKey == "" || triggerKey == m.lossCooldownTrigger {
		return
	}

	m.lossCooldownTrigger = triggerKey
	m.lossCooldownUntil = now.Add(time.Duration(m.cfg.LossStreakCooldownMin) * time.Minute)
}

// computeLossStreak 统计最近连续关闭交易中的净亏次数。
func computeLossStreak(rows []entity.LiveTradeSummary) (int, string) {
	closed := make([]entity.LiveTradeSummary, 0, len(rows))
	for _, row := range rows {
		if strings.ToUpper(strings.TrimSpace(row.Result)) != "CLOSED" {
			continue
		}
		closed = append(closed, row)
	}
	sort.SliceStable(closed, func(i, j int) bool {
		left := firstPresentString(closed[i].SettleTime, closed[i].OrderTime, closed[i].ID)
		right := firstPresentString(closed[j].SettleTime, closed[j].OrderTime, closed[j].ID)
		return left > right
	})
	if len(closed) == 0 {
		return 0, ""
	}

	triggerKey := firstPresentString(closed[0].ID, closed[0].SettleTime, closed[0].OrderTime)
	streak := 0
	for _, row := range closed {
		if row.Profit < -1e-9 {
			streak++
			continue
		}
		break
	}
	return streak, triggerKey
}

// buildGlobalRiskStatusLocked 把 manager 当前的账户层风控状态投影成 dashboard 可直接展示的结构。
func (m *PolymarketManager) buildGlobalRiskStatusLocked(blockReason, side string) entity.GlobalRiskStatus {
	exposure := m.currentGlobalExposureLocked(strings.ToUpper(strings.TrimSpace(side)))
	streak, _ := computeLossStreak(m.snapshot.LiveTrades)
	return entity.GlobalRiskStatus{
		Enabled:             m.cfg.MaxConcurrentMarkets > 0 || m.cfg.MaxTotalOpenNotional > 0 || m.cfg.MaxSameSideMarkets > 0 || m.cfg.LossStreakLimit > 0,
		OpenMarkets:         exposure.openMarkets,
		OpenNotional:        exposure.openNotional,
		SameSideOpenMarkets: exposure.sameSideCount,
		LossStreak:          streak,
		CooldownUntil:       zeroTimeString(m.lossCooldownUntil),
		LastBlockReason:     strings.TrimSpace(blockReason),
	}
}

// zeroTimeString 把零值时间统一渲染成空串，便于前端判断是否处于冷却期。
func zeroTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
