package service

import (
	"context"
	"math"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
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

func TestBuildFundingCandidateTimes_StrictlyRespectsHoldWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 16, 50, 0, 0, time.UTC).UnixMilli()
	long := entity.FundingSnapshot{FundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(), FundingIntervalHours: 4}
	short := entity.FundingSnapshot{FundingTimeMs: time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC).UnixMilli(), FundingIntervalHours: 1}

	times := buildFundingCandidateTimes(now, long, short, 10.0/60.0)
	if len(times) != 1 {
		t.Fatalf("expected exactly 1 candidate time within 10m hold window, got %d", len(times))
	}
	if want := time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC).UnixMilli(); times[0] != want {
		t.Fatalf("expected only 17:00 funding candidate, got %d", times[0])
	}
}

func TestBuildFundingCandidateTimes_IncludesSharedSettlementInsideHoldWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 19, 50, 0, 0, time.UTC).UnixMilli()
	long := entity.FundingSnapshot{FundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(), FundingIntervalHours: 4}
	short := entity.FundingSnapshot{FundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(), FundingIntervalHours: 1}

	times := buildFundingCandidateTimes(now, long, short, 10.0/60.0)
	if len(times) != 1 {
		t.Fatalf("expected shared 20:00 settlement to be included once, got %d candidates", len(times))
	}
	if want := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(); times[0] != want {
		t.Fatalf("expected only 20:00 funding candidate, got %d", times[0])
	}
}

