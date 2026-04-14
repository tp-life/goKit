package service

import (
	"testing"
	"time"

	"goKit/internal/domain/entity"
)

func TestBuildExecutionPlans_RebalancesLegsToCommonNotional(t *testing.T) {
	now := time.Now().UTC()
	runner := &StrategyRunner{
		cfg:   Config{TotalCapitalUSDT: 1000, CapitalUtilization: 1, Leverage: 1}.normalize(),
		store: NewMarketStore(),
	}

	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:    "longex",
		Symbol:      "BTC",
		VenueSymbol: "BTCUSDT",
		StepSize:    "0.1",
		MinQty:      "0.1",
		MinNotional: "10",
	})
	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:    "shortex",
		Symbol:      "BTC",
		VenueSymbol: "BTCUSDT",
		StepSize:    "3",
		MinQty:      "3",
		MinNotional: "10",
	})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "longex", Symbol: "BTC", AskPrice: 100, BidPrice: 99, EventTimeMs: now.UnixMilli()})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "shortex", Symbol: "BTC", AskPrice: 100.1, BidPrice: 100, EventTimeMs: now.UnixMilli()})

	opportunities := []entity.Opportunity{
		{
			Symbol:                "BTC",
			LongExchange:          "longex",
			ShortExchange:         "shortex",
			Status:                OpportunityStatusEligible,
			EligibleForExecution:  true,
			NetExpectedPNL:        3,
			GrossFundingPNL:       5,
			EntryFeePNL:           0.4,
			ExitFeePNL:            0.4,
			SlippagePNL:           0.2,
			SafetyBufferPNL:       0.1,
			EntryPenaltyBps:       5,
			ExitPenaltyBps:        5,
			HedgePenaltyBps:       1,
			GrossEdgeHourly:       0.0002,
			EarliestFundingTimeMs: now.Add(2 * time.Minute).UnixMilli(),
			LatestFundingTimeMs:   now.Add(2 * time.Minute).UnixMilli(),
		},
	}

	plans := runner.buildExecutionPlans(now, "batch-1", opportunities)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	plan := plans[0]
	if plan.LongQty != 9 {
		t.Fatalf("expected long qty to rebalance down to 9, got %.8f", plan.LongQty)
	}
	if plan.ShortQty != 9 {
		t.Fatalf("expected short qty to remain 9, got %.8f", plan.ShortQty)
	}
	if plan.RoundedNotionalUSDT != 900 {
		t.Fatalf("expected rounded notional 900, got %.2f", plan.RoundedNotionalUSDT)
	}
	if plan.NetExpectedPNL >= opportunities[0].NetExpectedPNL {
		t.Fatalf("expected scaled plan pnl to be lower than opportunity pnl after rebalancing, got plan=%.4f opp=%.4f", plan.NetExpectedPNL, opportunities[0].NetExpectedPNL)
	}
}

