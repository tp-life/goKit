package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"goKit/internal/domain/entity"
)

type strategyEntry struct {
	StrategyKey string
	MarketKey   string
	MarketSlug  string
	Side        string
	WindowSec   int
	Cost        float64
	ClosedAt    string
}

type strategyClosedTrade struct {
	StrategyKey string
	MarketKey   string
	MarketSlug  string
	Mode        string
	Side        string
	WindowSec   int
	Profit      float64
	ClosedAt    string
}

// buildStrategyPerformanceFromHistory 从本地闭环成交历史中恢复策略收益榜。
func buildStrategyPerformanceFromHistory(history []entity.TradeHistoryItem, liveTrades []entity.LiveTradeSummary, lookback int, minProfit float64, autoDisable bool) []entity.StrategyPerformance {
	closedTrades := buildClosedStrategyTrades(history, liveTrades)
	if len(closedTrades) == 0 {
		return []entity.StrategyPerformance{}
	}

	groups := map[string][]strategyClosedTrade{}
	for _, item := range closedTrades {
		groups[item.StrategyKey] = append(groups[item.StrategyKey], item)
	}

	out := make([]entity.StrategyPerformance, 0, len(groups))
	for _, rows := range groups {
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].ClosedAt > rows[j].ClosedAt
		})
		effective := rows
		if lookback > 0 && len(effective) > lookback {
			effective = effective[:lookback]
		}
		if len(effective) == 0 {
			continue
		}

		wins := 0
		losses := 0
		profit := 0.0
		for _, row := range effective {
			profit += row.Profit
			switch {
			case row.Profit > 1e-9:
				wins++
			case row.Profit < -1e-9:
				losses++
			}
		}
		item := entity.StrategyPerformance{
			StrategyKey:  effective[0].StrategyKey,
			MarketKey:    effective[0].MarketKey,
			MarketSlug:   effective[0].MarketSlug,
			Mode:         effective[0].Mode,
			Side:         effective[0].Side,
			WindowSec:    effective[0].WindowSec,
			Trades:       len(effective),
			Wins:         wins,
			Losses:       losses,
			WinRate:      float64(wins) / float64(len(effective)) * 100,
			Profit:       profit,
			AvgProfit:    profit / float64(len(effective)),
			LastClosedAt: effective[0].ClosedAt,
		}
		if autoDisable && lookback > 0 && len(effective) >= lookback && profit <= minProfit {
			item.Disabled = true
			item.DisableReason = fmt.Sprintf("最近 %d 笔净收益 %.4f 低于阈值 %.4f", len(effective), profit, minProfit)
		}
		out = append(out, item)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Disabled != out[j].Disabled {
			return !out[i].Disabled
		}
		if out[i].Profit != out[j].Profit {
			return out[i].Profit > out[j].Profit
		}
		return out[i].StrategyKey < out[j].StrategyKey
	})
	return out
}