func TestProjectFundingCarry_PrefersEarliestWindowForPrimaryProjection(t *testing.T) {
	r := &StrategyRunner{cfg: Config{HoldHours: 24, FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	// 主投影现在应该严格对应“当前窗口里最早兑现的事件”，
	// 而不是 hold 窗口里 carry 更大的更远退出点。
	long := entity.FundingSnapshot{FundingRate: 0.00005, FundingTimeMs: nowMs + int64(8*time.Hour/time.Millisecond), FundingIntervalHours: 8}
	short := entity.FundingSnapshot{FundingRate: 0.00010, FundingTimeMs: nowMs + int64(1*time.Hour/time.Millisecond), FundingIntervalHours: 1}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	expectedTime := nowMs + int64(1*time.Hour/time.Millisecond)
	if projection.ProjectedFundingTimeMs != expectedTime {
		t.Fatalf("expected primary projection at 1h, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 0 {
		t.Fatalf("expected long leg to have 0 events by 1h, got %d", projection.LongFundingEventCount)
	}
	if projection.ShortFundingEventCount != 1 {
		t.Fatalf("expected short leg to have 1 event by 1h, got %d", projection.ShortFundingEventCount)
	}
}

func TestProjectFundingCarry_OnlyCountsFundingEventsInsideHoldWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{HoldHours: 10.0 / 60.0, FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 16, 50, 0, 0, time.UTC)

	long := entity.FundingSnapshot{
		FundingRate:          0.0002,
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          0.0001,
		FundingTimeMs:        time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	if want := time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC).UnixMilli(); projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected best projection at 17:00, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 0 || projection.ShortFundingEventCount != 1 {
		t.Fatalf("expected only short leg funding inside window, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
	}
	if projection.CarryRate != short.FundingRate {
		t.Fatalf("expected carry to equal short-leg funding %.8f, got %.8f", short.FundingRate, projection.CarryRate)
	}
}

func TestProjectFundingCarry_CountsBothLegsWhenSharedSettlementInsideHoldWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{HoldHours: 10.0 / 60.0, FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 19, 50, 0, 0, time.UTC)

	long := entity.FundingSnapshot{
		FundingRate:          0.0002,
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          0.0006,
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	if projection.LongFundingEventCount != 1 || projection.ShortFundingEventCount != 1 {
		t.Fatalf("expected both legs to settle once at 20:00, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
	}
	if want := short.FundingRate - long.FundingRate; projection.CarryRate != want {
		t.Fatalf("expected carry %.8f, got %.8f", want, projection.CarryRate)
	}
}

func TestProjectFundingCarry_MisalignedSchedulesUsesCurrentEventWindow(t *testing.T) {
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
	if projection.ProjectedFundingTimeMs != nowMs+int64(1*time.Hour/time.Millisecond) {
		t.Fatalf("expected primary projection at 1h, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 0 {
		t.Fatalf("expected long leg to have 0 events, got %d", projection.LongFundingEventCount)
	}
	if projection.ShortFundingEventCount != 1 {
		t.Fatalf("expected short leg to have 1 event, got %d", projection.ShortFundingEventCount)
	}

	expectedCarry := short.FundingRate
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
	if projections[0].ProjectedFundingTimeMs > projections[1].ProjectedFundingTimeMs {
		t.Fatalf("expected projections sorted by projected funding time ascending")
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

func TestBuildFundingProjections_SortsEarliestWindowFirstForRevalidation(t *testing.T) {
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

	projections := buildFundingProjections(now, 8, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if len(projections) == 0 {
		t.Fatalf("expected projections")
	}
	if want := nowMs + int64(1*time.Hour/time.Millisecond); projections[0].ProjectedFundingTimeMs != want {
		t.Fatalf("expected earliest projection first at 1h, got %d", projections[0].ProjectedFundingTimeMs)
	}
}

func TestProjectFundingCarry_FourHourHoldStillUsesCurrentEventWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{HoldHours: 4, FundingRateContinuationDecay: 1}}
	now := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC)

	long := entity.FundingSnapshot{
		FundingRate:          0.0001,
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          0.0004,
		FundingTimeMs:        time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	if want := time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(); projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected current-event projection at 19:00, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 0 || projection.ShortFundingEventCount != 1 {
		t.Fatalf("expected only 19:00 short-leg event in primary projection, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
	}
	if projection.CarryRate != short.FundingRate {
		t.Fatalf("expected carry to equal 19:00 short-leg funding %.8f, got %.8f", short.FundingRate, projection.CarryRate)
	}
}

func TestEvaluateOpportunityProjectionWindows_SelectsHighestNetWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{SlippageBps: 0, SafetyBufferUSDT: 0}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	windows := r.evaluateOpportunityProjectionWindows(
		now,
		"BTC",
		"binance",
		"aster",
		[]fundingProjection{
			{ProjectedFundingTimeMs: now.Add(1 * time.Hour).UnixMilli(), FundingWindowHours: 1, CarryRate: 0.0010, CarryRateHourlyEquivalent: 0.0010, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(4 * time.Hour).UnixMilli(), FundingWindowHours: 4, CarryRate: 0.0030, CarryRateHourlyEquivalent: 0.00075, LongFundingEventCount: 1, ShortFundingEventCount: 1},
		},
		1000,
		1,
		1,
		0,
	)
	best, ok := selectBestOpportunityProjection(r.cfg, windows)
	if !ok {
		t.Fatal("expected best projection to be selected")
	}
	if want := now.Add(4 * time.Hour).UnixMilli(); best.Projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected 4h window to win by net pnl, got %d", best.Projection.ProjectedFundingTimeMs)
	}
	if !windows[1].Detail.IsBestProjection || windows[0].Detail.IsBestProjection {
		t.Fatalf("expected only second window to be marked best, got %+v", windows)
	}
}

func TestProjectFundingCarry_LatestProfitablePrefersLatestPositiveWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		HoldHours:                    4,
		HoldSelectionMode:            HoldSelectionModeLatestProfitable,
		FundingRateContinuationDecay: 1,
	}}
	now := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC)

	long := entity.FundingSnapshot{
		FundingRate:          0.0001,
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          0.0004,
		FundingTimeMs:        time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	if want := time.Date(2026, 1, 1, 22, 0, 0, 0, time.UTC).UnixMilli(); projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected latest profitable projection at 22:00, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 1 || projection.ShortFundingEventCount != 4 {
		t.Fatalf("expected latest profitable window to include long=1 short=4 events, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
	}
}

func TestProjectFundingCarry_StrictTargetPinsLatestWindowEvenWhenCarryTurnsNegative(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		HoldHours:                    4,
		HoldSelectionMode:            HoldSelectionModeStrictTarget,
		FundingRateContinuationDecay: 1,
	}}
	now := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC)

	long := entity.FundingSnapshot{
		FundingRate:          0.0020,
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingRate:          0.0004,
		FundingTimeMs:        time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	projection, ok := r.projectFundingCarry(now, long, spotForecast(long.FundingRate, 1), short, spotForecast(short.FundingRate, 1))
	if !ok {
		t.Fatalf("expected projection to be valid")
	}
	if want := time.Date(2026, 1, 1, 22, 0, 0, 0, time.UTC).UnixMilli(); projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected strict target projection at 22:00, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.CarryRate >= 0 {
		t.Fatalf("expected strict target latest window to preserve negative carry, got %.8f", projection.CarryRate)
	}
}

func TestEvaluateOpportunityProjectionWindows_LatestProfitablePicksLatestQualifiedWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		HoldSelectionMode: HoldSelectionModeLatestProfitable,
		MinNetPNL:         1.5,
		SlippageBps:       0,
		SafetyBufferUSDT:  0,
	}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	windows := r.evaluateOpportunityProjectionWindows(
		now,
		"BTC",
		"binance",
		"aster",
		[]fundingProjection{
			{ProjectedFundingTimeMs: now.Add(1 * time.Hour).UnixMilli(), FundingWindowHours: 1, CarryRate: 0.0018, CarryRateHourlyEquivalent: 0.0018, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(), FundingWindowHours: 2, CarryRate: 0.0022, CarryRateHourlyEquivalent: 0.0011, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(4 * time.Hour).UnixMilli(), FundingWindowHours: 4, CarryRate: 0.0017, CarryRateHourlyEquivalent: 0.000425, LongFundingEventCount: 1, ShortFundingEventCount: 1},
		},
		1000,
		0,
		0,
		0,
	)

	best, ok := selectBestOpportunityProjection(r.cfg, windows)
	if !ok {
		t.Fatal("expected latest profitable projection to be selected")
	}
	if want := now.Add(4 * time.Hour).UnixMilli(); best.Projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected latest qualified window at 4h, got %d", best.Projection.ProjectedFundingTimeMs)
	}
	if !windows[2].Detail.IsBestProjection || windows[0].Detail.IsBestProjection || windows[1].Detail.IsBestProjection {
		t.Fatalf("expected only latest qualified window to be marked best, got %+v", windows)
	}
}

