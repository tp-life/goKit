package service

import (
	"testing"
	"time"

	"goKit/internal/domain/entity"
)

func TestFundingEventCountUntil(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	next := now + int64(1*time.Hour/time.Millisecond)
	projected := now + int64(4*time.Hour/time.Millisecond)

	count := fundingEventCountUntil(now, projected, next, 1)
	if count != 4 {
		t.Fatalf("expected 4 events, got %d", count)
	}

	count = fundingEventCountUntil(now, projected, next, 8)
	if count != 1 {
		t.Fatalf("expected 1 event with 8h interval, got %d", count)
	}
}

func TestBuildFundingCandidateTimes_ExtendToHoldWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	long := entity.FundingSnapshot{FundingTimeMs: now + int64(8*time.Hour/time.Millisecond), FundingIntervalHours: 8}
	short := entity.FundingSnapshot{FundingTimeMs: now + int64(1*time.Hour/time.Millisecond), FundingIntervalHours: 1}

	times := buildFundingCandidateTimes(now, long, short, 24)
	if len(times) == 0 {
		t.Fatalf("expected candidate times")
	}
	last := times[len(times)-1]
	expectedLast := now + int64(24*time.Hour/time.Millisecond)
	if last != expectedLast {
		t.Fatalf("expected last candidate at 24h, got %d", last)
	}
}

