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

func TestConfigNormalize_AcceptsDynamicProfitHoldSelectionMode(t *testing.T) {
	cfg := Config{HoldSelectionMode: HoldSelectionModeDynamicProfit}

	normalized := cfg.normalize()

	if normalized.HoldSelectionMode != HoldSelectionModeDynamicProfit {
		t.Fatalf("expected hold selection mode %s, got %s", HoldSelectionModeDynamicProfit, normalized.HoldSelectionMode)
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
		SameExchange: StrategySameExchangeConfig{
			Entry: StrategySameExchangeEntryConfig{
				BasisLongHoldWindowHours: 16,
				MaxBasisPaybackEvents:    5,
			},
			Exit: StrategySameExchangeExitConfig{
				CloseOnNegativeFunding:        &trueValue,
				HistoryNegativeRatioThreshold: 0.65,
				RequirePositiveClosePNL:       &trueValue,
				MinClosePNL:                   2.2,
			},
			Risk: StrategySameExchangeRiskConfig{
				MaxPerpLeverage:              1.5,
				MinLiqDistanceRatio:          0.12,
				WarnLiqDistanceRatio:         0.20,
				ReduceLiqDistanceRatio:       0.10,
				EmergencyLiqDistanceRatio:    0.08,
				Max1hPriceShockRatio:         0.09,
				FundingExtremePercentile:     0.96,
				ExtremeFundingNegativeRatio:  0.55,
				ExtremeFundingSizeMultiplier: 0.45,
				ExtremeBasisPaybackEvents:    3.2,
				ExtremeBasisSizeMultiplier:   0.7,
			},
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
		Execution: ExecutionConfig{
			PositionMonitor: ExecutionPositionMonitorConfig{
				MaxQtyDeviationRatio: 0.2,
			},
			Replacement: ExecutionReplacementConfig{
				Enabled:               &trueValue,
				OnlyWhenConstrained:   &trueValue,
				RequireNetImprovement: &trueValue,
				MinNetImprovementPNL:  0.8,
				ExtraSafetyBufferUSDT: 0.3,
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
	if !normalized.ExecutionReplacementEnabled || normalized.ExecutionReplacementMinNetImprovementPNL != 0.8 {
		t.Fatalf("expected execution replacement config to map, got enabled=%v min_improvement=%.2f", normalized.ExecutionReplacementEnabled, normalized.ExecutionReplacementMinNetImprovementPNL)
	}
	if normalized.ExecutionPositionMonitorMaxQtyDeviationRatio != 0.2 {
		t.Fatalf("expected position monitor qty deviation ratio 0.2, got %.2f", normalized.ExecutionPositionMonitorMaxQtyDeviationRatio)
	}
	if !normalized.SameExchangeCloseOnNegativeFunding || normalized.SameExchangeHistoryNegativeExitThreshold != 0.65 || normalized.SameExchangeExitMinClosePNL != 2.2 {
		t.Fatalf("expected same-exchange exit config to map, got close_on_negative=%v threshold=%.2f min_close=%.2f", normalized.SameExchangeCloseOnNegativeFunding, normalized.SameExchangeHistoryNegativeExitThreshold, normalized.SameExchangeExitMinClosePNL)
	}
	if normalized.SameExchangeMaxPerpLeverage != 1.5 || normalized.SameExchangeMinLiqDistanceRatio != 0.12 {
		t.Fatalf("expected same-exchange risk config to map leverage/liquidation guard, got leverage=%.2f min_liq=%.2f", normalized.SameExchangeMaxPerpLeverage, normalized.SameExchangeMinLiqDistanceRatio)
	}
	if normalized.SameExchangeMax1hPriceShockRatio != 0.09 {
		t.Fatalf("expected same-exchange price shock guard to map, got %.2f", normalized.SameExchangeMax1hPriceShockRatio)
	}
	if normalized.SameExchangeFundingExtremePercentile != 0.96 || normalized.SameExchangeExtremeFundingSizeMultiplier != 0.45 {
		t.Fatalf("expected same-exchange risk sizing config to map, got percentile=%.2f multiplier=%.2f", normalized.SameExchangeFundingExtremePercentile, normalized.SameExchangeExtremeFundingSizeMultiplier)
	}
	if normalized.SameExchangeBasisLongHoldWindowHours != 16 || normalized.SameExchangeMaxBasisPaybackEvents != 5 {
		t.Fatalf("expected same-exchange basis entry config to map, got hold_window=%.2f payback=%.2f", normalized.SameExchangeBasisLongHoldWindowHours, normalized.SameExchangeMaxBasisPaybackEvents)
	}
	if normalized.SameExchangeExtremeBasisPaybackEvents != 3.2 || normalized.SameExchangeExtremeBasisSizeMultiplier != 0.7 {
		t.Fatalf("expected same-exchange basis risk sizing config to map, got payback=%.2f multiplier=%.2f", normalized.SameExchangeExtremeBasisPaybackEvents, normalized.SameExchangeExtremeBasisSizeMultiplier)
	}
}
