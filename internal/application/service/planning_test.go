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