// buildClosedStrategyTrades 把本地买卖成交流水恢复成闭环收益记录。
func buildClosedStrategyTrades(history []entity.TradeHistoryItem, liveTrades []entity.LiveTradeSummary) []strategyClosedTrade {
	sorted := cloneHistory(history)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time < sorted[j].Time
	})

	orderMeta := map[string]entity.TradeHistoryItem{}
	openEntries := map[string]strategyEntry{}
	out := make([]strategyClosedTrade, 0, 16)

	for _, item := range sorted {
		if strings.TrimSpace(item.OrderID) != "" && strings.EqualFold(strings.TrimSpace(item.Status), "submitted") {
			orderMeta[item.OrderID] = item
		}
		if !strings.EqualFold(strings.TrimSpace(item.Status), "filled") {
			continue
		}

		enriched := enrichStrategyHistoryItem(item, orderMeta[item.OrderID])
		slotKey := strings.ToLower(strings.TrimSpace(enriched.Slug)) + "|" + strings.ToUpper(strings.TrimSpace(enriched.Side))
		switch strings.ToUpper(strings.TrimSpace(enriched.Action)) {
		case "BUY":
			cost := enriched.Amount
			if cost <= 0 && enriched.Price > 0 && enriched.Size > 0 {
				cost = enriched.Price * enriched.Size
			}
			openEntries[slotKey] = strategyEntry{
				StrategyKey: enriched.StrategyKey,
				MarketKey:   extractMarketKey(enriched),
				MarketSlug:  enriched.Slug,
				Side:        strings.ToUpper(strings.TrimSpace(enriched.Side)),
				WindowSec:   enriched.WindowSec,
				Cost:        cost,
				ClosedAt:    enriched.Time,
			}
		case "SELL":
			entry, ok := openEntries[slotKey]
			if !ok {
				continue
			}
			proceeds := enriched.Amount
			if proceeds <= 0 && enriched.Price > 0 && enriched.Size > 0 {
				proceeds = enriched.Price * enriched.Size
			}
			out = append(out, strategyClosedTrade{
				StrategyKey: entry.StrategyKey,
				MarketKey:   entry.MarketKey,
				MarketSlug:  entry.MarketSlug,
				Mode:        strategyModeFromKey(entry.StrategyKey),
				Side:        entry.Side,
				WindowSec:   entry.WindowSec,
				Profit:      proceeds - entry.Cost,
				ClosedAt:    enriched.Time,
			})
			delete(openEntries, slotKey)
		}
	}

	// 对于“持有到结算”的策略，本地历史里可能没有 SELL，
	// 这里用账户聚合后的 closed live trades 为未闭合的本地买单补一条闭环记录。
	sortedLive := cloneLiveTrades(liveTrades)
	sort.SliceStable(sortedLive, func(i, j int) bool {
		return firstPresentString(sortedLive[i].SettleTime, sortedLive[i].OrderTime, sortedLive[i].ID) <
			firstPresentString(sortedLive[j].SettleTime, sortedLive[j].OrderTime, sortedLive[j].ID)
	})
	for _, row := range sortedLive {
		if !strings.EqualFold(strings.TrimSpace(row.Result), "CLOSED") {
			continue
		}
		side := strings.ToUpper(strings.TrimSpace(normalizeOutcomeLabel(row.Direction)))
		if side == "" || side == "-" || strings.EqualFold(side, "MIX") {
			continue
		}
		slotKey := strings.ToLower(strings.TrimSpace(row.Slug)) + "|" + side
		entry, ok := openEntries[slotKey]
		if !ok {
			continue
		}
		out = append(out, strategyClosedTrade{
			StrategyKey: entry.StrategyKey,
			MarketKey:   entry.MarketKey,
			MarketSlug:  entry.MarketSlug,
			Mode:        strategyModeFromKey(entry.StrategyKey),
			Side:        entry.Side,
			WindowSec:   entry.WindowSec,
			Profit:      row.Profit,
			ClosedAt:    firstPresentString(row.SettleTime, row.OrderTime, row.ID),
		})
		delete(openEntries, slotKey)
	}
	return out
}

// enrichStrategyHistoryItem 尝试从对应 submitted 记录中补齐 filled 行缺失的策略元数据。
func enrichStrategyHistoryItem(item entity.TradeHistoryItem, submitted entity.TradeHistoryItem) entity.TradeHistoryItem {
	out := item
	if strings.TrimSpace(out.StrategyKey) == "" {
		out.StrategyKey = strings.TrimSpace(submitted.StrategyKey)
	}
	if out.WindowSec <= 0 {
		out.WindowSec = submitted.WindowSec
	}
	if strings.TrimSpace(out.Reason) == "" {
		out.Reason = strings.TrimSpace(submitted.Reason)
	}
	if strings.TrimSpace(out.Execution) == "" {
		out.Execution = strings.TrimSpace(submitted.Execution)
	}
	if out.WindowSec <= 0 {
		out.WindowSec = parseWindowSecFromReason(out.Reason)
	}
	if strings.TrimSpace(out.StrategyKey) == "" && out.WindowSec > 0 && strings.TrimSpace(out.Side) != "" {
		out.StrategyKey = buildStrategyKey(extractMarketKey(out), 0, out.WindowSec, out.Side)
	}
	return out
}

// strategyModeFromKey 从策略键中提取策略模式，便于把主策略与尾盘策略分开统计。
func strategyModeFromKey(key string) string {
	parts := strings.Split(strings.TrimSpace(key), "|")
	if len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[1]), "tail-sweep") {
		return "tail-sweep"
	}
	return "main"
}

// extractMarketKey 优先从 strategy key 还原市场键，否则按 slug 退化生成。
func extractMarketKey(item entity.TradeHistoryItem) string {
	if key := strings.TrimSpace(item.StrategyKey); key != "" {
		parts := strings.Split(key, "|")
		if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}
	if slug := strings.TrimSpace(item.Slug); slug != "" {
		parts := strings.Split(slug, "-updown-")
		if len(parts) == 2 {
			interval := strings.TrimSpace(parts[1])
			if dash := strings.Index(interval, "-"); dash > 0 {
				interval = interval[:dash]
			}
			return strings.ToLower(parts[0]) + "-" + interval
		}
		return strings.ToLower(slug)
	}
	return "market"
}

