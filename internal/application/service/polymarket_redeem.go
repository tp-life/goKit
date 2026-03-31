package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// runAutoRedeemer 负责按天调度一次统一兑奖任务。
func (s *PolymarketService) runAutoRedeemer(ctx context.Context) {
	account, enabled, reason := s.autoRedeemAccount()

	s.mu.Lock()
	s.dashboard.AutoRedeem.Enabled = enabled
	s.dashboard.AutoRedeem.LastError = reason
	s.dashboard.AutoRedeem.LastRunAt = s.state.LastRedeemAt
	s.publishLocked()
	s.mu.Unlock()

	if !enabled {
		return
	}

	for {
		s.mu.RLock()
		nextRun := nextAutoRedeemRun(time.Now(), parseRedeemTime(s.state.LastRedeemAt), s.cfg.AutoRedeemHourLocal)
		s.mu.RUnlock()

		s.mu.Lock()
		s.dashboard.AutoRedeem.Enabled = true
		s.dashboard.AutoRedeem.NextRunAt = nextRun.Format(time.RFC3339)
		s.publishLocked()
		s.mu.Unlock()

		wait := time.Until(nextRun)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		s.addLog("INFO", fmt.Sprintf("开始每日统一兑奖任务，计划时间 %02d:00", s.cfg.AutoRedeemHourLocal))
		s.executeAutoRedeem(ctx, account)
	}
}

// executeAutoRedeem 执行当天的统一兑奖任务，并把结果写回 dashboard 与本地状态。
func (s *PolymarketService) executeAutoRedeem(ctx context.Context, account string) {
	jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	claimable, pendingCount, err := s.client.GetRedeemableConditions(jobCtx, account)
	claimableCount := len(claimable)
	now := time.Now()
	nowISO := now.Format(time.RFC3339)

	result := map[string]any{
		"time":          now.Format("2006-01-02 15:04:05"),
		"pending_count": pendingCount,
		"claimable":     claimableCount,
	}
	lastError := ""
	attemptRows := make([]map[string]any, 0)

	if err == nil {
		successCount := 0
		failureCount := 0
		txs := make([]string, 0, len(claimable))
		if len(claimable) > s.cfg.AutoRedeemMaxPerRun {
			claimable = append([]string(nil), claimable[:s.cfg.AutoRedeemMaxPerRun]...)
		}
		result["processed"] = len(claimable)

		if len(claimable) == 0 {
			s.addLog("INFO", "本次统一兑奖未发现可执行的 condition")
		}
		for _, conditionID := range claimable {
			s.addLog("INFO", fmt.Sprintf("开始兑奖 condition %s", shortConditionID(conditionID)))
			row, redeemErr := s.redeemConditionWithRetry(jobCtx, conditionID)
			attemptRows = append(attemptRows, row)
			if redeemErr != nil {
				failureCount++
				if lastError == "" {
					lastError = redeemErr.Error()
				}
				s.addLog("ERR", fmt.Sprintf("兑奖失败 %s: %v", shortConditionID(conditionID), redeemErr))
				continue
			}
			successCount++
			if tx, _ := row["tx"].(string); strings.TrimSpace(tx) != "" {
				txs = append(txs, tx)
				s.addLog("TRADE", fmt.Sprintf("兑奖成功 %s | tx %s", shortConditionID(conditionID), tx))
			} else {
				s.addLog("TRADE", fmt.Sprintf("兑奖成功 %s", shortConditionID(conditionID)))
			}
		}

		result["ok"] = failureCount == 0
		result["message"] = statusText(failureCount == 0, "ok", "partial_failed")
		result["success_count"] = successCount
		result["failure_count"] = failureCount
		result["attempts"] = attemptRows
		if len(txs) > 0 {
			result["txs"] = txs
		}
	} else {
		lastError = err.Error()
		result["ok"] = false
		result["message"] = err.Error()
	}

	s.mu.Lock()
	s.state.LastRedeemAt = nowISO
	s.dashboard.AutoRedeem.Enabled = true
	s.dashboard.AutoRedeem.PendingCount = pendingCount
	s.dashboard.AutoRedeem.ClaimableCount = claimableCount
	s.dashboard.AutoRedeem.LastRunAt = nowISO
	s.dashboard.AutoRedeem.LastResult = result
	s.dashboard.AutoRedeem.LastError = lastError
	s.dashboard.AutoRedeem.NextRunAt = nextAutoRedeemRun(now, &now, s.cfg.AutoRedeemHourLocal).Format(time.RFC3339)
	s.persistLocked(context.Background())
	s.publishLocked()
	s.mu.Unlock()

	// 兑奖后账户持仓和聚合盈亏通常会变化，任务结束后补一次账户快照同步。
	if lastError == "" {
		s.syncAccountSnapshot(ctx, account)
	}
}

