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

	projection, ok := r.projectFundingCarry(now, long, short)
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

	projection, ok := r.projectFundingCarry(now, long, short)
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
