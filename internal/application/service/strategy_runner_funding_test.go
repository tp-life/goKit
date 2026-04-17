package service

import (
	"context"
	"io"
	"log/slog"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"
)

type testMarketDataRepo struct {
	funding        []entity.FundingSnapshot
	fundingHistory []entity.FundingRateHistory
}

type testFundingHistoryMarket struct {
	name     string
	enabled  bool
	history  map[string][]entity.FundingRateHistory
	requests []string
}

func ptrBool(v bool) *bool { return &v }

func (m *testFundingHistoryMarket) Name() string { return m.name }
func (m *testFundingHistoryMarket) Enabled() bool {
	return m.enabled
}
func (m *testFundingHistoryMarket) Fees() exchange.FeeConfig {
	return exchange.FeeConfig{}
}
func (m *testFundingHistoryMarket) Config() exchange.ExchangeConfig {
	return exchange.ExchangeConfig{}
}
func (m *testFundingHistoryMarket) FetchTradableSymbols(context.Context, string, map[string]struct{}) ([]entity.Symbol, error) {
	return nil, nil
}
func (m *testFundingHistoryMarket) Start(context.Context, exchange.MarketSubscriptionProvider, exchange.MarketSink) {
}
func (m *testFundingHistoryMarket) FetchFundingRateHistory(_ context.Context, symbol entity.Symbol, startTime, endTime time.Time) ([]entity.FundingRateHistory, error) {
	m.requests = append(m.requests, symbol.Symbol+"@"+startTime.UTC().Format(time.RFC3339)+"-"+endTime.UTC().Format(time.RFC3339))
	return append([]entity.FundingRateHistory(nil), m.history[strings.ToUpper(symbol.Symbol)]...), nil
}

func (r *testMarketDataRepo) SaveFundingSnapshots(context.Context, []entity.FundingSnapshot) error {
	return nil
}

func (r *testMarketDataRepo) SaveBookTopSnapshots(context.Context, []entity.BookTopSnapshot) error {
	return nil
}

func (r *testMarketDataRepo) SaveFundingRateHistory(_ context.Context, items []entity.FundingRateHistory) error {
	r.fundingHistory = append(r.fundingHistory, items...)
	return nil
}

func (r *testMarketDataRepo) RecentFundingSnapshots(_ context.Context, exchangeName, symbol string, since time.Time, limit int) ([]entity.FundingSnapshot, error) {
	out := make([]entity.FundingSnapshot, 0, len(r.funding))
	for _, item := range r.funding {
		if !strings.EqualFold(item.Exchange, exchangeName) || !strings.EqualFold(item.Symbol, symbol) {
			continue
		}
		if item.EventTimeMs < since.UnixMilli() {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventTimeMs > out[j].EventTimeMs })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *testMarketDataRepo) RecentFundingRateHistory(_ context.Context, exchangeName, symbol string, since time.Time, limit int) ([]entity.FundingRateHistory, error) {
	out := make([]entity.FundingRateHistory, 0, len(r.fundingHistory))
	for _, item := range r.fundingHistory {
		if !strings.EqualFold(item.Exchange, exchangeName) || !strings.EqualFold(item.Symbol, symbol) {
			continue
		}
		if item.FundingTimeMs < since.UnixMilli() {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FundingTimeMs > out[j].FundingTimeMs })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *testMarketDataRepo) DeleteOldFundingSnapshots(context.Context, time.Time) error {
	return nil
}

func (r *testMarketDataRepo) DeleteOldFundingRateHistory(context.Context, time.Time) error {
	return nil
}

func (r *testMarketDataRepo) DeleteOldBookTopSnapshots(context.Context, time.Time) error {
	return nil
}

func (r *testMarketDataRepo) CountSnapshotStats(context.Context, time.Time) (repository.SnapshotStats, error) {
	return repository.SnapshotStats{}, nil
}

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

func TestEffectiveOpportunityBatchTimePrefersActualHandlingTimeWhenTickerFallsBehind(t *testing.T) {
	tickAt := time.Date(2026, 4, 17, 9, 33, 20, 0, time.UTC)
	handledAt := tickAt.Add(14 * time.Second)

	got := effectiveOpportunityBatchTime(tickAt, handledAt)
	if !got.Equal(handledAt) {
		t.Fatalf("expected delayed loop to use actual handling time %s, got %s", handledAt, got)
	}
}

