package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

func (r *StrategyRunner) fundingRateHistorySyncLoop(ctx context.Context) {
	interval := r.cfg.FundingRateHistorySyncInterval
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.syncFundingRateHistory(ctx, now.UTC())
		}
	}
}

func (r *StrategyRunner) syncFundingRateHistory(ctx context.Context, now time.Time) {
	if r == nil || r.marketRepo == nil {
		return
	}
	lookback := r.cfg.FundingRateHistoryLookback
	if lookback <= 0 {
		return
	}
	targets := r.fundingRateHistoryTargets()
	if len(targets) == 0 {
		return
	}
	start := now.Add(-lookback)
	syncedSymbols := 0
	savedRows := 0
	for exchangeName, items := range targets {
		exchangeStartedAt := time.Now().UTC()
		market := r.markets[strings.ToLower(strings.TrimSpace(exchangeName))]
		provider, ok := market.(exchange.FundingRateHistoryProvider)
		if !ok || market == nil || !market.Enabled() {
			r.logger.Info("funding_rate_history_sync_exchange_skipped",
				slog.String("exchange", exchangeName),
				slog.Int("target_symbols", len(items)),
			)
			continue
		}
		r.logger.Info("funding_rate_history_sync_exchange_begin",
			slog.String("exchange", exchangeName),
			slog.Int("target_symbols", len(items)),
			slog.String("lookback", lookback.String()),
		)
		syncTargets := r.prepareFundingRateHistorySyncTargets(ctx, exchangeName, items, start, now)
		exchangeSyncedSymbols := 0
		exchangeSavedRows := 0
		exchangeSkippedSymbols := len(items) - len(syncTargets)
		for _, target := range syncTargets {
			history, err := provider.FetchFundingRateHistory(ctx, target.Symbol, target.StartTime, now)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				r.logger.Warn("sync_funding_rate_history_failed",
					slog.String("exchange", exchangeName),
					slog.String("symbol", target.Symbol.Symbol),
					slog.String("start_time", target.StartTime.Format(time.RFC3339)),
					slog.Any("err", err),
				)
				continue
			}
			if len(history) == 0 {
				continue
			}
			if err := r.marketRepo.SaveFundingRateHistory(ctx, history); err != nil {
				r.logger.Error("save_funding_rate_history_failed",
					slog.String("exchange", exchangeName),
					slog.String("symbol", target.Symbol.Symbol),
					slog.Any("err", err),
				)
				continue
			}
			syncedSymbols++
			savedRows += len(history)
			exchangeSyncedSymbols++
			exchangeSavedRows += len(history)
		}
		r.logger.Info("funding_rate_history_sync_exchange_done",
			slog.String("exchange", exchangeName),
			slog.Int("requested_symbols", len(syncTargets)),
			slog.Int("skipped_symbols", exchangeSkippedSymbols),
			slog.Int("synced_symbols", exchangeSyncedSymbols),
			slog.Int("saved_rows", exchangeSavedRows),
			slog.Duration("elapsed", time.Since(exchangeStartedAt)),
		)
		if ctx.Err() != nil {
			return
		}
	}
	if syncedSymbols > 0 {
		r.logger.Info("funding_rate_history_synced",
			slog.Int("symbols", syncedSymbols),
			slog.Int("rows", savedRows),
			slog.String("lookback", lookback.String()),
		)
	}
}

type fundingRateHistorySyncTarget struct {
	Symbol              entity.Symbol
	StartTime           time.Time
	LatestFundingTimeMs int64
}

func (r *StrategyRunner) prepareFundingRateHistorySyncTargets(ctx context.Context, exchangeName string, items []entity.Symbol, lookbackStart, now time.Time) []fundingRateHistorySyncTarget {
	targets := make([]fundingRateHistorySyncTarget, 0, len(items))
	for _, item := range items {
		target, ok := r.prepareFundingRateHistorySyncTarget(ctx, exchangeName, item, lookbackStart, now)
		if !ok {
			continue
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		leftMissing := targets[i].LatestFundingTimeMs <= 0
		rightMissing := targets[j].LatestFundingTimeMs <= 0
		if leftMissing != rightMissing {
			return leftMissing
		}
		if targets[i].LatestFundingTimeMs != targets[j].LatestFundingTimeMs {
			return targets[i].LatestFundingTimeMs < targets[j].LatestFundingTimeMs
		}
		return targets[i].Symbol.Symbol < targets[j].Symbol.Symbol
	})
	return targets
}

func (r *StrategyRunner) prepareFundingRateHistorySyncTarget(ctx context.Context, exchangeName string, item entity.Symbol, lookbackStart, now time.Time) (fundingRateHistorySyncTarget, bool) {
	target := fundingRateHistorySyncTarget{
		Symbol:    item,
		StartTime: lookbackStart,
	}
	history, err := r.marketRepo.RecentFundingRateHistory(ctx, exchangeName, item.Symbol, lookbackStart, 1)
	if err != nil || len(history) == 0 {
		return target, true
	}

	latestFundingTimeMs := history[0].FundingTimeMs
	target.LatestFundingTimeMs = latestFundingTimeMs
	if latestFundingTimeMs <= 0 {
		return target, true
	}

	intervalHours := item.FundingIntervalHours
	if intervalHours > 0 {
		nextExpectedFundingMs := latestFundingTimeMs + int64(time.Duration(intervalHours)*time.Hour/time.Millisecond)
		if nextExpectedFundingMs > now.UnixMilli() {
			return fundingRateHistorySyncTarget{}, false
		}
	}

	startTime := time.UnixMilli(latestFundingTimeMs + 1).UTC()
	if startTime.Before(lookbackStart) {
		startTime = lookbackStart
	}
	if startTime.After(now) {
		return fundingRateHistorySyncTarget{}, false
	}
	target.StartTime = startTime
	return target, true
}

func (r *StrategyRunner) fundingRateHistoryTargets() map[string][]entity.Symbol {
	out := make(map[string][]entity.Symbol)
	seen := make(map[string]struct{})
	for exchangeName, items := range r.fundingSymbolsByExchange {
		for _, item := range items {
			if !isPerpetualSymbol(item) {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(exchangeName)) + "|" + strings.ToUpper(strings.TrimSpace(item.Symbol))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out[exchangeName] = append(out[exchangeName], item)
		}
	}
	return out
}

func (r *StrategyRunner) fundingRateHistoryRetention() time.Duration {
	retention := r.cfg.SnapshotRetention
	minRetention := r.cfg.FundingRateHistoryLookback + 24*time.Hour
	if minRetention <= 0 {
		minRetention = 31 * 24 * time.Hour
	}
	if retention < minRetention {
		return minRetention
	}
	return retention
}
