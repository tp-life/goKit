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