func TestEffectiveOpportunityBatchTimeKeepsTickerTimeWhenClockHasNotMovedPastIt(t *testing.T) {
	tickAt := time.Date(2026, 4, 17, 9, 33, 20, 0, time.UTC)
	handledAt := tickAt.Add(-2 * time.Second)

	got := effectiveOpportunityBatchTime(tickAt, handledAt)
	if !got.Equal(tickAt) {
		t.Fatalf("expected helper to keep ticker time %s, got %s", tickAt, got)
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

func TestBuildFundingRankContext_RanksFundingDescendingPerExchange(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewMarketStore()
	store.SetWatchlist([]string{"BTC", "ETH", "SOL"})
	for symbol, rate := range map[string]float64{
		"BTC": 0.0100,
		"ETH": 0.0040,
		"SOL": -0.0010,
	} {
		store.UpsertSymbol(entity.Symbol{
			Exchange:     "binance",
			Symbol:       symbol,
			VenueSymbol:  symbol + "USDT",
			ContractType: "PERPETUAL",
		})
		store.UpsertFunding(entity.FundingSnapshot{
			Exchange:             "binance",
			Symbol:               symbol,
			VenueSymbol:          symbol + "USDT",
			FundingRate:          rate,
			FundingTimeMs:        now.Add(time.Hour).UnixMilli(),
			FundingIntervalHours: 8,
			EventTimeMs:          now.UnixMilli(),
		})
	}

	runner := &StrategyRunner{
		cfg:   Config{MaxDataAge: time.Minute}.normalize(),
		store: store,
	}

	ranks := runner.buildFundingRankContext(now)
	if got := ranks.lookup("binance", "BTC"); got.Rank != 1 || got.Total != 3 {
		t.Fatalf("expected BTC rank #1/3, got %+v", got)
	}
	if got := ranks.lookup("binance", "ETH"); got.Rank != 2 {
		t.Fatalf("expected ETH rank #2, got %+v", got)
	}
	if got := ranks.lookup("binance", "SOL"); got.Rank != 3 || got.Percentile != 0 {
		t.Fatalf("expected SOL rank #3 with percentile 0, got %+v", got)
	}
}

func TestAssessSameExchangeLongHold_ComputesAnnualizedRatesAndEligibility(t *testing.T) {
	cfg := Config{
		MinNetPNL: 1.5,
		SameExchange: StrategySameExchangeConfig{
			Entry: StrategySameExchangeEntryConfig{
				MinHistorySampleCount:     10,
				MinHistoricalSupportRatio: 0.55,
				MinAnnualizedNetRate:      0.15,
			},
		},
	}.normalize()
	rule := entity.OpportunityFundingRule{FundingIntervalHours: 8}
	forecast := fundingForecast{
		HistoryMean:          0.0002,
		HistorySampleCount:   24,
		HistoryPositiveRatio: 0.70,
	}

	got := assessSameExchangeLongHold(cfg, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), rule, 0.0008, forecast, 1000, 3)
	if !got.Eligible {
		t.Fatalf("expected long-hold assessment to be eligible, got %+v", got)
	}
	if got.HistoricalSupportRatio != 0.70 {
		t.Fatalf("expected support ratio 0.70, got %.4f", got.HistoricalSupportRatio)
	}
	wantCarry := (0.0008*0.70 + 0.0002*0.30) * (24 * 365 / 8.0)
	if math.Abs(got.EstimatedAnnualizedCarryRate-wantCarry) > 1e-9 {
		t.Fatalf("expected annualized carry %.10f, got %.10f", wantCarry, got.EstimatedAnnualizedCarryRate)
	}
	wantNet := wantCarry - 3.0/1000.0
	if math.Abs(got.EstimatedAnnualizedNetRate-wantNet) > 1e-9 {
		t.Fatalf("expected annualized net %.10f, got %.10f", wantNet, got.EstimatedAnnualizedNetRate)
	}
}

func TestAssessSameExchangeLongHold_SuggestsHoldToReachMinNetPNL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg := Config{
		MinNetPNL: 1.5,
		Prediction: StrategyPredictionConfig{
			FundingRateHistoryLookback: 30 * 24 * time.Hour,
		},
		SameExchange: StrategySameExchangeConfig{
			Entry: StrategySameExchangeEntryConfig{
				MinHistorySampleCount:     10,
				MinHistoricalSupportRatio: 0.55,
				MinAnnualizedNetRate:      0.15,
			},
		},
	}.normalize()
	rule := entity.OpportunityFundingRule{
		FundingIntervalHours: 8,
		NextFundingTimeMs:    now.Add(2 * time.Hour).UnixMilli(),
	}
	forecast := fundingForecast{
		HistoryMean:          0.0002,
		HistorySampleCount:   24,
		HistoryPositiveRatio: 0.75,
	}

	got := assessSameExchangeLongHold(cfg, now, rule, 0.0004, forecast, 1000, 1.1)
	if !got.Eligible {
		t.Fatalf("expected long-hold assessment to stay eligible, got %+v", got)
	}
	if got.SuggestedFundingEvents != 8 {
		t.Fatalf("expected 8 suggested funding events, got %d", got.SuggestedFundingEvents)
	}
	if math.Abs(got.SuggestedHoldHours-58) > 1e-9 {
		t.Fatalf("expected suggested hold hours 58, got %.4f", got.SuggestedHoldHours)
	}
	if got.SuggestedFundingTimeMs != now.Add(58*time.Hour).UnixMilli() {
		t.Fatalf("expected suggested funding time %d, got %d", now.Add(58*time.Hour).UnixMilli(), got.SuggestedFundingTimeMs)
	}
	if got.SuggestedNetPNL < cfg.MinNetPNL {
		t.Fatalf("expected suggested net pnl >= min %.2f, got %.4f", cfg.MinNetPNL, got.SuggestedNetPNL)
	}
}