func TestProjectFundingCarry_CanChooseFartherExitToCoverCosts(t *testing.T) {
	r := &StrategyRunner{cfg: Config{HoldHours: 24, FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	// Long leg(8h) pay a relatively small positive funding; short leg(1h) receives funding every hour.
	// In this setting, best carry should be at the far end of hold window instead of first 8h intersection.
	long := entity.FundingSnapshot{FundingRate: 0.00005, FundingTimeMs: nowMs + int64(8*time.Hour/time.Millisecond), FundingIntervalHours: 8}
	short := entity.FundingSnapshot{FundingRate: 0.00010, FundingTimeMs: nowMs + int64(1*time.Hour/time.Millisecond), FundingIntervalHours: 1}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	expectedTime := nowMs + int64(24*time.Hour/time.Millisecond)
	if projection.ProjectedFundingTimeMs != expectedTime {
		t.Fatalf("expected best projection at 24h, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 3 {
		t.Fatalf("expected long leg to have 3 events by 24h, got %d", projection.LongFundingEventCount)
	}
	if projection.ShortFundingEventCount != 24 {
		t.Fatalf("expected short leg to have 24 events by 24h, got %d", projection.ShortFundingEventCount)
	}
}

func TestProjectFundingCarry_MisalignedSchedules(t *testing.T) {
	r := &StrategyRunner{cfg: Config{FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	long := entity.FundingSnapshot{
		FundingRate:          -0.0007,
		FundingTimeMs:        nowMs + int64(4*time.Hour/time.Millisecond),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          -0.0001,
		FundingTimeMs:        nowMs + int64(1*time.Hour/time.Millisecond),
		FundingIntervalHours: 1,
	}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	if projection.ProjectedFundingTimeMs != nowMs+int64(4*time.Hour/time.Millisecond) {
		t.Fatalf("expected best projection at 4h, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 1 {
		t.Fatalf("expected long leg to have 1 event, got %d", projection.LongFundingEventCount)
	}
	if projection.ShortFundingEventCount != 4 {
		t.Fatalf("expected short leg to have 4 events, got %d", projection.ShortFundingEventCount)
	}

	expectedCarry := float64(projection.ShortFundingEventCount)*short.FundingRate - float64(projection.LongFundingEventCount)*long.FundingRate
	if projection.CarryRate != expectedCarry {
		t.Fatalf("expected carry %.8f, got %.8f", expectedCarry, projection.CarryRate)
	}
}

func TestProjectFundingCarryVariants_ReturnsMultipleOrderedWindows(t *testing.T) {
	r := &StrategyRunner{cfg: Config{HoldHours: 8, FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	long := entity.FundingSnapshot{
		FundingRate:          -0.0007,
		FundingTimeMs:        nowMs + int64(4*time.Hour/time.Millisecond),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          -0.0001,
		FundingTimeMs:        nowMs + int64(1*time.Hour/time.Millisecond),
		FundingIntervalHours: 1,
	}

	projections := r.projectFundingCarryVariants(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if len(projections) < 2 {
		t.Fatalf("expected multiple candidate projections, got %d", len(projections))
	}
	if projections[0].CarryRate < projections[1].CarryRate {
		t.Fatalf("expected projections sorted by carry descending")
	}
	found4h := false
	for _, projection := range projections {
		if projection.ProjectedFundingTimeMs == nowMs+int64(4*time.Hour/time.Millisecond) {
			found4h = true
			if projection.LongFundingEventCount != 1 || projection.ShortFundingEventCount != 4 {
				t.Fatalf("expected 4h window to have long=1 short=4, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
			}
		}
	}
	if !found4h {
		t.Fatalf("expected 4h candidate projection to be present")
	}
}

func TestFundingRateEventMultiplier_Decay(t *testing.T) {
	m := fundingRateEventMultiplier(5, 0.6)
	if !(m > 2.3 && m < 2.31) {
		t.Fatalf("expected multiplier around 2.3056, got %.8f", m)
	}
	noDecay := fundingRateEventMultiplier(5, 1)
	if noDecay != 5 {
		t.Fatalf("expected no-decay multiplier 5, got %.8f", noDecay)
	}
}

func TestBlendedFundingRate(t *testing.T) {
	got := blendedFundingRate(0.0010, 0.0004, 0.7)
	want := 0.00082
	if got != want {
		t.Fatalf("expected blended rate %.8f, got %.8f", want, got)
	}
}

func TestBlendedFundingRate_InvalidWeightFallsBackToDefault(t *testing.T) {
	got := blendedFundingRate(0.0010, 0.0004, 2)
	want := 0.00082
	if got != want {
		t.Fatalf("expected default-weight blended rate %.8f, got %.8f", want, got)
	}
}

func TestProjectedLegFundingCarry_UsesCurrentRateForFirstEventOnly(t *testing.T) {
	got := projectedLegFundingCarry(fundingForecast{
		CurrentRate:        0.0010,
		BaselineRate:       0.0004,
		HistoryMean:        0.0004,
		ContinuationDecay:  0.6,
		MeanReversion:      0.2,
		EffectiveFloorRate: -1,
		EffectiveCapRate:   1,
	}, 1)
	if got != 0.0010 {
		t.Fatalf("expected first event to use current rate only, got %.8f", got)
	}
}

func TestProjectedLegFundingCarry_UsesSmoothedRateForLaterEvents(t *testing.T) {
	got := projectedLegFundingCarry(fundingForecast{
		CurrentRate:        0.0010,
		BaselineRate:       0.0004,
		HistoryMean:        0.0004,
		ContinuationDecay:  0.6,
		MeanReversion:      0.2,
		EffectiveFloorRate: -1,
		EffectiveCapRate:   1,
	}, 3)
	want := 0.0010 + 0.0004 + 0.0004*0.6
	if got != want {
		t.Fatalf("expected later events to use smoothed future rate %.8f, got %.8f", want, got)
	}
}

func TestFundingEstimateProfile(t *testing.T) {
	mode, confidence := fundingEstimateProfile(fundingProjection{LongFundingEventCount: 1, ShortFundingEventCount: 1}, fundingForecast{Confidence: "high"}, fundingForecast{Confidence: "high"})
	if mode != "single_cycle_spot" || confidence != "high" {
		t.Fatalf("expected single-cycle high confidence, got mode=%s confidence=%s", mode, confidence)
	}
	mode, confidence = fundingEstimateProfile(
		fundingProjection{LongFundingEventCount: 3, ShortFundingEventCount: 2},
		fundingForecast{Confidence: "guarded"},
		fundingForecast{Confidence: "medium"},
	)
	if mode != "multi_cycle_regime_aware" || confidence != "guarded" {
		t.Fatalf("expected multi-cycle guarded confidence, got mode=%s confidence=%s", mode, confidence)
	}
}

func TestFundingForecast_PredictedRateForEvent_RevertsToMean(t *testing.T) {
	forecast := fundingForecast{
		CurrentRate:        0.0012,
		BaselineRate:       0.0010,
		HistoryMean:        0.0002,
		MeanReversion:      0.5,
		ContinuationDecay:  0.7,
		EffectiveFloorRate: -0.002,
		EffectiveCapRate:   0.002,
	}
	if got := forecast.PredictedRateForEvent(1); got != 0.0012 {
		t.Fatalf("expected event1 to use current rate, got %.8f", got)
	}
	if got := forecast.PredictedRateForEvent(2); !(got < 0.0010 && got > 0.0002) {
		t.Fatalf("expected event2 to revert toward mean, got %.8f", got)
	}
	if got2, got4 := forecast.PredictedRateForEvent(2), forecast.PredictedRateForEvent(4); !(got4 < got2 && got4 > 0.0002) {
		t.Fatalf("expected later events to continue reverting, got event2=%.8f event4=%.8f", got2, got4)
	}
}

func TestVenueFundingClamp_BinanceLikeScaledByInterval(t *testing.T) {
	floor, cap, source := venueFundingClamp("binance", 8)
	if source != "binance_like_cap" {
		t.Fatalf("expected binance clamp source, got %s", source)
	}
	if floor != -0.0075 || cap != 0.0075 {
		t.Fatalf("expected 8h clamp ±0.0075, got floor=%.6f cap=%.6f", floor, cap)
	}

	floor, cap, _ = venueFundingClamp("binance", 1)
	if floor != -0.0009375 || cap != 0.0009375 {
		t.Fatalf("expected 1h scaled clamp ±0.0009375, got floor=%.7f cap=%.7f", floor, cap)
	}
}

func TestEffectiveFundingClamp_UsesVenueIntersection(t *testing.T) {
	item := entity.FundingSnapshot{Exchange: "hyperliquid", FundingIntervalHours: 1}
	floor, cap, source := effectiveFundingClamp(item, 0.0001, 0.0001, 0.01)
	if source != "hyperliquid_cap" {
		t.Fatalf("expected hyperliquid cap source, got %s", source)
	}
	if floor != -0.004 || cap != 0.004 {
		t.Fatalf("expected hyperliquid clamp ±0.004, got floor=%.6f cap=%.6f", floor, cap)
	}
}

func TestEffectiveFundingClamp_BinanceLikeShortIntervalIsAdaptive(t *testing.T) {
	item := entity.FundingSnapshot{Exchange: "binance", FundingIntervalHours: 1}
	floor, cap, source := effectiveFundingClamp(item, 0.0020, 0.0015, 0.0002)
	if source != "binance_like_adaptive_short_interval_cap" {
		t.Fatalf("expected adaptive short-interval clamp source, got %s", source)
	}
	// 不应再被严格压回线性 1h cap ±0.0009375。
	if cap <= 0.0009375 {
		t.Fatalf("expected adaptive cap to remain above strict linear 1h cap, got %.7f", cap)
	}
	if floor >= -0.0009375 {
		t.Fatalf("expected adaptive floor to remain below strict linear 1h floor, got %.7f", floor)
	}
}

func TestFundingForecast_PredictedRateForEvent_RespectsClamp(t *testing.T) {
	forecast := fundingForecast{
		CurrentRate:        0.0100,
		BaselineRate:       0.0090,
		HistoryMean:        0.0010,
		MeanReversion:      0.1,
		ContinuationDecay:  0.8,
		EffectiveFloorRate: -0.002,
		EffectiveCapRate:   0.002,
		ClampSource:        "test_cap",
	}
	if got := forecast.PredictedRateForEvent(2); got != 0.002 {
		t.Fatalf("expected event2 to be clamped at 0.002, got %.6f", got)
	}
}

func TestProjectedLegFundingCarry_RegimeAwarePath(t *testing.T) {
	forecast := fundingForecast{
		CurrentRate:        0.0010,
		BaselineRate:       0.0008,
		HistoryMean:        0.0002,
		MeanReversion:      0.5,
		ContinuationDecay:  0.5,
		EffectiveFloorRate: -0.002,
		EffectiveCapRate:   0.002,
	}
	got := projectedLegFundingCarry(forecast, 3)
	// 事件路径：
	// e1 = 0.0010
	// e2 = 0.0005, 权重 1
	// e3 = 0.00035, 权重 0.5
	want := 0.0010 + 0.0005 + 0.00035*0.5
	if got != want {
		t.Fatalf("expected regime-aware path carry %.8f, got %.8f", want, got)
	}
}

func TestFundingRegimeProfile(t *testing.T) {
	regime, confidence, meanReversion, decayTilt := fundingRegimeProfile(3.0, 0.001, 0.0002)
	if regime != "extreme_positive_reversion" || confidence != "medium" {
		t.Fatalf("unexpected extreme positive regime=%s confidence=%s", regime, confidence)
	}
	if !(meanReversion > 0.6 && decayTilt < 0.6) {
		t.Fatalf("expected stronger reversion and lower decay tilt, got reversion=%.2f tilt=%.2f", meanReversion, decayTilt)
	}
	regime, confidence, _, _ = fundingRegimeProfile(0.1, 0.0003, 0.0002)
	if regime != "stable_carry" || confidence != "high" {
		t.Fatalf("unexpected stable regime=%s confidence=%s", regime, confidence)
	}
}

func spotForecast(currentRate, decay float64) fundingForecast {
	return fundingForecast{
		CurrentRate:        currentRate,
		BaselineRate:       currentRate,
		HistoryMean:        currentRate,
		Regime:             "spot_only",
		Confidence:         "high",
		MeanReversion:      0.2,
		ContinuationDecay:  decay,
		EffectiveFloorRate: currentRate,
		EffectiveCapRate:   currentRate,
	}
}

func TestAllowedBasisThresholdBps_GrowsWithWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{MaxSpreadBps: 10, DynamicMaxSpreadMultiplier: 2, DynamicMaxSpreadReferenceHours: 20}}
	shortWindow := r.allowedBasisThresholdBps(fundingProjection{FundingWindowHours: 2})
	if shortWindow <= 10 || shortWindow >= 12 {
		t.Fatalf("expected short window threshold around 11, got %.4f", shortWindow)
	}
	longWindow := r.allowedBasisThresholdBps(fundingProjection{FundingWindowHours: 20})
	if longWindow != 20 {
		t.Fatalf("expected long window threshold at cap 20, got %.4f", longWindow)
	}
}

func TestAllowedPlanBasisThresholdBps_UsesOpportunityValue(t *testing.T) {
	r := &StrategyRunner{cfg: Config{MaxSpreadBps: 10, DynamicMaxSpreadMultiplier: 2, DynamicMaxSpreadReferenceHours: 20}}
	opp := entity.Opportunity{MaxAllowedBasisBps: 18, FundingWindowHours: 2}
	got := r.allowedPlanBasisThresholdBps(opp)
	if got != 18 {
		t.Fatalf("expected plan threshold to respect opportunity value 18, got %.4f", got)
	}
}

func TestEstimateExecutionPenalty_ShortWindowKeepsMoreBasisRisk(t *testing.T) {
	r := &StrategyRunner{cfg: Config{SlippageBps: 2}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	short := r.estimateExecutionPenalty(now, "BTC", "binance", "aster", fundingProjection{FundingWindowHours: 1, LongFundingEventCount: 1, ShortFundingEventCount: 1}, 10)
	long := r.estimateExecutionPenalty(now, "BTC", "binance", "aster", fundingProjection{FundingWindowHours: 12, LongFundingEventCount: 1, ShortFundingEventCount: 1}, 10)
	if short.ExitPenaltyBps <= long.ExitPenaltyBps {
		t.Fatalf("expected short-window exit penalty %.4f to exceed long-window penalty %.4f", short.ExitPenaltyBps, long.ExitPenaltyBps)
	}
}

func TestEstimateExecutionPenalty_MoreEventsIncreaseComplexityPenalty(t *testing.T) {
	r := &StrategyRunner{cfg: Config{SlippageBps: 2}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	single := r.estimateExecutionPenalty(now, "BTC", "binance", "aster", fundingProjection{FundingWindowHours: 4, LongFundingEventCount: 1, ShortFundingEventCount: 1}, 8)
	multi := r.estimateExecutionPenalty(now, "BTC", "binance", "aster", fundingProjection{FundingWindowHours: 4, LongFundingEventCount: 3, ShortFundingEventCount: 2}, 8)
	if multi.HedgeRollbackBps <= single.HedgeRollbackBps {
		t.Fatalf("expected multi-event hedge penalty %.4f to exceed single-event penalty %.4f", multi.HedgeRollbackBps, single.HedgeRollbackBps)
	}
}

func TestEstimateExecutionPenalty_TimeBucketAndExchangeMultipliers(t *testing.T) {
	r := &StrategyRunner{cfg: Config{SlippageBps: 2}, venues: defaultVenueProfileRegistry()}
	hot := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	normal := time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC)
	binance := r.estimateExecutionPenalty(normal, "BTC", "binance", "binance", fundingProjection{FundingWindowHours: 4, LongFundingEventCount: 1, ShortFundingEventCount: 1}, 5)
	hyper := r.estimateExecutionPenalty(normal, "BTC", "hyperliquid", "hyperliquid", fundingProjection{FundingWindowHours: 4, LongFundingEventCount: 1, ShortFundingEventCount: 1}, 5)
	hotPenalty := r.estimateExecutionPenalty(hot, "BTC", "binance", "binance", fundingProjection{FundingWindowHours: 4, LongFundingEventCount: 1, ShortFundingEventCount: 1}, 5)
	if hyper.EntryPenaltyBps <= binance.EntryPenaltyBps {
		t.Fatalf("expected hyperliquid entry penalty %.4f to exceed binance %.4f", hyper.EntryPenaltyBps, binance.EntryPenaltyBps)
	}
	if hotPenalty.EntryPenaltyBps <= binance.EntryPenaltyBps {
		t.Fatalf("expected hot bucket penalty %.4f to exceed normal %.4f", hotPenalty.EntryPenaltyBps, binance.EntryPenaltyBps)
	}
	if hotPenalty.ExperienceBucket != "funding_window_hot" {
		t.Fatalf("expected funding_window_hot bucket, got %s", hotPenalty.ExperienceBucket)
	}
}

func TestVenueProfileRegistry_DefaultFundingClampAndPenalty(t *testing.T) {
	registry := defaultVenueProfileRegistry()

	floor, cap, source := registry.FundingClamp("aster", 1)
	if source != "binance_like_cap" {
		t.Fatalf("expected binance-like source for aster, got %s", source)
	}
	if floor != -0.0009375 || cap != 0.0009375 {
		t.Fatalf("expected aster 1h clamp ±0.0009375, got floor=%.7f cap=%.7f", floor, cap)
	}

	if got := registry.ExecutionPenaltyMultiplier("hyperliquid"); got != 1.15 {
		t.Fatalf("expected hyperliquid penalty multiplier 1.15, got %.2f", got)
	}
	if got := registry.ExecutionPenaltyMultiplier("unknown"); got != 1.05 {
		t.Fatalf("expected default penalty multiplier 1.05, got %.2f", got)
	}
}