// parseWindowSecFromReason 从自动交易落盘原因中恢复窗口秒数。
func parseWindowSecFromReason(reason string) int {
	raw := strings.TrimSpace(reason)
	if !strings.Contains(raw, "剩余≤") || !strings.Contains(raw, "s") {
		return 0
	}
	start := strings.Index(raw, "剩余≤")
	if start < 0 {
		return 0
	}
	start += len("剩余≤")
	end := strings.Index(raw[start:], "s")
	if end <= 0 {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw[start : start+end]))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// negativeStrategyBlockReasonLocked 根据最近闭环表现决定是否临时停用某个策略桶。
func (m *PolymarketManager) negativeStrategyBlockReasonLocked(req autoTradeRiskRequest) string {
	lookback := m.cfg.AutoDisableLookback
	minProfit := m.cfg.AutoDisableMinProfit
	if req.DisableLookback > 0 {
		lookback = req.DisableLookback
		minProfit = req.DisableMinProfit
	}
	leaderboard := buildStrategyPerformanceFromHistory(
		m.mergeTradeHistoryLocked(),
		cloneLiveTrades(m.snapshot.LiveTrades),
		lookback,
		minProfit,
		true,
	)
	for _, item := range leaderboard {
		if item.StrategyKey != strings.TrimSpace(req.StrategyKey) {
			continue
		}
		if item.Disabled {
			return "策略已自动停用: " + item.DisableReason
		}
		return ""
	}
	return ""
}

// strategyLossStreakBlockReasonLocked 根据最近连续亏损结果，决定是否临时停用指定策略桶。
func (m *PolymarketManager) strategyLossStreakBlockReasonLocked(req autoTradeRiskRequest) string {
	if req.LossStreakLimit <= 0 || strings.TrimSpace(req.StrategyKey) == "" {
		return ""
	}
	streak := computeStrategyLossStreak(
		m.mergeTradeHistoryLocked(),
		cloneLiveTrades(m.snapshot.LiveTrades),
		req.StrategyKey,
	)
	if streak >= req.LossStreakLimit {
		return fmt.Sprintf("策略连续亏损 %d 笔，达到上限 %d", streak, req.LossStreakLimit)
	}
	return ""
}

// recommendAutoTradeAmount 根据最近闭环表现为自动交易给出一个更合适的仓位建议。
func (m *PolymarketManager) recommendAutoTradeAmount(req autoTradeSizeRequest) float64 {
	base := req.BaseTradeAmount
	if base <= 0 {
		return 0
	}
	if !m.cfg.AutoSizeByPerformance {
		return base
	}

	m.mu.RLock()
	leaderboard := buildStrategyPerformanceFromHistory(
		m.mergeTradeHistoryLocked(),
		cloneLiveTrades(m.snapshot.LiveTrades),
		m.cfg.AutoSizeLookback,
		m.cfg.AutoDisableMinProfit,
		false,
	)
	minTrades := m.cfg.AutoSizeMinTrades
	minMultiplier := m.cfg.AutoSizeMinMultiplier
	maxMultiplier := m.cfg.AutoSizeMaxMultiplier
	m.mu.RUnlock()

	if minMultiplier <= 0 {
		minMultiplier = 0.5
	}
	if maxMultiplier < minMultiplier {
		maxMultiplier = minMultiplier
	}

	for _, item := range leaderboard {
		if item.StrategyKey != strings.TrimSpace(req.StrategyKey) {
			continue
		}
		if minTrades > 0 && item.Trades < minTrades {
			return base
		}
		multiplier := 1.0
		switch {
		case item.Profit <= 0 || item.WinRate < 45:
			multiplier = minMultiplier
		case item.WinRate >= 70 && item.Profit > 1:
			multiplier = maxMultiplier
		case item.WinRate >= 60 && item.Profit > 0:
			multiplier = minFloat(maxMultiplier, 1.25)
		case item.WinRate < 55:
			multiplier = minFloat(0.8, maxMultiplier)
		}
		return base * multiplier
	}
	return base
}

// computeStrategyLossStreak 统计某个策略桶最近连续闭环交易中的亏损次数。
func computeStrategyLossStreak(history []entity.TradeHistoryItem, liveTrades []entity.LiveTradeSummary, strategyKey string) int {
	key := strings.TrimSpace(strategyKey)
	if key == "" {
		return 0
	}
	rows := buildClosedStrategyTrades(history, liveTrades)
	filtered := make([]strategyClosedTrade, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.StrategyKey) != key {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].ClosedAt > filtered[j].ClosedAt
	})
	streak := 0
	for _, row := range filtered {
		if row.Profit < -1e-9 {
			streak++
			continue
		}
		break
	}
	return streak
}