func TestBuildExecutionPlans_RollingModeTargetsFirstReviewTime(t *testing.T) {
	now := time.Now().UTC()
	runner := &StrategyRunner{
		cfg: Config{
			TotalCapitalUSDT:   1000,
			CapitalUtilization: 1,
			Leverage:           1,
			StrategyMode:       StrategyModeRollingCycleAligned,
			Rolling: StrategyRollingConfig{
				Review: StrategyRollingReviewConfig{
					SettleGracePeriod: 12 * time.Second,
				},
			},
		}.normalize(),
		store: NewMarketStore(),
	}

	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:    "aster",
		Symbol:      "BTC",
		VenueSymbol: "BTCUSDT",
		StepSize:    "0.1",
		MinQty:      "0.1",
		MinNotional: "10",
	})
	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:    "binance",
		Symbol:      "BTC",
		VenueSymbol: "BTCUSDT",
		StepSize:    "0.1",
		MinQty:      "0.1",
		MinNotional: "10",
	})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "aster", Symbol: "BTC", AskPrice: 100, BidPrice: 99.9, EventTimeMs: now.UnixMilli()})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance", Symbol: "BTC", AskPrice: 100.2, BidPrice: 100.1, EventTimeMs: now.UnixMilli()})

	nextReviewTimeMs := now.Add(2 * time.Minute).UnixMilli()
	syncBoundaryTimeMs := now.Add(4 * time.Minute).UnixMilli()
	opportunities := []entity.Opportunity{
		{
			Symbol:                       "BTC",
			LongExchange:                 "aster",
			ShortExchange:                "binance",
			Status:                       OpportunityStatusEligible,
			EligibleForExecution:         true,
			NetExpectedPNL:               5,
			GrossFundingPNL:              7,
			EntryFeePNL:                  0.4,
			ExitFeePNL:                   0.4,
			SlippagePNL:                  0.2,
			SafetyBufferPNL:              0.1,
			EntryPenaltyBps:              5,
			ExitPenaltyBps:               5,
			HedgePenaltyBps:              1,
			GrossEdgeHourly:              0.0002,
			StrategyMode:                 StrategyModeRollingCycleAligned,
			EarliestFundingTimeMs:        nextReviewTimeMs,
			LatestFundingTimeMs:          syncBoundaryTimeMs,
			ProjectedFundingTimeMs:       now.Add(3 * time.Minute).UnixMilli(),
			RequiredEntryByFundingTimeMs: nextReviewTimeMs,
			NextReviewTimeMs:             nextReviewTimeMs,
			SyncBoundaryTimeMs:           syncBoundaryTimeMs,
			EntryPathSegmentCount:        2,
			EntryPathStopReason:          "sync_boundary",
		},
	}

	plans := runner.buildExecutionPlans(now, "batch-rolling", opportunities)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	plan := plans[0]
	if plan.StrategyMode != StrategyModeRollingCycleAligned {
		t.Fatalf("expected rolling strategy mode, got %s", plan.StrategyMode)
	}
	if want := buildRollingGroupKey("BTC", "aster", "binance"); plan.RollingGroupKey != want {
		t.Fatalf("expected rolling group key %s, got %s", want, plan.RollingGroupKey)
	}
	if plan.NextReviewTimeMs != nextReviewTimeMs || plan.SyncBoundaryTimeMs != syncBoundaryTimeMs {
		t.Fatalf("expected rolling review anchors to be copied, got next_review=%d sync_boundary=%d", plan.NextReviewTimeMs, plan.SyncBoundaryTimeMs)
	}
	if want := nextReviewTimeMs + 12_000; plan.TargetCloseTimeMs != want {
		t.Fatalf("expected rolling target close to follow first review time, got %d want %d", plan.TargetCloseTimeMs, want)
	}
}

