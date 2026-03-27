package entity

import "testing"

func TestOpportunityJSONHooks_RoundTripProjectionDetails(t *testing.T) {
	opp := &Opportunity{
		ProjectionDetails: []OpportunityProjection{
			{
				ProjectionRank:            1,
				IsBestProjection:          true,
				ProjectedFundingTimeMs:    123,
				LongFundingEventCount:     1,
				ShortFundingEventCount:    4,
				FundingWindowHours:        4,
				CarryRate:                 0.0012,
				CarryRateHourlyEquivalent: 0.0003,
			},
		},
		LongFundingRule: OpportunityFundingRule{
			Exchange:             "binance",
			FundingIntervalHours: 1,
			ClampSource:          "binance_like_adaptive_short_interval_cap",
		},
		FundingSegments: []OpportunityFundingSegment{
			{
				SegmentRank:            1,
				SettlementTimeMs:       123,
				SegmentType:            "single_real",
				HeldLongExchange:       "aster",
				HeldShortExchange:      "binance",
				OptimalLongExchange:    "aster",
				OptimalShortExchange:   "binance",
				DirectionMatchesHeld:   true,
				CarryRate:              0.0012,
				HeldDirectionCarryRate: 0.0012,
			},
		},
	}
	if err := opp.BeforeSave(nil); err != nil {
		t.Fatalf("BeforeSave failed: %v", err)
	}
	if opp.ProjectionDetailsJSON == "" {
		t.Fatalf("expected ProjectionDetailsJSON to be populated")
	}

	loaded := &Opportunity{
		ProjectionDetailsJSON: opp.ProjectionDetailsJSON,
		LongFundingRuleJSON:   opp.LongFundingRuleJSON,
		ShortFundingRuleJSON:  opp.ShortFundingRuleJSON,
		FundingSegmentsJSON:   opp.FundingSegmentsJSON,
		EntryPathSegmentsJSON: opp.EntryPathSegmentsJSON,
	}
	if err := loaded.AfterFind(nil); err != nil {
		t.Fatalf("AfterFind failed: %v", err)
	}
	if len(loaded.ProjectionDetails) != 1 {
		t.Fatalf("expected 1 projection detail, got %d", len(loaded.ProjectionDetails))
	}
	if !loaded.ProjectionDetails[0].IsBestProjection {
		t.Fatalf("expected best projection flag to survive round-trip")
	}
	if loaded.LongFundingRule.ClampSource != "binance_like_adaptive_short_interval_cap" {
		t.Fatalf("unexpected clamp source after round-trip: %s", loaded.LongFundingRule.ClampSource)
	}
	if len(loaded.FundingSegments) != 1 {
		t.Fatalf("expected 1 funding segment, got %d", len(loaded.FundingSegments))
	}
	if loaded.FundingSegments[0].OptimalLongExchange != "aster" {
		t.Fatalf("unexpected optimal long exchange after round-trip: %s", loaded.FundingSegments[0].OptimalLongExchange)
	}
}