func TestEvaluateOpportunityProjectionWindows_LatestProfitableFallsBackToBestNet(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		HoldSelectionMode: HoldSelectionModeLatestProfitable,
		MinNetPNL:         1.5,
		SlippageBps:       0,
		SafetyBufferUSDT:  0,
	}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	windows := r.evaluateOpportunityProjectionWindows(
		now,
		"BTC",
		"binance",
		"aster",
		[]fundingProjection{
			{ProjectedFundingTimeMs: now.Add(1 * time.Hour).UnixMilli(), FundingWindowHours: 1, CarryRate: 0.0010, CarryRateHourlyEquivalent: 0.0010, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(), FundingWindowHours: 2, CarryRate: 0.0014, CarryRateHourlyEquivalent: 0.0007, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(4 * time.Hour).UnixMilli(), FundingWindowHours: 4, CarryRate: 0.0012, CarryRateHourlyEquivalent: 0.0003, LongFundingEventCount: 1, ShortFundingEventCount: 1},
		},
		1000,
		0,
		0,
		0,
	)

	best, ok := selectBestOpportunityProjection(r.cfg, windows)
	if !ok {
		t.Fatal("expected fallback projection to be selected")
	}
	if want := now.Add(2 * time.Hour).UnixMilli(); best.Projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected fallback to highest-net window at 2h, got %d", best.Projection.ProjectedFundingTimeMs)
	}
	if !windows[1].Detail.IsBestProjection || windows[0].Detail.IsBestProjection || windows[2].Detail.IsBestProjection {
		t.Fatalf("expected fallback best-net window to be marked best, got %+v", windows)
	}
}

