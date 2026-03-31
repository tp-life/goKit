package service

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	infraPolymarket "goKit/internal/infrastructure/polymarket"
)

// runAccountSnapshotPoller 周期性同步钱包持仓、历史和 PnL 快照。
func (s *PolymarketService) runAccountSnapshotPoller(ctx context.Context) {
	account := strings.ToLower(strings.TrimSpace(s.dashboardAccount()))
	if account == "" {
		return
	}

	// 启动后立即做一次同步，避免 dashboard 长时间显示空白。
	s.syncAccountSnapshot(ctx, account)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAccountSnapshot(ctx, account)
		}
	}
}

// syncAccountSnapshot 拉取账户侧快照，并把成功结果投影到 dashboard。
func (s *PolymarketService) syncAccountSnapshot(ctx context.Context, account string) {
	if strings.TrimSpace(account) == "" {
		return
	}

	// 账户同步不应长时间阻塞主流程，因此单独收一个更短的超时。
	syncCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	positions, errPositions := s.client.GetWalletPositions(syncCtx, account)
	closed, errClosed := s.client.GetWalletClosedPositions(syncCtx, account)
	activity, errActivity := s.client.GetTradeActivity(syncCtx, account, 500)

	if errPositions != nil && errClosed != nil && errActivity != nil {
		if s.logger != nil {
			s.logger.Warn("polymarket_account_sync_failed", "account", account, "positions_error", errPositions, "closed_error", errClosed, "activity_error", errActivity)
		}
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 分项更新账户快照，避免单个接口失败时把已有内容全部清空。
	if errPositions == nil {
		s.dashboard.WalletPositions = buildWalletPositions(positions)
		s.dashboard.LivePositionsCount = len(positions)
		s.dashboard.LiveUnrealizedPnL = computeWalletUnrealizedPnL(positions)
	}
	if errClosed == nil {
		s.dashboard.WalletHistory = buildWalletHistoryItems(closed)
		s.dashboard.LiveRealizedPnL = computeWalletRealizedPnL(closed)
	}
	if errActivity == nil {
		s.dashboard.LiveTrades = buildMarketAggregatedTrades(activity)
	}

	s.dashboard.LiveTotalPnL = s.dashboard.LiveRealizedPnL + s.dashboard.LiveUnrealizedPnL
	// 账户聚合变更后，顺手重建 dashboard 派生字段，确保轮次结果等视图同步刷新。
	s.syncDashboardLocked()
	s.publishLocked()
}

// dashboardAccount 返回 dashboard 应该用于账户同步的钱包地址。
func (s *PolymarketService) dashboardAccount() string {
	account := strings.TrimSpace(s.client.FunderHex())
	if account != "" {
		return account
	}
	return strings.TrimSpace(s.client.AddressHex())
}

// buildWalletPositions 把 Data API 持仓映射成 dashboard 直接使用的结构。
func buildWalletPositions(rows []infraPolymarket.DataPositionResponse) []entity.WalletPosition {
	out := make([]entity.WalletPosition, 0, len(rows))
	for _, row := range rows {
		out = append(out, entity.WalletPosition{
			ProxyWallet: row.ProxyWallet,
			Asset:       row.Asset,
			ConditionID: row.ConditionID,
			Slug:        firstNonEmpty(row.Slug, row.EventSlug),
			EventSlug:   row.EventSlug,
			Title:       row.Title,
			Outcome:     normalizeOutcomeLabel(firstNonEmpty(row.Outcome, row.Side)),
			Side:        normalizeOutcomeLabel(firstNonEmpty(row.Side, row.Outcome)),
			Size:        row.Size.OrZero(),
			AvgPrice:    row.AvgPrice.Ptr(),
			CurPrice:    row.CurPrice.Ptr(),
			RealizedPnL: row.RealizedPnL.Ptr(),
			Redeemable:  row.Redeemable,
			Mergeable:   row.Mergeable,
		})
	}
	return out
}

// buildWalletHistoryItems 把已关闭仓位映射成 dashboard 历史项。
func buildWalletHistoryItems(rows []infraPolymarket.DataClosedPositionResponse) []entity.TradeHistoryItem {
	items := make([]entity.TradeHistoryItem, 0, len(rows))
	for _, row := range rows {
		side := normalizeOutcomeLabel(firstNonEmpty(row.Outcome, row.Side, row.PositionSide))
		item := entity.TradeHistoryItem{
			Time:    firstNonEmpty(row.EndDate.String(), row.Timestamp.String(), row.UpdatedAt.String(), "-"),
			Slug:    firstNonEmpty(row.Slug, row.MarketSlug, row.Question, "-"),
			Action:  "CLOSE",
			Side:    firstNonEmpty(side, "-"),
			OrderID: firstNonEmpty(row.TransactionHash.String(), row.ID.String()),
			Status:  "closed",
			Reason:  "wallet_sync",
			Price:   firstFloat(row.AvgPrice.Ptr(), row.AvgPriceAlt.Ptr()),
			Amount:  row.Size.OrZero(),
			PnL:     firstFloatPtr(row.RealizedPnL.Ptr(), row.RealizedPnLAlt.Ptr()),
		}
		items = append(items, item)
	}
	if len(items) > 200 {
		return append([]entity.TradeHistoryItem(nil), items[:200]...)
	}
	return items
}

type tradeGroup struct {
	ID             string
	Slug           string
	Direction      string
	Reason         string
	Outcomes       map[string]struct{}
	BuyCount       int
	SellCount      int
	RedeemCount    int
	BuySize        float64
	SellSize       float64
	BuyNotional    float64
	SellNotional   float64
	RedeemNotional float64
	FirstTS        string
	LastTS         string
	FirstTSMS      int64
	LastTSMS       int64
}

// buildMarketAggregatedTrades 把活动流水按市场聚合成更易读的实时交易摘要。
func buildMarketAggregatedTrades(rows []infraPolymarket.DataActivityResponse) []entity.LiveTradeSummary {
	sortedRows := append([]infraPolymarket.DataActivityResponse(nil), rows...)
	sort.Slice(sortedRows, func(i, j int) bool {
		return tradeTimestampMillis(sortedRows[i]) < tradeTimestampMillis(sortedRows[j])
	})

	groups := map[string]*tradeGroup{}
	for _, row := range sortedRows {
		kind := tradeEventKind(row)
		if kind == "IGNORE" {
			continue
		}

		price := row.Price.OrZero()
		size := firstFloat(row.SizeMatched.Ptr(), row.Size.Ptr(), row.OriginalSize.Ptr())
		usdcSize := tradeUSDCSize(row)
		if kind != "REDEEM" && (price <= 0 || size <= 0) {
			continue
		}
		if kind == "REDEEM" && usdcSize <= 0 {
			continue
		}

		key := tradeMarketKey(row)
		ts := firstNonEmpty(row.MatchTime.String(), row.MatchTimeAlt.String(), row.Timestamp.String(), row.CreatedAt.String(), row.Time.String())
		tsMS := tradeTimestampMillis(row)

		group := groups[key]
		if group == nil {
			group = &tradeGroup{
				ID:        "agg-" + key,
				Slug:      firstNonEmpty(row.EventSlug, row.Slug),
				Direction: normalizeOutcomeLabel(firstNonEmpty(row.Outcome, row.Direction)),
				Reason:    resolveTradeReason(row),
				Outcomes:  map[string]struct{}{},
				FirstTS:   ts,
				LastTS:    ts,
				FirstTSMS: tsMS,
				LastTSMS:  tsMS,
			}
			groups[key] = group
		}

		if tsMS > 0 && (group.FirstTSMS == 0 || tsMS < group.FirstTSMS) {
			group.FirstTSMS = tsMS
			group.FirstTS = ts
		}
		if tsMS > 0 && tsMS >= group.LastTSMS {
			group.LastTSMS = tsMS
			group.LastTS = ts
		}

		if outcome := normalizeOutcomeLabel(firstNonEmpty(row.Outcome, row.Direction)); outcome != "" && outcome != "-" {
			group.Outcomes[outcome] = struct{}{}
		}

		switch kind {
		case "BUY":
			group.BuyCount++
			group.BuySize += size
			group.BuyNotional += usdcSize
		case "SELL":
			group.SellCount++
			group.SellSize += size
			group.SellNotional += usdcSize
		case "REDEEM":
			group.RedeemCount++
			group.RedeemNotional += usdcSize
		}
	}

	out := make([]entity.LiveTradeSummary, 0, len(groups))
	for _, group := range groups {
		if group.BuyCount+group.SellCount+group.RedeemCount <= 0 {
			continue
		}

		if len(group.Outcomes) == 1 {
			for outcome := range group.Outcomes {
				group.Direction = outcome
			}
		} else if len(group.Outcomes) > 1 {
			group.Direction = "MIX"
		}

		var entryPrice *float64
		if group.BuySize > 1e-9 {
			entryPrice = floatPtr(group.BuyNotional / group.BuySize)
		}
		var exitPrice *float64
		if group.SellSize > 1e-9 {
			exitPrice = floatPtr(group.SellNotional / group.SellSize)
		}

		result := "OPEN"
		if group.SellCount > 0 || group.RedeemCount > 0 {
			result = "CLOSED"
		}

		out = append(out, entity.LiveTradeSummary{
			ID:              group.ID,
			PairID:          group.ID,
			Direction:       group.Direction,
			Reason:          group.Reason,
			Slug:            group.Slug,
			BuyCount:        group.BuyCount,
			SellCount:       group.SellCount,
			RedeemCount:     group.RedeemCount,
			BuyUSDC:         group.BuyNotional,
			SellUSDC:        group.SellNotional,
			RedeemUSDC:      group.RedeemNotional,
			Size:            tradeDisplaySize(group.BuySize, group.SellSize),
			EntryPriceQuote: entryPrice,
			ExitPriceQuote:  exitPrice,
			OrderTime:       group.FirstTS,
			SettleTime:      group.LastTS,
			Profit:          group.SellNotional + group.RedeemNotional - group.BuyNotional,
			Result:          result,
			Status:          "AGG",
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return toMillis(out[i].SettleTime) < toMillis(out[j].SettleTime)
	})
	return out
}

// computeWalletRealizedPnL 汇总已关闭仓位中的 realized pnl。
func computeWalletRealizedPnL(rows []infraPolymarket.DataClosedPositionResponse) float64 {
	total := 0.0
	for _, row := range rows {
		total += firstFloat(row.RealizedPnL.Ptr(), row.RealizedPnLAlt.Ptr())
	}
	return total
}

// computeWalletUnrealizedPnL 依据当前持仓的平均价和现价计算未实现盈亏。
func computeWalletUnrealizedPnL(rows []infraPolymarket.DataPositionResponse) float64 {
	total := 0.0
	for _, row := range rows {
		if !row.CurPrice.Valid || !row.AvgPrice.Valid || !row.Size.Valid {
			continue
		}
		total += (row.CurPrice.Value - row.AvgPrice.Value) * row.Size.Value
	}
	return total
}

// tradeEventKind 判断一条活动流水属于买入、卖出、兑奖还是忽略项。
func tradeEventKind(row infraPolymarket.DataActivityResponse) string {
	typ := strings.ToUpper(strings.TrimSpace(row.Type.String()))
	side := strings.ToUpper(strings.TrimSpace(row.Side.String()))
	if typ == "REDEEM" {
		return "REDEEM"
	}
	if typ == "DEPOSIT" || typ == "WITHDRAW" || typ == "WITHDRAWAL" || typ == "TRANSFER" {
		return "IGNORE"
	}
	if side == "BUY" || side == "SELL" {
		return side
	}
	return "IGNORE"
}

// tradeTimestampMillis 把活动流水里的时间字段统一转换成毫秒时间戳。
func tradeTimestampMillis(row infraPolymarket.DataActivityResponse) int64 {
	for _, raw := range []string{
		row.MatchTime.String(),
		row.MatchTimeAlt.String(),
		row.Timestamp.String(),
		row.CreatedAt.String(),
		row.Time.String(),
	} {
		if ms := toMillis(raw); ms > 0 {
			return ms
		}
	}
	return 0
}

// tradeUSDCSize 返回一条流水对应的 USDC 名义金额。
func tradeUSDCSize(row infraPolymarket.DataActivityResponse) float64 {
	if row.USDCSize.Valid {
		return absFloat(row.USDCSize.Value)
	}
	if row.USDCSizeAlt.Valid {
		return absFloat(row.USDCSizeAlt.Value)
	}
	price := row.Price.OrZero()
	size := firstFloat(row.SizeMatched.Ptr(), row.Size.Ptr(), row.OriginalSize.Ptr())
	if price > 0 && size > 0 {
		return absFloat(price * size)
	}
	return 0
}

// tradeMarketKey 为一条活动流水挑选稳定的市场聚合键。
func tradeMarketKey(row infraPolymarket.DataActivityResponse) string {
	if key := firstNonEmpty(row.ConditionID.String(), row.ConditionIDAlt.String(), row.Market.String(), row.MarketID.String()); key != "" {
		return key
	}
	if slug := firstNonEmpty(row.EventSlug, row.Slug); slug != "" {
		return slug
	}
	if asset := firstNonEmpty(row.AssetID.String(), row.Asset.String(), row.TokenID.String()); asset != "" {
		return asset
	}
	return "market"
}

// resolveTradeReason 为一条活动流水生成可展示的市场说明文本。
func resolveTradeReason(row infraPolymarket.DataActivityResponse) string {
	if title := firstNonEmpty(row.Title, row.EventTitle, row.Name, row.Question); title != "" {
		return title
	}
	if slug := firstNonEmpty(row.EventSlug, row.Slug); slug != "" {
		return slug
	}
	return "市场"
}

// normalizeOutcomeLabel 把 YES/NO 等标签统一归一成 UP/DOWN。
func normalizeOutcomeLabel(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	switch {
	case strings.Contains(normalized, "UP"), normalized == "YES":
		return "UP"
	case strings.Contains(normalized, "DOWN"), normalized == "NO":
		return "DOWN"
	case normalized == "":
		return "-"
	default:
		return normalized
	}
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// firstFloat 返回第一个有效浮点值。
func firstFloat(values ...*float64) float64 {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return 0
}

// minMatchedSize 返回买卖两侧共同匹配的最小份额。
func minMatchedSize(a, b float64) float64 {
	if a <= 1e-9 || b <= 1e-9 {
		return 0
	}
	if a < b {
		return a
	}
	return b
}

// tradeDisplaySize 返回聚合交易在界面上展示的份额。
func tradeDisplaySize(buySize, sellSize float64) float64 {
	if matched := minMatchedSize(buySize, sellSize); matched > 1e-9 {
		return matched
	}
	return maxFloat(buySize, sellSize)
}

// maxFloat 返回两个浮点数中的较大值。
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// toMillis 把字符串形式的时间或数字时间统一转换成毫秒。
func toMillis(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1e12 {
			return n
		}
		return n * 1000
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		if n > 1e12 {
			return int64(n)
		}
		return int64(n * 1000)
	}
	if ts, err := time.Parse(time.RFC3339, strings.Replace(raw, "Z", "+00:00", 1)); err == nil {
		return ts.UnixMilli()
	}
	return 0
}
