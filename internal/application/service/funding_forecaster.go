package service

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

// FundingForecaster 负责把“当前 funding 快照 + 历史序列”转成未来 funding 事件的预测路径。
//
// 设计目标：
// 1. 保留当前项目已经做对的“事件时间轴”框架；
// 2. 不退回简单 hourly spread；
// 3. 用轻量、可解释的 regime 模型替代“所有后续事件都共享同一个 futureRate”的近似；
// 4. 在没有历史数据时自动退化为当前快照，保证策略可用性。
type FundingForecaster interface {
	Forecast(ctx context.Context, now time.Time, item entity.FundingSnapshot) fundingForecast
}

type fundingForecast struct {
	CurrentRate        float64
	BaselineRate       float64
	HistoryMean        float64
	HistoryStdDev      float64
	ZScore             float64
	Regime             string
	Confidence         string
	MeanReversion      float64
	ContinuationDecay  float64
	EffectiveFloorRate float64
	EffectiveCapRate   float64
	ClampSource        string
}

func (f fundingForecast) PredictedRateForEvent(eventIndex int) float64 {
	if eventIndex <= 1 {
		return f.CurrentRate
	}
	reversion := clampFloat(f.MeanReversion, 0.05, 0.95)
	step := float64(eventIndex - 1)
	projected := f.HistoryMean + (f.BaselineRate-f.HistoryMean)*math.Pow(1-reversion, step)
	projected = clampFloat(projected, f.EffectiveFloorRate, f.EffectiveCapRate)
	return projected
}

type fundingStats struct {
	mean      float64
	stdDev    float64
	recentAvg float64
	count     int
	expiresAt time.Time
}

type RegimeAwareFundingForecaster struct {
	cfg        Config
	marketRepo repository.MarketDataRepository

	mu    sync.RWMutex
	cache map[string]fundingStats
}

func NewFundingForecaster(cfg Config, marketRepo repository.MarketDataRepository) FundingForecaster {
	return &RegimeAwareFundingForecaster{
		cfg:        cfg.normalize(),
		marketRepo: marketRepo,
		cache:      make(map[string]fundingStats),
	}
}

func (f *RegimeAwareFundingForecaster) Forecast(ctx context.Context, now time.Time, item entity.FundingSnapshot) fundingForecast {
	stats, ok := f.historyStats(ctx, now, item)
	if !ok {
		decay := normalizedContinuationDecay(f.cfg.FundingRateContinuationDecay)
		return fundingForecast{
			CurrentRate:        item.FundingRate,
			BaselineRate:       item.FundingRate,
			HistoryMean:        item.FundingRate,
			HistoryStdDev:      0,
			ZScore:             0,
			Regime:             "spot_only",
			Confidence:         "low",
			MeanReversion:      0.20,
			ContinuationDecay:  decay,
			EffectiveFloorRate: item.FundingRate,
			EffectiveCapRate:   item.FundingRate,
			ClampSource:        "spot_only",
		}
	}

	stdDev := stats.stdDev
	if stdDev <= 1e-9 {
		stdDev = math.Abs(stats.mean) * 0.25
		if stdDev <= 1e-9 {
			stdDev = 0.0001
		}
	}
	zScore := (item.FundingRate - stats.mean) / stdDev
	regime, confidence, meanReversion, decayTilt := fundingRegimeProfile(zScore, item.FundingRate, stats.mean)

	// baselineRate 不直接取当前值，也不直接取均值，而是引入 recentAvg 做“短期状态 + 长期均值”折中。
	baseline := weightedBlend(
		item.FundingRate,
		weightedBlend(stats.recentAvg, stats.mean, 0.65),
		f.cfg.FundingSmoothingCurrentWeight,
	)
	floorRate, capRate, clampSource := effectiveFundingClamp(item, item.FundingRate, stats.mean, stdDev)
	decay := normalizedContinuationDecay(f.cfg.FundingRateContinuationDecay) * decayTilt
	decay = clampFloat(decay, 0.20, 0.98)

	return fundingForecast{
		CurrentRate:        item.FundingRate,
		BaselineRate:       clampFloat(baseline, floorRate, capRate),
		HistoryMean:        stats.mean,
		HistoryStdDev:      stats.stdDev,
		ZScore:             zScore,
		Regime:             regime,
		Confidence:         confidence,
		MeanReversion:      meanReversion,
		ContinuationDecay:  decay,
		EffectiveFloorRate: floorRate,
		EffectiveCapRate:   capRate,
		ClampSource:        clampSource,
	}
}

func (f *RegimeAwareFundingForecaster) historyStats(ctx context.Context, now time.Time, item entity.FundingSnapshot) (fundingStats, bool) {
	key := strings.ToLower(item.Exchange) + "|" + strings.ToUpper(item.Symbol)
	f.mu.RLock()
	cached, ok := f.cache[key]
	f.mu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return cached, true
	}
	if f.marketRepo == nil {
		return fundingStats{}, false
	}
	lookback := f.cfg.FundingHistoryLookback
	if lookback <= 0 {
		return fundingStats{}, false
	}
	history, err := f.marketRepo.RecentFundingSnapshots(ctx, item.Exchange, item.Symbol, now.Add(-lookback), 48)
	if err != nil || len(history) == 0 {
		return fundingStats{}, false
	}

	values := make([]float64, 0, len(history)+1)
	values = append(values, item.FundingRate)
	for _, snap := range history {
		if !math.IsNaN(snap.FundingRate) && !math.IsInf(snap.FundingRate, 0) {
			values = append(values, snap.FundingRate)
		}
	}
	if len(values) == 0 {
		return fundingStats{}, false
	}

	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))

	variance := 0.0
	for _, value := range values {
		diff := value - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	stdDev := math.Sqrt(variance)

	recentN := minInt(4, len(values))
	recentAvg := 0.0
	for i := 0; i < recentN; i++ {
		recentAvg += values[i]
	}
	recentAvg /= float64(recentN)

	ttl := f.cfg.OpportunityCalcInterval * 6
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if ttl > 30*time.Second {
		ttl = 30 * time.Second
	}
	stats := fundingStats{
		mean:      mean,
		stdDev:    stdDev,
		recentAvg: recentAvg,
		count:     len(values),
		expiresAt: now.Add(ttl),
	}
	f.mu.Lock()
	f.cache[key] = stats
	f.mu.Unlock()
	return stats, true
}