// redeemConditionWithRetry 执行单个 condition 的兑奖，并在必要时按受限次数重试。
func (s *PolymarketService) redeemConditionWithRetry(ctx context.Context, conditionID string) (map[string]any, error) {
	maxRetry := s.cfg.AutoRedeemMaxRetry
	if maxRetry <= 0 {
		maxRetry = 1
	}

	row := map[string]any{
		"condition_id": conditionID,
		"attempts":     0,
		"ok":           false,
	}
	var lastErr error
	for attempt := 1; attempt <= maxRetry; attempt++ {
		row["attempts"] = attempt

		// 每次尝试都使用独立的回执等待超时，避免单次等待把整场任务卡死。
		attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.AutoRedeemReceiptSec)*time.Second)
		redeemResult, err := s.client.RedeemCondition(attemptCtx, conditionID)
		cancel()

		if redeemResult != nil {
			row["tx"] = redeemResult.TxHash
			row["transaction_id"] = redeemResult.TransactionID
			row["block_number"] = redeemResult.BlockNumber
			row["gas_used"] = redeemResult.GasUsed
			row["receipt_status"] = redeemResult.Status
			row["state"] = redeemResult.State
			row["proxy_wallet"] = redeemResult.ProxyWallet
			row["error_message"] = redeemResult.ErrorMessage
		}
		if err == nil {
			row["ok"] = true
			row["message"] = "ok"
			return row, nil
		}

		lastErr = err
		row["message"] = err.Error()

		// 已经发出交易但只是在等回执超时，此时不再重发，避免重复 nonce 和重复交易。
		if strings.Contains(err.Error(), "context deadline exceeded") && redeemResult != nil && (strings.TrimSpace(redeemResult.TxHash) != "" || strings.TrimSpace(redeemResult.TransactionID) != "") {
			row["message"] = "交易已提交，但等待终态超时"
			if strings.TrimSpace(redeemResult.TxHash) != "" {
				return row, fmt.Errorf("兑奖交易 %s 等待回执超时", redeemResult.TxHash)
			}
			return row, fmt.Errorf("兑奖事务 %s 等待终态超时", redeemResult.TransactionID)
		}
		if attempt >= maxRetry {
			break
		}

		s.addLog("WARN", fmt.Sprintf("兑奖重试 %s，第 %d/%d 次失败: %v", shortConditionID(conditionID), attempt, maxRetry, err))
		select {
		case <-ctx.Done():
			return row, ctx.Err()
		case <-time.After(time.Duration(s.cfg.AutoRedeemRetryDelay) * time.Second):
		}
	}
	return row, lastErr
}

// autoRedeemAccount 校验兑奖前置条件，并返回可用的钱包地址。
func (s *PolymarketService) autoRedeemAccount() (string, bool, string) {
	if !s.cfg.AutoRedeem {
		return "", false, ""
	}
	account := s.dashboardAccount()
	if account == "" {
		return "", false, "缺少可用于兑奖的钱包地址"
	}
	if !s.client.HasPrivateKey() {
		return "", false, "缺少 PRIVATE_KEY，无法兑奖"
	}

	// EOA 继续直接链上兑奖；Proxy/Safe 则切换到 relayer 路径。
	switch s.cfg.SignatureType {
	case 1, 2:
		if !s.cfg.HasAnyRelayerAuth() {
			return "", false, "代理钱包兑奖需要配置 Builder API 凭证或 POLYMARKET_RELAYER_API_KEY"
		}
	default:
		if s.client.FunderHex() != "" && s.client.AddressHex() != "" && s.client.FunderHex() != s.client.AddressHex() {
			return "", false, "EOA 兑奖要求签名地址与 FUNDER_ADDRESS 一致"
		}
		if s.cfg.PolygonRPCURL == "" {
			return "", false, "缺少 POLYGON_RPC_URL，无法兑奖"
		}
	}
	return account, true, ""
}

// nextAutoRedeemRun 计算下一次日度兑奖任务的触发时间。
func nextAutoRedeemRun(now time.Time, lastRun *time.Time, hourLocal int) time.Time {
	scheduledToday := time.Date(now.Year(), now.Month(), now.Day(), hourLocal, 0, 0, 0, now.Location())
	if lastRun != nil && sameCalendarDay(*lastRun, now) {
		return scheduledToday.Add(24 * time.Hour)
	}
	if now.Before(scheduledToday) {
		return scheduledToday
	}
	return now
}

// parseRedeemTime 解析本地状态里的 last_redeem_at 字段。
func parseRedeemTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed
	}
	return nil
}

// sameCalendarDay 判断两个时间是否落在同一本地自然日。
func sameCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// shortConditionID 返回更适合日志展示的 condition_id 缩写。
func shortConditionID(conditionID string) string {
	normalized := strings.TrimSpace(conditionID)
	if len(normalized) <= 14 {
		return normalized
	}
	return normalized[:8] + "..." + normalized[len(normalized)-6:]
}