func TestEvaluateOpportunityProjectionWindows_StrictTargetPinsLatestWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		HoldSelectionMode: HoldSelectionModeStrictTarget,
		MinNetPNL:         1.5,
		SlippageBps:       0,
		SafetyBufferUSDT:  0,
	}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	windows := r.evaluateOpportunityProjectionWindows(
		now,
		"BTC",
		"binance",
		"aster",
		[]fundingProjection{
			{ProjectedFundingTimeMs: now.Add(1 * time.Hour).UnixMilli(), FundingWindowHours: 1, CarryRate: 0.0022, CarryRateHourlyEquivalent: 0.0022, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(4 * time.Hour).UnixMilli(), FundingWindowHours: 4, CarryRate: 0.0010, CarryRateHourlyEquivalent: 0.00025, LongFundingEventCount: 1, ShortFundingEventCount: 1},
		},
		1000,
		0,
		0,
		0,
	)

	best, ok := selectBestOpportunityProjection(r.cfg, windows)
	if !ok {
		t.Fatal("expected strict target projection to be selected")
	}
	if want := now.Add(4 * time.Hour).UnixMilli(); best.Projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected strict target to pin 4h window, got %d", best.Projection.ProjectedFundingTimeMs)
	}
	if best.Detail.NetExpectedPNL >= r.cfg.MinNetPNL {
		t.Fatalf("expected strict target example to stay below min net pnl, got %.4f", best.Detail.NetExpectedPNL)
	}
	if !windows[1].Detail.IsBestProjection || windows[0].Detail.IsBestProjection {
		t.Fatalf("expected latest window to be marked best under strict_target, got %+v", windows)
	}
}

func TestEvaluateOpportunityProjectionWindows_RepricesPenaltiesPerWindow(t *testing.T) {
	r := &StrategyRunner{cfg: Config{SlippageBps: 2, SafetyBufferUSDT: 0}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	windows := r.evaluateOpportunityProjectionWindows(
		now,
		"BTC",
		"binance",
		"aster",
		[]fundingProjection{
			{ProjectedFundingTimeMs: now.Add(1 * time.Hour).UnixMilli(), FundingWindowHours: 1, CarryRate: 0.0020, CarryRateHourlyEquivalent: 0.0020, LongFundingEventCount: 1, ShortFundingEventCount: 1},
			{ProjectedFundingTimeMs: now.Add(8 * time.Hour).UnixMilli(), FundingWindowHours: 8, CarryRate: 0.0020, CarryRateHourlyEquivalent: 0.00025, LongFundingEventCount: 3, ShortFundingEventCount: 3},
		},
		1000,
		0,
		0,
		0,
	)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	if windows[1].ExecutionPenaltyBps <= windows[0].ExecutionPenaltyBps {
		t.Fatalf("expected long window penalty %.4f to exceed short window penalty %.4f", windows[1].ExecutionPenaltyBps, windows[0].ExecutionPenaltyBps)
	}
	if windows[1].Detail.NetExpectedPNL >= windows[0].Detail.NetExpectedPNL {
		t.Fatalf("expected long window net pnl %.4f to be lower after repricing, got short=%.4f long=%.4f", windows[1].Detail.NetExpectedPNL, windows[0].Detail.NetExpectedPNL, windows[1].Detail.NetExpectedPNL)
	}
}

func TestSelectBetterDirectionCandidate_UsesFinalNetAcrossDirections(t *testing.T) {
	cfg := Config{HoldSelectionMode: HoldSelectionModeLatestProfitable}

	left := evaluatedDirectionCandidate{
		LongExchange:  "aster",
		ShortExchange: "binance",
		PrimaryWindow: opportunityProjectionEvaluation{
			Projection: fundingProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0072,
			},
			Detail: entity.OpportunityProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0072,
				NetExpectedPNL:         6.0,
			},
		},
	}
	right := evaluatedDirectionCandidate{
		LongExchange:  "binance",
		ShortExchange: "aster",
		PrimaryWindow: opportunityProjectionEvaluation{
			Projection: fundingProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0050,
			},
			Detail: entity.OpportunityProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0050,
				NetExpectedPNL:         7.5,
			},
		},
	}

	if !selectBetterDirectionCandidate(cfg, right, left) {
		t.Fatalf("expected reverse direction to win when its final selected window has higher net pnl")
	}
	if selectBetterDirectionCandidate(cfg, left, right) {
		t.Fatalf("expected lower-net direction to lose even when its carry rate is higher")
	}
}