func fundingRegimeProfile(zScore, currentRate, mean float64) (regime, confidence string, meanReversion, decayTilt float64) {
	switch {
	case zScore >= 2.5:
		return "extreme_positive_reversion", "medium", 0.70, 0.55
	case zScore >= 1.2:
		return "elevated_positive_reversion", "medium", 0.55, 0.75
	case zScore <= -2.5:
		return "extreme_negative_reversion", "medium", 0.70, 0.55
	case zScore <= -1.2:
		return "elevated_negative_reversion", "medium", 0.55, 0.75
	default:
		if sameFundingSign(currentRate, mean) {
			return "stable_carry", "high", 0.30, 1.00
		}
		return "sign_flip_risk", "guarded", 0.45, 0.70
	}
}

func effectiveFundingClamp(item entity.FundingSnapshot, currentRate, historyMean, historyStdDev float64) (floorRate, capRate float64, source string) {
	floorRate = historyMean - historyStdDev*3
	capRate = historyMean + historyStdDev*3
	source = "history_band"
	if floorRate > capRate {
		floorRate, capRate = capRate, floorRate
	}

	venueFloor, venueCap, venueSource := venueFundingClamp(item.Exchange, item.FundingIntervalHours)
	if venueSource == "" {
		return floorRate, capRate, source
	}
	if adaptiveFloor, adaptiveCap, adaptiveSource, widened := adaptiveShortIntervalClamp(item.Exchange, item.FundingIntervalHours, currentRate, historyMean, historyStdDev, venueFloor, venueCap); widened {
		// 对 Binance-like 的 1h / 4h 特殊 funding 合约，不再用严格交集去压缩信号。
		// 这些短周期 funding 本身就是 alpha 来源；如果简单把 8h cap 线性缩成 1h/4h，
		// 再和历史带取交集，会让很多真实存在的高 funding 机会被模型过度抹平。
		floorRate = math.Min(floorRate, adaptiveFloor)
		capRate = math.Max(capRate, adaptiveCap)
		floorRate = math.Max(floorRate, -0.0075)
		capRate = math.Min(capRate, 0.0075)
		return floorRate, capRate, adaptiveSource
	}
	// 采用更严格的交集区间：既尊重历史统计带，也尊重交易所机制上限。
	floorRate = math.Max(floorRate, venueFloor)
	capRate = math.Min(capRate, venueCap)
	if floorRate > capRate {
		center := clampFloat(historyMean, venueFloor, venueCap)
		span := math.Max(math.Abs(historyStdDev), 0.00005)
		floorRate = math.Max(venueFloor, center-span)
		capRate = math.Min(venueCap, center+span)
	}
	return floorRate, capRate, venueSource
}

func adaptiveShortIntervalClamp(exchangeName string, intervalHours int, currentRate, historyMean, historyStdDev, venueFloor, venueCap float64) (floorRate, capRate float64, source string, widened bool) {
	if !(strings.EqualFold(exchangeName, "binance") || strings.EqualFold(exchangeName, "aster")) {
		return 0, 0, "", false
	}
	if intervalHours <= 0 || intervalHours >= 8 {
		return 0, 0, "", false
	}

	fullWindowCap := 0.0075
	dynamicCap := math.Max(math.Max(
		math.Abs(currentRate)*1.25,
		math.Abs(historyMean)+historyStdDev*4),
		math.Abs(venueCap),
	)
	dynamicCap = math.Min(dynamicCap, fullWindowCap)
	if dynamicCap <= math.Abs(venueCap) {
		return 0, 0, "", false
	}
	return -dynamicCap, dynamicCap, "binance_like_adaptive_short_interval_cap", true
}

func venueFundingClamp(exchangeName string, intervalHours int) (floorRate, capRate float64, source string) {
	intervalHours = maxInt(intervalHours, 1)
	switch strings.ToLower(strings.TrimSpace(exchangeName)) {
	case "binance", "aster":
		// Binance-like perp 常见 funding 上下限约在每 8h ±0.75%。
		// 对 1h 周期按时间比例缩放，避免把 Hyperliquid 这类高频结算路径过度放宽。
		perHourCap := 0.0075 / 8.0
		cap := perHourCap * float64(intervalHours)
		return -cap, cap, "binance_like_cap"
	case "hyperliquid":
		// Hyperliquid 为 1h 结算，实盘中 funding 往往明显小于 CEX 的 8h cap；
		// 这里给出保守的每小时上下限，防止极端瞬时 funding 被机械外推到多轮。
		cap := 0.0040
		return -cap, cap, "hyperliquid_cap"
	default:
		return 0, 0, ""
	}
}

func sameFundingSign(a, b float64) bool {
	return (a >= 0 && b >= 0) || (a <= 0 && b <= 0)
}

func weightedBlend(primary, secondary, primaryWeight float64) float64 {
	if primaryWeight < 0 || primaryWeight > 1 {
		primaryWeight = 0.7
	}
	return primary*primaryWeight + secondary*(1-primaryWeight)
}

func normalizedContinuationDecay(v float64) float64 {
	if v <= 0 || v > 1 {
		return 0.6
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