func TestBuildExecutionPlans_SameExchangeCopiesLongHoldMetrics(t *testing.T) {
	now := time.Now().UTC()
	runner := &StrategyRunner{
		cfg: Config{
			ArbitrageMode:      ArbitrageModeSameExchangeSpotPerp,
			TotalCapitalUSDT:   1000,
			CapitalUtilization: 1,
			Leverage:           1,
		}.normalize(),
		store: NewMarketStore(),
	}

	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:     "binance_spot",
		Symbol:       "BTC",
		VenueSymbol:  "BTCUSDT",
		StepSize:     "0.001",
		MinQty:       "0.001",
		MinNotional:  "10",
		ContractType: "SPOT",
	})
	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:             "binance",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		StepSize:             "0.001",
		MinQty:               "0.001",
		MinNotional:          "10",
		ContractType:         "PERPETUAL",
		FundingIntervalHours: 8,
	})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance_spot", Symbol: "BTC", AskPrice: 100, BidPrice: 99.9, EventTimeMs: now.UnixMilli()})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance", Symbol: "BTC", AskPrice: 100.2, BidPrice: 100.1, EventTimeMs: now.UnixMilli()})

	opportunities := []entity.Opportunity{
		{
			Symbol:                       "BTC",
			LongExchange:                 "binance_spot",
			ShortExchange:                "binance",
			Status:                       OpportunityStatusEligible,
			EligibleForExecution:         true,
			NetExpectedPNL:               3,
			GrossFundingPNL:              5,
			EntryFeePNL:                  0.4,
			ExitFeePNL:                   0.4,
			SlippagePNL:                  0.2,
			SafetyBufferPNL:              0.1,
			EarliestFundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
			LatestFundingTimeMs:          now.Add(2 * time.Minute).UnixMilli(),
			RequiredEntryByFundingTimeMs: now.Add(2 * time.Minute).UnixMilli(),
			ShortFundingRule: entity.OpportunityFundingRule{
				HistorySampleCount:           20,
				HistoryMeanRate:              0.0002,
				HistoricalSupportRatio:       0.7,
				EstimatedEventRate:           0.001,
				EstimatedAnnualizedCarryRate: 0.45,
				EstimatedAnnualizedNetRate:   0.39,
				SuggestedFundingEvents:       5,
				SuggestedHoldHours:           34,
				SuggestedFundingTimeMs:       now.Add(34 * time.Hour).UnixMilli(),
				SuggestedGrossFundingPNL:     5.0,
				SuggestedNetPNL:              3.9,
				LongHoldEligible:             true,
				LongHoldReason:               "eligible",
			},
			SameExchangeLongHoldUsingHistoryEstimate: true,
		},
	}

	plans := runner.buildExecutionPlans(now, "batch-same", opportunities)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	plan := plans[0]
	if plan.PerpFundingHistoryMeanRate != 0.0002 {
		t.Fatalf("expected history mean rate to be copied, got %.6f", plan.PerpFundingHistoryMeanRate)
	}
	if plan.PerpFundingHistoricalSupportRatio != 0.7 {
		t.Fatalf("expected support ratio to be copied, got %.4f", plan.PerpFundingHistoricalSupportRatio)
	}
	if plan.PerpFundingEstimatedEventRate != 0.001 {
		t.Fatalf("expected estimated event rate to be copied, got %.6f", plan.PerpFundingEstimatedEventRate)
	}
	if plan.PerpFundingEstimatedAnnualizedNetRate < 0.44 || plan.PerpFundingEstimatedAnnualizedNetRate > 0.46 {
		t.Fatalf("expected annualized net rate to be recalculated near 0.45, got %.4f", plan.PerpFundingEstimatedAnnualizedNetRate)
	}
	if !plan.SameExchangeLongHoldEligible {
		t.Fatal("expected long-hold eligibility to be copied")
	}
	if !plan.SameExchangeLongHoldUsingHistoryEstimate {
		t.Fatal("expected plan to keep historical long-hold estimate flag")
	}
	if plan.SameExchangeLongHoldSuggestedFundingEvents != 5 {
		t.Fatalf("expected suggested funding events to be copied, got %d", plan.SameExchangeLongHoldSuggestedFundingEvents)
	}
	if plan.SameExchangeLongHoldSuggestedHoldHours != 34 {
		t.Fatalf("expected suggested hold hours to be copied, got %.2f", plan.SameExchangeLongHoldSuggestedHoldHours)
	}
	if plan.SameExchangeLongHoldSuggestedNetPNL <= 0 {
		t.Fatalf("expected suggested net pnl to stay positive after scaling, got %.4f", plan.SameExchangeLongHoldSuggestedNetPNL)
	}
}