func TestSelectBetterDirectionCandidate_PrefersLaterWindowOnNetTieInLatestMode(t *testing.T) {
	cfg := Config{HoldSelectionMode: HoldSelectionModeLatestProfitable}

	earlier := evaluatedDirectionCandidate{
		LongExchange:  "aster",
		ShortExchange: "binance",
		PrimaryWindow: opportunityProjectionEvaluation{
			Projection: fundingProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0060,
			},
			Detail: entity.OpportunityProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0060,
				NetExpectedPNL:         6.0,
			},
		},
	}
	later := evaluatedDirectionCandidate{
		LongExchange:  "binance",
		ShortExchange: "aster",
		PrimaryWindow: opportunityProjectionEvaluation{
			Projection: fundingProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0060,
			},
			Detail: entity.OpportunityProjection{
				ProjectedFundingTimeMs: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
				CarryRate:              0.0060,
				NetExpectedPNL:         6.0,
			},
		},
	}

	if !selectBetterDirectionCandidate(cfg, later, earlier) {
		t.Fatalf("expected later window to win tie-break under latest_profitable")
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

func TestBuildRollingDirectionalFundingPlan_UsesOnlyFirstRealSegmentBeforeBoundary(t *testing.T) {
	cfg := Config{
		StrategyMode: StrategyModeRollingCycleAligned,
		HoldHours:    4,
		RollingEntryPathRequireConsistentDirection:     true,
		RollingAllowIntermediateForecastBeforeBoundary: true,
		RollingForbidBoundaryForecast:                  true,
	}.normalize()

	now := time.Date(2026, 1, 1, 13, 48, 0, 0, time.UTC)
	longFunding := entity.FundingSnapshot{
		Exchange:             "aster",
		FundingRate:          -0.00356,
		FundingTimeMs:        time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}
	shortFunding := entity.FundingSnapshot{
		Exchange:             "binance",
		FundingRate:          -0.012,
		FundingTimeMs:        time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}

	plan := buildRollingDirectionalFundingPlan(
		cfg,
		now,
		"aster",
		longFunding,
		spotForecast(longFunding.FundingRate, 1),
		"binance",
		shortFunding,
		spotForecast(shortFunding.FundingRate, 1),
	)

	if len(plan.Segments) != 1 {
		t.Fatalf("expected only the first real rolling segment before sync boundary, got %d", len(plan.Segments))
	}
	if plan.Segments[0].SegmentType != fundingSegmentTypeSingleReal {
		t.Fatalf("expected single_real segment, got %s", plan.Segments[0].SegmentType)
	}
	if len(plan.Projections) != 1 {
		t.Fatalf("expected only one current entry-path projection, got %d", len(plan.Projections))
	}
	if want := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(); plan.Projections[0].ProjectedFundingTimeMs != want {
		t.Fatalf("expected current real rolling projection at 14:00, got %d", plan.Projections[0].ProjectedFundingTimeMs)
	}
	if want := 0.00356; math.Abs(plan.Projections[0].CarryRate-want) > 1e-9 {
		t.Fatalf("expected current real carry %.8f, got %.8f", want, plan.Projections[0].CarryRate)
	}
	if plan.Projections[0].StrategyMode != StrategyModeRollingCycleAligned {
		t.Fatalf("expected rolling projection mode, got %s", plan.Projections[0].StrategyMode)
	}
	if plan.Projections[0].NextReviewTimeMs != longFunding.FundingTimeMs {
		t.Fatalf("expected next review at first settlement %d, got %d", longFunding.FundingTimeMs, plan.Projections[0].NextReviewTimeMs)
	}
	if plan.Projections[0].SyncBoundaryTimeMs != shortFunding.FundingTimeMs {
		t.Fatalf("expected sync boundary %d, got %d", shortFunding.FundingTimeMs, plan.Projections[0].SyncBoundaryTimeMs)
	}
	if plan.Projections[0].PathEndReason != fundingProjectionPathEndBoundary {
		t.Fatalf("expected path to stop at sync boundary, got %s", plan.Projections[0].PathEndReason)
	}
}

func TestBuildRollingDirectionalFundingPlan_NoLongerBuildsForecastFlipSegment(t *testing.T) {
	cfg := Config{
		StrategyMode: StrategyModeRollingCycleAligned,
		HoldHours:    4,
		RollingEntryPathRequireConsistentDirection:     true,
		RollingAllowIntermediateForecastBeforeBoundary: true,
		RollingForbidBoundaryForecast:                  true,
	}.normalize()

	now := time.Date(2026, 1, 1, 13, 48, 0, 0, time.UTC)
	longFunding := entity.FundingSnapshot{
		Exchange:             "aster",
		FundingRate:          -0.00356,
		FundingTimeMs:        time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}
	shortFunding := entity.FundingSnapshot{
		Exchange:             "binance",
		FundingRate:          -0.012,
		FundingTimeMs:        time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}

	longForecast := fundingForecast{
		CurrentRate:        longFunding.FundingRate,
		BaselineRate:       0.001,
		HistoryMean:        0.001,
		Regime:             "sign_flip_risk",
		Confidence:         "guarded",
		MeanReversion:      0.5,
		ContinuationDecay:  1,
		EffectiveFloorRate: -0.01,
		EffectiveCapRate:   0.01,
	}

	plan := buildRollingDirectionalFundingPlan(
		cfg,
		now,
		"aster",
		longFunding,
		longForecast,
		"binance",
		shortFunding,
		spotForecast(shortFunding.FundingRate, 1),
	)

	if len(plan.Segments) != 1 {
		t.Fatalf("expected only the first real segment to remain, got %d", len(plan.Segments))
	}
	if len(plan.Projections) != 1 {
		t.Fatalf("expected a single real-only entry path projection, got %d projections", len(plan.Projections))
	}
	if want := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(); plan.Projections[0].ProjectedFundingTimeMs != want {
		t.Fatalf("expected entry path to stop at first real segment, got %d", plan.Projections[0].ProjectedFundingTimeMs)
	}
	if plan.Projections[0].PathEndReason != fundingProjectionPathEndBoundary {
		t.Fatalf("expected path end reason %s, got %s", fundingProjectionPathEndBoundary, plan.Projections[0].PathEndReason)
	}
}

func TestBestFundingDirection_RollingModeChoosesCurrentEntryDirection(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		StrategyMode: StrategyModeRollingCycleAligned,
		HoldHours:    4,
		RollingEntryPathRequireConsistentDirection:     true,
		RollingAllowIntermediateForecastBeforeBoundary: true,
		RollingForbidBoundaryForecast:                  true,
	}}

	now := time.Date(2026, 1, 1, 13, 48, 0, 0, time.UTC)
	aster := entity.FundingSnapshot{
		Exchange:             "aster",
		FundingRate:          -0.00356,
		FundingTimeMs:        time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}
	binance := entity.FundingSnapshot{
		Exchange:             "binance",
		FundingRate:          -0.012,
		FundingTimeMs:        time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}

	longEx, shortEx, projection, ok := r.bestFundingDirection(now, "aster", aster, spotForecast(aster.FundingRate, 1), "binance", binance, spotForecast(binance.FundingRate, 1))
	if !ok {
		t.Fatalf("expected rolling mode to find a valid direction")
	}
	if longEx != "aster" || shortEx != "binance" {
		t.Fatalf("expected rolling direction long aster / short binance, got long=%s short=%s", longEx, shortEx)
	}
	if want := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(); projection.ProjectedFundingTimeMs != want {
		t.Fatalf("expected current real entry path at 14:00, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount != 1 || projection.ShortFundingEventCount != 0 {
		t.Fatalf("expected path counts long=1 short=0, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
	}
}

func TestStrategyRunnerForecastFunding_RollingModeFallsBackToSpotOnly(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		StrategyMode:                  StrategyModeRollingCycleAligned,
		FundingRateContinuationDecay:  0.3,
		FundingSmoothingCurrentWeight: 0.1,
		FundingHistoryLookback:        12 * time.Hour,
	}}

	now := time.Date(2026, 1, 1, 13, 48, 0, 0, time.UTC)
	item := entity.FundingSnapshot{
		Exchange:             "aster",
		Symbol:               "KATUSDT",
		FundingRate:          -0.00356,
		FundingTimeMs:        time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	forecast := r.forecastFunding(context.Background(), now, item)
	if forecast.Regime != "spot_only" {
		t.Fatalf("expected rolling mode to use spot_only regime, got %s", forecast.Regime)
	}
	if forecast.CurrentRate != item.FundingRate || forecast.BaselineRate != item.FundingRate || forecast.HistoryMean != item.FundingRate {
		t.Fatalf("expected rolling forecast rates to stay on current spot %.8f, got current=%.8f baseline=%.8f mean=%.8f", item.FundingRate, forecast.CurrentRate, forecast.BaselineRate, forecast.HistoryMean)
	}
	if got := forecast.PredictedRateForEvent(2); got != item.FundingRate {
		t.Fatalf("expected later rolling event to stay on current spot %.8f, got %.8f", item.FundingRate, got)
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

func TestBuildVenueProfileRegistry_MapsConfiguredExchangeToVenueFamily(t *testing.T) {
	registry := BuildVenueProfileRegistry(exchange.ConfigSet{
		Additional: map[string]exchange.ExchangeConfig{
			"bybit": {
				Enabled:     true,
				AdapterKind: exchange.AdapterKindBinanceLike,
			},
			"myhl": {
				Enabled:     true,
				AdapterKind: "hyperliquid",
			},
			"customaster": {
				Enabled:   true,
				VenueKind: "aster",
			},
		},
	})

	if got := registry.ExecutionPenaltyMultiplier("bybit"); got != 1.00 {
		t.Fatalf("expected bybit to inherit binance-like multiplier 1.00, got %.2f", got)
	}
	if floor, cap, source := registry.FundingClamp("myhl", 1); source != "hyperliquid_cap" || floor != -0.004 || cap != 0.004 {
		t.Fatalf("expected myhl to inherit hyperliquid profile, got source=%s floor=%.6f cap=%.6f", source, floor, cap)
	}
	if got := registry.ExecutionPenaltyMultiplier("customaster"); got != 1.10 {
		t.Fatalf("expected explicit aster venue kind multiplier 1.10, got %.2f", got)
	}
}
