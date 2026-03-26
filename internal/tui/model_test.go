package tui

import (
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

func TestSplitStackedHeights(t *testing.T) {
	for total := 1; total <= 40; total++ {
		top, bottom := splitStackedHeights(total)
		if top < 1 {
			t.Fatalf("expected top height to stay positive, got %d for total=%d", top, total)
		}
		if bottom < 0 {
			t.Fatalf("expected bottom height to stay non-negative, got %d for total=%d", bottom, total)
		}
		if top+bottom != total {
			t.Fatalf("expected split heights to sum to total, got top=%d bottom=%d total=%d", top, bottom, total)
		}
	}
}

func TestPanelListContentHeightUsesPanelFrame(t *testing.T) {
	height := 18
	want := height - ui.panel.GetVerticalFrameSize() - listHeaderLines
	if want < 1 {
		want = 1
	}
	if got := panelListContentHeight(height); got != want {
		t.Fatalf("expected panel list content height %d, got %d", want, got)
	}
}

func TestNormalizeSelections_AutoFollowKeepsTopRankedOpportunity(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.Opportunities = []OpportunityListItem{
		{Symbol: "AAA", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "AAAUSDT", ShortVenueSymbol: "AAAUSDT", NetExpectedPNL: 10},
		{Symbol: "BBB", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "BBBUSDT", ShortVenueSymbol: "BBBUSDT", NetExpectedPNL: 5},
	}
	m.normalizeSelections()

	if got, ok := m.selectedOpportunity(); !ok || got.Symbol != "AAA" {
		t.Fatalf("expected initial top opportunity AAA, got %+v ok=%v", got, ok)
	}

	m.data.AllPlans = []entity.ExecutionPlan{
		{
			PlanKey:          "plan-bbb",
			Symbol:           "BBB",
			LongExchange:     "aster",
			ShortExchange:    "binance",
			LongVenueSymbol:  "BBBUSDT",
			ShortVenueSymbol: "BBBUSDT",
			ReadyNow:         true,
		},
	}
	m.normalizeSelections()

	got, ok := m.selectedOpportunity()
	if !ok {
		t.Fatal("expected selected opportunity after ranking update")
	}
	if got.Symbol != "BBB" {
		t.Fatalf("expected auto-follow to move selection to BBB, got %s", got.Symbol)
	}
	if m.opportunityOffset != 0 {
		t.Fatalf("expected auto-follow to keep top offset, got %d", m.opportunityOffset)
	}
}

func TestNormalizeSelections_PreservesManualOpportunitySelection(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.Opportunities = []repository.OpportunitySummary{
		{Symbol: "AAA", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "AAAUSDT", ShortVenueSymbol: "AAAUSDT", NetExpectedPNL: 10},
		{Symbol: "BBB", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "BBBUSDT", ShortVenueSymbol: "BBBUSDT", NetExpectedPNL: 5},
	}
	m.normalizeSelections()
	m.opportunityAutoFollow = false
	m.selectedOpportunityKey = opportunityKey(m.data.Opportunities[0])

	m.data.AllPlans = []entity.ExecutionPlan{
		{
			PlanKey:          "plan-bbb",
			Symbol:           "BBB",
			LongExchange:     "aster",
			ShortExchange:    "binance",
			LongVenueSymbol:  "BBBUSDT",
			ShortVenueSymbol: "BBBUSDT",
			ReadyNow:         true,
		},
	}
	m.normalizeSelections()

	got, ok := m.selectedOpportunity()
	if !ok {
		t.Fatal("expected selected opportunity after manual selection")
	}
	if got.Symbol != "AAA" {
		t.Fatalf("expected manual selection to stay on AAA, got %s", got.Symbol)
	}
}

func TestRefreshLoadedMsg_AppliesCoreDataAtomically(t *testing.T) {
	m := NewModel(nil, 0)
	m.refreshSeq = 1

	next, _ := m.Update(refreshLoadedMsg{
		Seq: 1,
		System: SystemStatus{
			Watchlist: []string{"BTC"},
		},
		Opportunities: []OpportunityListItem{
			{ID: 11, BatchID: "batch-1", Symbol: "AAA", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "AAAUSDT", ShortVenueSymbol: "AAAUSDT", NetExpectedPNL: 10},
		},
		Executions: []entity.ExecutionRecord{
			{PlanKey: "exec-1", Symbol: "AAA"},
		},
		Stats: repository.SnapshotStats{
			FundingCount24h: 7,
		},
		AllPlans: []entity.ExecutionPlan{
			{PlanKey: "plan-1", Symbol: "AAA", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "AAAUSDT", ShortVenueSymbol: "AAAUSDT"},
		},
		BatchPlans: []entity.ExecutionPlan{
			{PlanKey: "plan-1", OpportunityBatchID: "batch-1"},
		},
		CurrentBatch: "batch-1",
	})

	got := next.(Model)
	if got.data.CurrentBatchID != "batch-1" {
		t.Fatalf("expected current batch batch-1, got %q", got.data.CurrentBatchID)
	}
	if len(got.data.Opportunities) != 1 || len(got.data.AllPlans) != 1 || len(got.data.BatchPlans) != 1 {
		t.Fatalf("expected core refresh data to apply together, got opps=%d allPlans=%d batchPlans=%d", len(got.data.Opportunities), len(got.data.AllPlans), len(got.data.BatchPlans))
	}
	if got.selectedOpportunityKey == "" {
		t.Fatal("expected normalized selection after atomic refresh")
	}
}

func TestRefreshLoadedMsg_IgnoresStaleSeq(t *testing.T) {
	m := NewModel(nil, 0)
	m.refreshSeq = 2
	m.data.CurrentBatchID = "current"
	m.data.Opportunities = []OpportunityListItem{
		{ID: 1, BatchID: "current", Symbol: "OLD", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "OLDUSDT", ShortVenueSymbol: "OLDUSDT", NetExpectedPNL: 1},
	}

	next, _ := m.Update(refreshLoadedMsg{
		Seq:          1,
		CurrentBatch: "stale",
		Opportunities: []OpportunityListItem{
			{ID: 2, BatchID: "stale", Symbol: "NEW", LongExchange: "aster", ShortExchange: "binance", LongVenueSymbol: "NEWUSDT", ShortVenueSymbol: "NEWUSDT", NetExpectedPNL: 9},
		},
	})

	got := next.(Model)
	if got.data.CurrentBatchID != "current" {
		t.Fatalf("expected stale refresh to be ignored, got batch %q", got.data.CurrentBatchID)
	}
	if len(got.data.Opportunities) != 1 || got.data.Opportunities[0].Symbol != "OLD" {
		t.Fatalf("expected stale opportunities to be ignored, got %#v", got.data.Opportunities)
	}
}

func TestEnsureMarketCmd_RefreshesCurrentSymbolWhenSnapshotIsStale(t *testing.T) {
	m := NewModel(nil, 8*time.Second)
	m.selectedOpportunityKey = opportunityKey(OpportunityListItem{
		Symbol:           "BTC",
		LongExchange:     "binance",
		ShortExchange:    "aster",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
	})
	m.data.Opportunities = []OpportunityListItem{
		{Symbol: "BTC", LongExchange: "binance", ShortExchange: "aster", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT"},
	}
	m.data.Market.Symbol = "BTC"
	m.marketLastRefresh = time.Now().Add(-9 * time.Second)

	if cmd := m.ensureMarketCmd(); cmd == nil {
		t.Fatal("expected stale market snapshot to trigger refresh")
	}
}

func TestEnsureMarketCmd_SkipsCurrentSymbolWhenSnapshotIsFresh(t *testing.T) {
	m := NewModel(nil, 8*time.Second)
	m.selectedOpportunityKey = opportunityKey(OpportunityListItem{
		Symbol:           "BTC",
		LongExchange:     "binance",
		ShortExchange:    "aster",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
	})
	m.data.Opportunities = []OpportunityListItem{
		{Symbol: "BTC", LongExchange: "binance", ShortExchange: "aster", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT"},
	}
	m.data.Market.Symbol = "BTC"
	m.marketLastRefresh = time.Now().Add(-2 * time.Second)

	if cmd := m.ensureMarketCmd(); cmd != nil {
		t.Fatal("expected fresh market snapshot to skip refresh")
	}
}