func TestBuildExecutionPlans_SameExchangeExtremeFundingDownsizesPlan(t *testing.T) {
	now := time.Now().UTC()
	runner := &StrategyRunner{
		cfg: Config{
			ArbitrageMode:      ArbitrageModeSameExchangeSpotPerp,
			TotalCapitalUSDT:   1000,
			CapitalUtilization: 1,
			Leverage:           2,
			SameExchange: StrategySameExchangeConfig{
				Risk: StrategySameExchangeRiskConfig{
					MaxPerpLeverage:              1.5,
					FundingExtremePercentile:     0.95,
					ExtremeFundingNegativeRatio:  0.50,
					ExtremeFundingSizeMultiplier: 0.50,
				},
			},
		}.normalize(),
		store: NewMarketStore(),
	}

	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:     "binance_spot",
		Symbol:       "BTC",
		VenueSymbol:  "BTCUSDT",
		StepSize:     "0.001",
		MinQty:       "0.001",
		MinNotional:  "10",
		ContractType: "SPOT",
	})
	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:     "binance",
		Symbol:       "BTC",
		VenueSymbol:  "BTCUSDT",
		StepSize:     "0.001",
		MinQty:       "0.001",
		MinNotional:  "10",
		ContractType: "PERPETUAL",
	})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance_spot", Symbol: "BTC", AskPrice: 100, BidPrice: 99.9, EventTimeMs: now.UnixMilli()})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance", Symbol: "BTC", AskPrice: 100.2, BidPrice: 100.1, EventTimeMs: now.UnixMilli()})

	opportunities := []entity.Opportunity{
		{
			Symbol:                       "BTC",
			LongExchange:                 "binance_spot",
			ShortExchange:                "binance",
			Status:                       OpportunityStatusEligible,
			EligibleForExecution:         true,
			NetExpectedPNL:               3,
			GrossFundingPNL:              6,
			EntryFeePNL:                  0.4,
			ExitFeePNL:                   0.4,
			SlippagePNL:                  0.2,
			SafetyBufferPNL:              0.1,
			EarliestFundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
			LatestFundingTimeMs:          now.Add(2 * time.Minute).UnixMilli(),
			RequiredEntryByFundingTimeMs: now.Add(2 * time.Minute).UnixMilli(),
			ShortFundingRule: entity.OpportunityFundingRule{
				HistorySampleCount:          18,
				HistoryNegativeRatio:        0.62,
				CurrentHistoricalPercentile: 0.98,
			},
		},
	}

	plans := runner.buildExecutionPlans(now, "batch-risk", opportunities)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	plan := plans[0]
	if plan.TargetLeverage != 1.5 {
		t.Fatalf("expected same-exchange leverage cap 1.5, got %.2f", plan.TargetLeverage)
	}
	if plan.TargetNotionalUSDT != 500 {
		t.Fatalf("expected target notional to be down-sized to 500, got %.2f", plan.TargetNotionalUSDT)
	}
	if plan.CapitalAllocatedUSDT != 500 {
		t.Fatalf("expected capital allocation to be down-sized to 500, got %.2f", plan.CapitalAllocatedUSDT)
	}
}