func TestBuildOpportunityFromDirectionCandidate_SameExchangePromotesHistoricalLongHoldEstimate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runner := &StrategyRunner{
		cfg: Config{
			ArbitrageMode:      ArbitrageModeSameExchangeSpotPerp,
			TotalCapitalUSDT:   1000,
			CapitalUtilization: 1,
			Leverage:           1,
			MinNetPNL:          1.5,
			SameExchange: StrategySameExchangeConfig{
				Entry: StrategySameExchangeEntryConfig{
					RequireLongHoldEligible:   ptrBool(true),
					MinHistorySampleCount:     10,
					MinHistoricalSupportRatio: 0.55,
					MinAnnualizedNetRate:      0.15,
				},
			},
			Prediction: StrategyPredictionConfig{
				FundingRateHistoryLookback: 30 * 24 * time.Hour,
			},
			SpreadGuard: StrategySpreadGuardConfig{
				EntryLeadTime:   4 * time.Hour,
				EntryCutoffTime: 30 * time.Second,
			},
		}.normalize(),
		store:      NewMarketStore(),
		marketRepo: &testMarketDataRepo{},
	}

	selected := evaluatedDirectionCandidate{
		LongExchange:  "binance_spot",
		ShortExchange: "binance",
		LongFunding: entity.FundingSnapshot{
			Exchange:             "binance_spot",
			Symbol:               "BTC",
			VenueSymbol:          "BTCUSDT",
			FundingRate:          0,
			FundingTimeMs:        now.Add(2 * time.Hour).UnixMilli(),
			FundingIntervalHours: 8,
			EventTimeMs:          now.UnixMilli(),
			MarkPrice:            100,
		},
		ShortFunding: entity.FundingSnapshot{
			Exchange:             "binance",
			Symbol:               "BTC",
			VenueSymbol:          "BTCUSDT",
			FundingRate:          0.0006,
			FundingTimeMs:        now.Add(2 * time.Hour).UnixMilli(),
			FundingIntervalHours: 8,
			EventTimeMs:          now.UnixMilli(),
			MarkPrice:            100.2,
		},
		LongBook: entity.BookTopSnapshot{
			Exchange:    "binance_spot",
			Symbol:      "BTC",
			VenueSymbol: "BTCUSDT",
			BidPrice:    100,
			AskPrice:    100.1,
			EventTimeMs: now.UnixMilli(),
		},
		ShortBook: entity.BookTopSnapshot{
			Exchange:    "binance",
			Symbol:      "BTC",
			VenueSymbol: "BTCUSDT",
			BidPrice:    100.2,
			AskPrice:    100.3,
			EventTimeMs: now.UnixMilli(),
		},
		LongMeta: entity.Symbol{
			Exchange:     "binance_spot",
			Symbol:       "BTC",
			VenueSymbol:  "BTCUSDT",
			ContractType: "SPOT",
		},
		ShortMeta: entity.Symbol{
			Exchange:             "binance",
			Symbol:               "BTC",
			VenueSymbol:          "BTCUSDT",
			ContractType:         "PERPETUAL",
			FundingIntervalHours: 8,
		},
		LongForecast: syntheticSpotFundingForecast(),
		ShortForecast: fundingForecast{
			CurrentRate:                 0.0006,
			BaselineRate:                0.0006,
			HistoryMean:                 0.0002,
			HistorySampleCount:          24,
			HistoryPositiveRatio:        0.8,
			CurrentHistoricalPercentile: 0.92,
			Regime:                      "positive_premium",
			Confidence:                  "medium",
			MeanReversion:               0.20,
			ContinuationDecay:           0.5,
			EffectiveFloorRate:          0.0001,
			EffectiveCapRate:            0.001,
			ClampSource:                 "history_band",
		},
		EntryFeePNL: 0.5,
		ExitFeePNL:  0.5,
		BasisBps:    10,
		WindowEvaluations: []opportunityProjectionEvaluation{
			{
				Projection: fundingProjection{
					ProjectedFundingTimeMs:       now.Add(2 * time.Hour).UnixMilli(),
					RequiredEntryByFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(),
					LongFundingEventCount:        1,
					ShortFundingEventCount:       1,
					FundingWindowHours:           2,
					CarryRate:                    0.001,
					CarryRateHourlyEquivalent:    0.0005,
					ComputationMode:              "legacy_projection",
					StrategyMode:                 StrategyModeLegacyProjection,
				},
				Detail: entity.OpportunityProjection{
					ProjectionRank:               1,
					IsBestProjection:             true,
					ProjectedFundingTimeMs:       now.Add(2 * time.Hour).UnixMilli(),
					RequiredEntryByFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(),
					LongFundingEventCount:        1,
					ShortFundingEventCount:       1,
					FundingWindowHours:           2,
					CarryRate:                    0.001,
					CarryRateHourlyEquivalent:    0.0005,
					GrossFundingPNL:              1.0,
					NetExpectedPNL:               -0.6,
					NetExpectedBps:               -6,
					ComputationMode:              "legacy_projection",
					StrategyMode:                 StrategyModeLegacyProjection,
				},
				MaxAllowedBasisBps: 12,
				SlippagePNL:        0.4,
				SafetyBufferPNL:    0.2,
			},
		},
		PrimaryWindow: opportunityProjectionEvaluation{
			Projection: fundingProjection{
				ProjectedFundingTimeMs:       now.Add(2 * time.Hour).UnixMilli(),
				RequiredEntryByFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(),
				LongFundingEventCount:        1,
				ShortFundingEventCount:       1,
				FundingWindowHours:           2,
				CarryRate:                    0.001,
				CarryRateHourlyEquivalent:    0.0005,
				ComputationMode:              "legacy_projection",
				StrategyMode:                 StrategyModeLegacyProjection,
			},
			Detail: entity.OpportunityProjection{
				ProjectionRank:               1,
				IsBestProjection:             true,
				ProjectedFundingTimeMs:       now.Add(2 * time.Hour).UnixMilli(),
				RequiredEntryByFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(),
				LongFundingEventCount:        1,
				ShortFundingEventCount:       1,
				FundingWindowHours:           2,
				CarryRate:                    0.001,
				CarryRateHourlyEquivalent:    0.0005,
				GrossFundingPNL:              1.0,
				NetExpectedPNL:               -0.6,
				NetExpectedBps:               -6,
				ComputationMode:              "legacy_projection",
				StrategyMode:                 StrategyModeLegacyProjection,
			},
			MaxAllowedBasisBps: 12,
			SlippagePNL:        0.4,
			SafetyBufferPNL:    0.2,
		},
	}

	opp := buildOpportunityFromDirectionCandidate(runner, now, "BTC", selected, fundingRankContext{})
	if !opp.SameExchangeLongHoldUsingHistoryEstimate {
		t.Fatal("expected same-exchange opportunity to use historical long-hold estimate")
	}
	if !opp.EligibleForExecution || opp.Status != OpportunityStatusEligible {
		t.Fatalf("expected opportunity to become eligible via historical long-hold estimate, got status=%s eligible=%v reason=%s", opp.Status, opp.EligibleForExecution, opp.RejectReason)
	}
	if opp.NetExpectedPNL < runner.cfg.MinNetPNL {
		t.Fatalf("expected promoted net pnl >= min %.2f, got %.4f", runner.cfg.MinNetPNL, opp.NetExpectedPNL)
	}
	if opp.FundingComputationMode != "same_exchange_long_hold_history" {
		t.Fatalf("expected historical long-hold computation mode, got %s", opp.FundingComputationMode)
	}
	if opp.ShortFundingRule.SuggestedFundingEvents <= 1 {
		t.Fatalf("expected suggested funding events > 1, got %d", opp.ShortFundingRule.SuggestedFundingEvents)
	}
	if len(opp.ProjectionDetails) == 0 || !opp.ProjectionDetails[0].IsBestProjection {
		t.Fatalf("expected first projection row to be injected as best historical projection, got %+v", opp.ProjectionDetails)
	}
}

