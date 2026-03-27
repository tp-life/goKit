package service

import (
	"testing"
	"time"
)

func TestConfigNormalize_DefaultsTradingModesToTaker(t *testing.T) {
	cfg := Config{}

	normalized := cfg.normalize()

	if normalized.EntryMode != "taker" {
		t.Fatalf("expected default entry mode taker, got %s", normalized.EntryMode)
	}
	if normalized.ExitMode != "taker" {
		t.Fatalf("expected default exit mode taker, got %s", normalized.ExitMode)
	}
	if normalized.HoldSelectionMode != HoldSelectionModeBestNet {
		t.Fatalf("expected default hold selection mode %s, got %s", HoldSelectionModeBestNet, normalized.HoldSelectionMode)
	}
	if normalized.Execution.CloseGracePeriod != 15*time.Second {
		t.Fatalf("expected default close grace period 15s, got %s", normalized.Execution.CloseGracePeriod)
	}
}

func TestConfigNormalize_UnknownHoldSelectionModeFallsBack(t *testing.T) {
	cfg := Config{HoldSelectionMode: "unknown_mode"}

	normalized := cfg.normalize()

	if normalized.HoldSelectionMode != HoldSelectionModeBestNet {
		t.Fatalf("expected fallback hold selection mode %s, got %s", HoldSelectionModeBestNet, normalized.HoldSelectionMode)
	}
}

func TestConfigNormalize_AcceptsStrictTargetHoldSelectionMode(t *testing.T) {
	cfg := Config{HoldSelectionMode: HoldSelectionModeStrictTarget}

	normalized := cfg.normalize()

	if normalized.HoldSelectionMode != HoldSelectionModeStrictTarget {
		t.Fatalf("expected hold selection mode %s, got %s", HoldSelectionModeStrictTarget, normalized.HoldSelectionMode)
	}
}

func TestConfigNormalize_MapsNestedRollingDesignConfigIntoRuntimeFields(t *testing.T) {
	trueValue := true

	cfg := Config{
		StrategyMode: StrategyModeRollingCycleAligned,
		Universe: StrategyUniverseConfig{
			AllowedSymbols: []string{"BTC", "ETH"},
			CoreSymbols:    []string{"BTC"},
			QuoteAsset:     "USDT",
		},
		Capital: StrategyCapitalConfig{
			TotalCapitalUSDT:   1200,
			CapitalUtilization: 0.75,
			Leverage:           3,
			AssumedNotional:    2700,
		},
		Opportunity: StrategyOpportunityConfig{
			HoldHours:               12,
			LegacyHoldSelectionMode: HoldSelectionModeLatestProfitable,
			MinNetPNL:               2.5,
		},
		ExecutionCost: StrategyExecutionCostConfig{
			SlippageBps:      3,
			SafetyBufferUSDT: 0.4,
			EntryMode:        "maker",
			ExitMode:         "mixed",
		},
		MarketData: StrategyMarketDataConfig{
			MaxDataAge:                     20 * time.Second,
			FundingSnapshotPersistInterval: 30 * time.Second,
			BookSnapshotPersistInterval:    10 * time.Second,
			SnapshotRetention:              48 * time.Hour,
			OpportunityCalcInterval:        4 * time.Second,
		},
		SpreadGuard: StrategySpreadGuardConfig{
			MaxSpreadBps:                   10,
			DynamicMaxSpreadMultiplier:     1.8,
			DynamicMaxSpreadReferenceHours: 12,
			EntryLeadTime:                  2 * time.Minute,
			EntryCutoffTime:                30 * time.Second,
		},
		Prediction: StrategyPredictionConfig{
			FundingHistoryLookback:                  8 * time.Hour,
			FundingSmoothingCurrentWeight:           0.6,
			FundingRateContinuationDecay:            0.5,
			AllowIntermediateForecastBeforeBoundary: &trueValue,
			ForbidBoundaryForecast:                  &trueValue,
		},
		Scan: StrategyScanConfig{
			DynamicCandidateLimit: 40,
			RotationBatchSize:     15,
			RotationInterval:      45 * time.Second,
			DeepScanHoldDuration:  5 * time.Minute,
		},
		Rolling: StrategyRollingConfig{
			EntryPathRequireConsistentDirection: &trueValue,
			Review: StrategyRollingReviewConfig{
				SettleGracePeriod:             12 * time.Second,
				FreshSnapshotMaxWait:          18 * time.Second,
				CloseOnSnapshotTimeout:        &trueValue,
				ContinueOnSameDirection:       &trueValue,
				CloseOnUnprofitable:           &trueValue,
				RequireIncrementalNetPositive: &trueValue,
				MinIncrementalNetPNL:          0.3,
			},
			Flip: StrategyRollingFlipConfig{
				Enabled:               &trueValue,
				RequireNetPositive:    &trueValue,
				MinNetPNL:             3,
				SlippageMultiplier:    1.8,
				ExtraSafetyBufferUSDT: 0.6,
			},
		},
	}

	normalized := cfg.normalize()

	if normalized.StrategyMode != StrategyModeRollingCycleAligned {
		t.Fatalf("expected strategy mode %s, got %s", StrategyModeRollingCycleAligned, normalized.StrategyMode)
	}
	if normalized.HoldHours != 12 {
		t.Fatalf("expected hold_hours 12, got %.2f", normalized.HoldHours)
	}
	if normalized.HoldSelectionMode != HoldSelectionModeLatestProfitable {
		t.Fatalf("expected hold selection mode %s, got %s", HoldSelectionModeLatestProfitable, normalized.HoldSelectionMode)
	}
	if normalized.TotalCapitalUSDT != 1200 || normalized.CapitalUtilization != 0.75 || normalized.Leverage != 3 {
		t.Fatalf("unexpected capital normalization: %+v", normalized)
	}
	if normalized.SlippageBps != 3 || normalized.SafetyBufferUSDT != 0.4 {
		t.Fatalf("expected execution cost mapping, got slippage=%.2f safety_buffer=%.2f", normalized.SlippageBps, normalized.SafetyBufferUSDT)
	}
	if normalized.EntryMode != "maker" || normalized.ExitMode != "mixed" {
		t.Fatalf("expected mapped order modes maker/mixed, got %s/%s", normalized.EntryMode, normalized.ExitMode)
	}
	if normalized.RollingReviewSettleGracePeriod != 12*time.Second {
		t.Fatalf("expected rolling settle grace 12s, got %s", normalized.RollingReviewSettleGracePeriod)
	}
	if normalized.RollingReviewFreshSnapshotMaxWait != 18*time.Second {
		t.Fatalf("expected rolling fresh snapshot max wait 18s, got %s", normalized.RollingReviewFreshSnapshotMaxWait)
	}
	if !normalized.RollingFlipEnabled || normalized.RollingFlipMinNetPNL != 3 {
		t.Fatalf("expected rolling flip config to map, got enabled=%v min_net=%.2f", normalized.RollingFlipEnabled, normalized.RollingFlipMinNetPNL)
	}
}