func TestBuildExecutionPlans_SameExchangeExtremeBasisDownsizesPlan(t *testing.T) {
	now := time.Now().UTC()
	runner := &StrategyRunner{
		cfg: Config{
			ArbitrageMode:      ArbitrageModeSameExchangeSpotPerp,
			TotalCapitalUSDT:   1000,
			CapitalUtilization: 1,
			SameExchange: StrategySameExchangeConfig{
				Entry: StrategySameExchangeEntryConfig{
					BasisLongHoldWindowHours: 12,
					MaxBasisPaybackEvents:    4,
				},
				Risk: StrategySameExchangeRiskConfig{
					ExtremeBasisPaybackEvents:  1.8,
					ExtremeBasisSizeMultiplier: 0.65,
				},
			},
		}.normalize(),
		store: NewMarketStore(),
	}

	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:     "binance_spot",
		Symbol:       "BTC",
		VenueSymbol:  "BTCUSDT",
		StepSize:     "0.001",
		MinQty:       "0.001",
		MinNotional:  "10",
		ContractType: "SPOT",
	})
	runner.store.UpsertSymbol(entity.Symbol{
		Exchange:     "binance",
		Symbol:       "BTC",
		VenueSymbol:  "BTCUSDT",
		StepSize:     "0.001",
		MinQty:       "0.001",
		MinNotional:  "10",
		ContractType: "PERPETUAL",
	})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance_spot", Symbol: "BTC", AskPrice: 100, BidPrice: 99.9, EventTimeMs: now.UnixMilli()})
	runner.store.UpsertBookTop(entity.BookTopSnapshot{Exchange: "binance", Symbol: "BTC", AskPrice: 100.45, BidPrice: 100.4, EventTimeMs: now.UnixMilli()})

	opportunities := []entity.Opportunity{
		{
			Symbol:                       "BTC",
			LongExchange:                 "binance_spot",
			ShortExchange:                "binance",
			Status:                       OpportunityStatusEligible,
			EligibleForExecution:         true,
			NetExpectedPNL:               5,
			GrossFundingPNL:              9,
			EntryFeePNL:                  0.5,
			ExitFeePNL:                   0.5,
			SlippagePNL:                  0.5,
			SafetyBufferPNL:              0.5,
			FundingWindowHours:           24,
			LongFundingEventCount:        0,
			ShortFundingEventCount:       3,
			EarliestFundingTimeMs:        now.Add(2 * time.Minute).UnixMilli(),
			LatestFundingTimeMs:          now.Add(24 * time.Hour).UnixMilli(),
			RequiredEntryByFundingTimeMs: now.Add(2 * time.Minute).UnixMilli(),
			ProjectedFundingTimeMs:       now.Add(24 * time.Hour).UnixMilli(),
		},
	}

	plans := runner.buildExecutionPlans(now, "batch-basis-risk", opportunities)
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	plan := plans[0]
	if plan.TargetNotionalUSDT != 650 {
		t.Fatalf("expected target notional to be down-sized to 650, got %.2f", plan.TargetNotionalUSDT)
	}
	if plan.CapitalAllocatedUSDT != 650 {
		t.Fatalf("expected capital allocation to be down-sized to 650, got %.2f", plan.CapitalAllocatedUSDT)
	}
	if !plan.SameExchangeBasisUsesPaybackModel {
		t.Fatalf("expected plan to record payback-model basis evaluation, got %+v", plan)
	}
	if plan.SameExchangeBasisReason != sameExchangeBasisReasonExtremePayback {
		t.Fatalf("expected basis reason %s, got %s", sameExchangeBasisReasonExtremePayback, plan.SameExchangeBasisReason)
	}
	if plan.SameExchangeBasisPaybackFundingEvents <= 1.8 {
		t.Fatalf("expected payback events above extreme threshold, got %.4f", plan.SameExchangeBasisPaybackFundingEvents)
	}
	if plan.SameExchangeBasisRiskSizeMultiplier != 0.65 {
		t.Fatalf("expected basis risk size multiplier 0.65, got %.4f", plan.SameExchangeBasisRiskSizeMultiplier)
	}
}

func TestIsOpportunityEligible_SameExchangeRequiresLongHoldEligibility(t *testing.T) {
	now := time.Now().UTC()
	runner := &StrategyRunner{
		cfg: Config{
			ArbitrageMode: ArbitrageModeSameExchangeSpotPerp,
			SameExchange: StrategySameExchangeConfig{
				Entry: StrategySameExchangeEntryConfig{
					RequireLongHoldEligible: boolPtr(true),
				},
			},
		}.normalize(),
	}

	opp := entity.Opportunity{
		Status:                OpportunityStatusEligible,
		EligibleForExecution:  true,
		NetExpectedPNL:        3,
		EarliestFundingTimeMs: now.Add(2 * time.Minute).UnixMilli(),
		ShortFundingRule: entity.OpportunityFundingRule{
			LongHoldEligible: false,
			LongHoldReason:   "support_ratio_low",
		},
	}

	if runner.isOpportunityEligible(now, opp) {
		t.Fatal("expected same-exchange opportunity to be rejected when long-hold guard fails")
	}
}
