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

	projection, ok := r.projectFundingCarry(now, long, long.FundingRate, short, short.FundingRate)
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

	projection, ok := r.projectFundingCarry(now, long, long.FundingRate, short, short.FundingRate)
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
	got := projectedLegFundingCarry(0.0010, 0.0004, 1, 0.6)
	if got != 0.0010 {
		t.Fatalf("expected first event to use current rate only, got %.8f", got)
	}
}

func TestProjectedLegFundingCarry_UsesSmoothedRateForLaterEvents(t *testing.T) {
	got := projectedLegFundingCarry(0.0010, 0.0004, 3, 0.6)
	want := 0.0010 + 0.0004*(1+0.6)
	if got != want {
		t.Fatalf("expected later events to use smoothed future rate %.8f, got %.8f", want, got)
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