func TestSameExchangeHistoricalLongHoldProjectionForPlan_UsesAdjustedHistoricalRate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	plan := &entity.ExecutionPlan{
		ArbitrageMode:                              ArbitrageModeSameExchangeSpotPerp,
		SameExchangeLongHoldUsingHistoryEstimate:   true,
		SameExchangeLongHoldSuggestedFundingEvents: 4,
		PerpFundingHistorySampleCount:              20,
		PerpFundingHistoryMeanRate:                 0.0002,
		PerpFundingHistoricalSupportRatio:          0.75,
		RequiredEntryByFundingTimeMs:               now.Add(time.Hour).UnixMilli(),
	}
	perpFunding := entity.FundingSnapshot{
		Exchange:             "binance",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingRate:          0.0006,
		FundingTimeMs:        now.Add(2 * time.Hour).UnixMilli(),
		FundingIntervalHours: 8,
	}

	got, ok := sameExchangeHistoricalLongHoldProjectionForPlan(now, plan, perpFunding)
	if !ok {
		t.Fatal("expected plan revalidation projection to be available")
	}
	wantCarry := (0.0006*0.75 + 0.0002*0.25) * 4
	if math.Abs(got.CarryRate-wantCarry) > 1e-9 {
		t.Fatalf("expected carry %.10f, got %.10f", wantCarry, got.CarryRate)
	}
	if got.ProjectedFundingTimeMs != now.Add(26*time.Hour).UnixMilli() {
		t.Fatalf("expected projected funding time %d, got %d", now.Add(26*time.Hour).UnixMilli(), got.ProjectedFundingTimeMs)
	}
	if got.ComputationMode != "same_exchange_long_hold_history" {
		t.Fatalf("expected historical long-hold computation mode, got %s", got.ComputationMode)
	}
}

func TestSyncFundingRateHistory_SavesExchangeHistoryForPerpetualTargets(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	repo := &testMarketDataRepo{}
	market := &testFundingHistoryMarket{
		name:    "binance",
		enabled: true,
		history: map[string][]entity.FundingRateHistory{
			"BTC": {
				{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", FundingRate: 0.0012, FundingTimeMs: now.Add(-8 * time.Hour).UnixMilli()},
				{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", FundingRate: -0.0008, FundingTimeMs: now.Add(-16 * time.Hour).UnixMilli()},
			},
		},
	}
	runner := &StrategyRunner{
		cfg: Config{
			FundingRateHistoryLookback: 30 * 24 * time.Hour,
		}.normalize(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		marketRepo: repo,
		markets: map[string]exchange.MarketAdapter{
			"binance": market,
		},
		fundingSymbolsByExchange: map[string][]entity.Symbol{
			"binance": {
				{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "PERPETUAL"},
			},
			"binance_spot": {
				{Exchange: "binance_spot", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "SPOT"},
			},
		},
	}

	runner.syncFundingRateHistory(context.Background(), now)

	if len(market.requests) != 1 {
		t.Fatalf("expected exactly one perpetual history sync request, got %d", len(market.requests))
	}
	if len(repo.fundingHistory) != 2 {
		t.Fatalf("expected 2 saved funding history rows, got %d", len(repo.fundingHistory))
	}
}

func TestSyncFundingRateHistory_UsesIncrementalStartAndPrioritizesMissingSymbols(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	repo := &testMarketDataRepo{
		fundingHistory: []entity.FundingRateHistory{
			{
				Exchange:      "binance",
				Symbol:        "BTC",
				VenueSymbol:   "BTCUSDT",
				FundingRate:   0.0012,
				FundingTimeMs: now.Add(-4 * time.Hour).UnixMilli(),
			},
		},
	}
	market := &testFundingHistoryMarket{
		name:    "binance",
		enabled: true,
		history: map[string][]entity.FundingRateHistory{
			"AAA": {
				{Exchange: "binance", Symbol: "AAA", VenueSymbol: "AAAUSDT", FundingRate: 0.0010, FundingTimeMs: now.Add(-8 * time.Hour).UnixMilli()},
			},
			"BTC": {
				{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", FundingRate: 0.0015, FundingTimeMs: now.Add(-3 * time.Hour).UnixMilli()},
			},
		},
	}
	runner := &StrategyRunner{
		cfg: Config{
			FundingRateHistoryLookback: 30 * 24 * time.Hour,
		}.normalize(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		marketRepo: repo,
		markets: map[string]exchange.MarketAdapter{
			"binance": market,
		},
		fundingSymbolsByExchange: map[string][]entity.Symbol{
			"binance": {
				{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "PERPETUAL", FundingIntervalHours: 1},
				{Exchange: "binance", Symbol: "AAA", VenueSymbol: "AAAUSDT", ContractType: "PERPETUAL", FundingIntervalHours: 8},
			},
		},
	}

	runner.syncFundingRateHistory(context.Background(), now)

	if len(market.requests) != 2 {
		t.Fatalf("expected 2 sync requests, got %d", len(market.requests))
	}
	if !strings.HasPrefix(market.requests[0], "AAA@") {
		t.Fatalf("expected missing symbol AAA to be requested first, got %#v", market.requests)
	}
	if !strings.Contains(market.requests[1], "BTC@2026-01-02T08:00:00Z-2026-01-02T12:00:00Z") {
		t.Fatalf("expected BTC request to resume from latest local funding time, got %#v", market.requests)
	}
}

func TestSyncFundingRateHistory_SkipsSymbolsWithNoDueFundingEvent(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	repo := &testMarketDataRepo{
		fundingHistory: []entity.FundingRateHistory{
			{
				Exchange:      "binance",
				Symbol:        "BTC",
				VenueSymbol:   "BTCUSDT",
				FundingRate:   0.0012,
				FundingTimeMs: now.Add(-2 * time.Hour).UnixMilli(),
			},
		},
	}
	market := &testFundingHistoryMarket{
		name:    "binance",
		enabled: true,
		history: map[string][]entity.FundingRateHistory{},
	}
	runner := &StrategyRunner{
		cfg: Config{
			FundingRateHistoryLookback: 30 * 24 * time.Hour,
		}.normalize(),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		marketRepo: repo,
		markets: map[string]exchange.MarketAdapter{
			"binance": market,
		},
		fundingSymbolsByExchange: map[string][]entity.Symbol{
			"binance": {
				{Exchange: "binance", Symbol: "BTC", VenueSymbol: "BTCUSDT", ContractType: "PERPETUAL", FundingIntervalHours: 8},
			},
		},
	}

	runner.syncFundingRateHistory(context.Background(), now)

	if len(market.requests) != 0 {
		t.Fatalf("expected fresh symbol with no due funding event to be skipped, got %#v", market.requests)
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

func TestBuildFundingCandidateTimesForConfig_DynamicProfitIgnoresHoldHours(t *testing.T) {
	cfg := Config{
		HoldHours:         1,
		HoldSelectionMode: HoldSelectionModeDynamicProfit,
		StrategyMode:      StrategyModeLegacyProjection,
	}
	now := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC).UnixMilli()

	long := entity.FundingSnapshot{
		FundingTimeMs:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 4,
	}
	short := entity.FundingSnapshot{
		FundingTimeMs:        time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 1,
	}

	times := buildFundingCandidateTimesForConfig(cfg, now, long, short)
	if len(times) == 0 {
		t.Fatal("expected dynamic profit mode to return candidate times")
	}
	found22h := false
	for _, ts := range times {
		if ts == time.Date(2026, 1, 1, 22, 0, 0, 0, time.UTC).UnixMilli() {
			found22h = true
			break
		}
	}
	if !found22h {
		t.Fatalf("expected dynamic profit mode to search beyond hold_hours and include 22:00, got %v", times)
	}
}

func TestProjectFundingCarry_DynamicProfitSelectsBestCarryBeyondHoldHours(t *testing.T) {
	r := &StrategyRunner{cfg: Config{
		HoldHours:                    1,
		HoldSelectionMode:            HoldSelectionModeDynamicProfit,
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
	if minWant := time.Date(2026, 1, 1, 22, 0, 0, 0, time.UTC).UnixMilli(); projection.ProjectedFundingTimeMs <= minWant {
		t.Fatalf("expected dynamic profit mode to select a window beyond hold_hours, got %d", projection.ProjectedFundingTimeMs)
	}
	if projection.LongFundingEventCount <= 1 || projection.ShortFundingEventCount <= 4 {
		t.Fatalf("expected dynamic profit window to accumulate more events than the hold_hours-limited 22:00 window, got long=%d short=%d", projection.LongFundingEventCount, projection.ShortFundingEventCount)
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

func TestStrategyRunnerForecastFunding_RollingModePreservesHistoryContext(t *testing.T) {
	now := time.Date(2026, 1, 1, 13, 48, 0, 0, time.UTC)
	repo := &testMarketDataRepo{
		fundingHistory: []entity.FundingRateHistory{
			{Exchange: "binance", Symbol: "BTC", FundingRate: -0.0020, FundingTimeMs: now.Add(-5 * time.Hour).UnixMilli()},
			{Exchange: "binance", Symbol: "BTC", FundingRate: -0.0010, FundingTimeMs: now.Add(-4 * time.Hour).UnixMilli()},
			{Exchange: "binance", Symbol: "BTC", FundingRate: 0.0015, FundingTimeMs: now.Add(-3 * time.Hour).UnixMilli()},
			{Exchange: "binance", Symbol: "BTC", FundingRate: 0.0020, FundingTimeMs: now.Add(-2 * time.Hour).UnixMilli()},
		},
	}
	cfg := Config{
		StrategyMode:                  StrategyModeRollingCycleAligned,
		FundingRateContinuationDecay:  0.3,
		FundingSmoothingCurrentWeight: 0.1,
		FundingHistoryLookback:        6 * time.Hour,
		OpportunityCalcInterval:       5 * time.Second,
	}
	r := &StrategyRunner{
		cfg:        cfg,
		forecaster: NewFundingForecaster(cfg, repo, nil),
	}

	item := entity.FundingSnapshot{
		Exchange:             "binance",
		Symbol:               "BTC",
		FundingRate:          0.0100,
		FundingTimeMs:        time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC).UnixMilli(),
		FundingIntervalHours: 8,
	}

	forecast := r.forecastFunding(context.Background(), now, item)
	if forecast.Regime != "rolling_real_only" {
		t.Fatalf("expected rolling forecast to keep history context with rolling_real_only regime, got %s", forecast.Regime)
	}
	if forecast.HistorySampleCount != 4 {
		t.Fatalf("expected rolling forecast to keep 4 history samples, got %d", forecast.HistorySampleCount)
	}
	if forecast.HistoryNegativeRatio != 0.5 {
		t.Fatalf("expected 50%% negative history ratio, got %.4f", forecast.HistoryNegativeRatio)
	}
	if forecast.HistoryPositiveRatio != 0.5 {
		t.Fatalf("expected 50%% positive history ratio, got %.4f", forecast.HistoryPositiveRatio)
	}
	if forecast.CurrentHistoricalPercentile != 1 {
		t.Fatalf("expected current rate percentile to be 1.0, got %.4f", forecast.CurrentHistoricalPercentile)
	}
	if got := forecast.PredictedRateForEvent(2); got != item.FundingRate {
		t.Fatalf("expected rolling forecast to keep future event on current rate %.8f, got %.8f", item.FundingRate, got)
	}
	if forecast.EffectiveFloorRate != item.FundingRate || forecast.EffectiveCapRate != item.FundingRate {
		t.Fatalf("expected rolling clamp to stay on current rate %.8f, got [%.8f, %.8f]", item.FundingRate, forecast.EffectiveFloorRate, forecast.EffectiveCapRate)
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

func TestAssessSameExchangeBasisRisk_ShortWindowSoftensToDownsize(t *testing.T) {
	cfg := Config{
		ArbitrageMode: ArbitrageModeSameExchangeSpotPerp,
		MaxSpreadBps:  12,
		SameExchange: StrategySameExchangeConfig{
			Entry: StrategySameExchangeEntryConfig{
				BasisLongHoldWindowHours: 12,
				MaxBasisPaybackEvents:    4,
			},
			Risk: StrategySameExchangeRiskConfig{
				ExtremeBasisPaybackEvents:  2.5,
				ExtremeBasisSizeMultiplier: 0.65,
			},
		},
	}.normalize()

	got := assessSameExchangeBasisRisk(cfg, ArbitrageModeSameExchangeSpotPerp, 6, 0.006, 1, 1, 18, 2, 1000, 12)
	if !got.Allowed {
		t.Fatalf("expected short-window basis to stay eligible, got %+v", got)
	}
	if got.Reason != sameExchangeBasisReasonShortWindowTooWide {
		t.Fatalf("expected short-window reason %s, got %s", sameExchangeBasisReasonShortWindowTooWide, got.Reason)
	}
	if got.UsesPaybackModel {
		t.Fatalf("expected short-window guard to skip payback model, got %+v", got)
	}
	if got.SizeMultiplier != 0.65 {
		t.Fatalf("expected short-window basis to downsize to 0.65, got %+v", got)
	}
}

func TestAssessSameExchangeBasisRisk_LongWindowUsesPayback(t *testing.T) {
	cfg := Config{
		ArbitrageMode: ArbitrageModeSameExchangeSpotPerp,
		MaxSpreadBps:  12,
		SameExchange: StrategySameExchangeConfig{
			Entry: StrategySameExchangeEntryConfig{
				BasisLongHoldWindowHours: 12,
				MaxBasisPaybackEvents:    4,
			},
			Risk: StrategySameExchangeRiskConfig{
				ExtremeBasisPaybackEvents:  2.5,
				ExtremeBasisSizeMultiplier: 0.65,
			},
		},
	}.normalize()

	got := assessSameExchangeBasisRisk(cfg, ArbitrageModeSameExchangeSpotPerp, 24, 0.009, 0, 3, 24, 3, 1000, 18)
	if !got.Allowed {
		t.Fatalf("expected long-window payback guard to allow opportunity, got %+v", got)
	}
	if !got.UsesPaybackModel {
		t.Fatalf("expected long-window guard to use payback model, got %+v", got)
	}
	if got.PaybackFundingEvents <= 0 || got.PaybackFundingEvents >= 2 {
		t.Fatalf("expected payback events below 2, got %+v", got)
	}
}

func TestAssessSameExchangeBasisRisk_SlowPaybackDownsizesInsteadOfRejecting(t *testing.T) {
	cfg := Config{
		ArbitrageMode: ArbitrageModeSameExchangeSpotPerp,
		MaxSpreadBps:  12,
		SameExchange: StrategySameExchangeConfig{
			Entry: StrategySameExchangeEntryConfig{
				BasisLongHoldWindowHours: 12,
				MaxBasisPaybackEvents:    4,
			},
			Risk: StrategySameExchangeRiskConfig{
				ExtremeBasisPaybackEvents:  2.5,
				ExtremeBasisSizeMultiplier: 0.65,
			},
		},
	}.normalize()

	got := assessSameExchangeBasisRisk(cfg, ArbitrageModeSameExchangeSpotPerp, 24, 0.0015, 0, 3, 24, 4, 1000, 18)
	if !got.Allowed {
		t.Fatalf("expected slow payback to remain eligible with reduced size, got %+v", got)
	}
	if got.Reason != sameExchangeBasisReasonPaybackTooHigh {
		t.Fatalf("expected payback-too-high reason, got %+v", got)
	}
	if got.SizeMultiplier != 0.65 {
		t.Fatalf("expected slow payback to downsize to 0.65, got %+v", got)
	}
}

func TestAssessSameExchangePriceRisk_BlocksAbnormal1hPump(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := &testMarketDataRepo{
		funding: []entity.FundingSnapshot{
			{
				Exchange:    "binance",
				Symbol:      "BTC",
				MarkPrice:   100,
				EventTimeMs: now.Add(-55 * time.Minute).UnixMilli(),
			},
			{
				Exchange:    "binance",
				Symbol:      "BTC",
				MarkPrice:   104,
				EventTimeMs: now.Add(-20 * time.Minute).UnixMilli(),
			},
		},
	}
	cfg := Config{
		ArbitrageMode: ArbitrageModeSameExchangeSpotPerp,
		SameExchange: StrategySameExchangeConfig{
			Risk: StrategySameExchangeRiskConfig{
				Max1hPriceShockRatio: 0.08,
			},
		},
	}.normalize()

	got := assessSameExchangePriceRisk(
		context.Background(),
		cfg,
		repo,
		ArbitrageModeSameExchangeSpotPerp,
		"binance",
		"BTC",
		entity.FundingSnapshot{
			Exchange:    "binance",
			Symbol:      "BTC",
			MarkPrice:   109,
			EventTimeMs: now.UnixMilli(),
		},
		now,
	)
	if got.Allowed {
		t.Fatalf("expected abnormal 1h pump to be blocked, got %+v", got)
	}
	if got.Reason != sameExchangePriceRiskReason1hPriceShock {
		t.Fatalf("expected 1h price shock reason, got %+v", got)
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
