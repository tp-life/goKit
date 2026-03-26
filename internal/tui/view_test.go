package tui

import (
	"strings"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

func TestFeeBreakdownText_UsesConfiguredModePerExchange(t *testing.T) {
	item := OpportunityListItem{
		LongExchange:  "binance",
		ShortExchange: "hyperliquid",
	}
	strategy := StrategyStatus{
		FeesByExchange: map[string]exchange.FeeConfig{
			"binance":     {MakerBps: 1.00, TakerBps: 4.00},
			"hyperliquid": {MakerBps: 1.50, TakerBps: 4.50},
		},
	}

	got := feeBreakdownText(item, strategy, "maker")
	if got != "binance maker 1.00 bps + hyperliquid maker 1.50 bps" {
		t.Fatalf("unexpected fee breakdown: %q", got)
	}
}

func TestRenderPnLBreakdownDetail_IncludesFormulaAndComponents(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		EffectiveNotional: 1600,
		EntryMode:         "maker",
		ExitMode:          "taker",
		FeesByExchange: map[string]exchange.FeeConfig{
			"binance": {MakerBps: 1.00, TakerBps: 4.00},
			"aster":   {MakerBps: 1.20, TakerBps: 4.20},
		},
	}
	item := OpportunityListItem{
		LongExchange:           "binance",
		ShortExchange:          "aster",
		GrossFundingPNL:        8.0,
		EntryFeePNL:            0.4,
		ExitFeePNL:             0.5,
		SlippagePNL:            0.3,
		SafetyBufferPNL:        0.2,
		NetExpectedPNL:         6.6,
		ShortFundingRate:       0.0020,
		LongFundingRate:        0.0010,
		GrossEdgeHourly:        0.0002,
		LongFundingEventCount:  1,
		ShortFundingEventCount: 1,
	}

	got := m.renderPnLBreakdownDetail(item, nil, false, entity.ExecutionPlan{}, false, 160)
	for _, needle := range []string{
		"公式: 净收益 = 资金收益 - 入场手续费 - 出场手续费 - 滑点 - 安全缓冲",
		"资金收益",
		"入场手续费",
		"出场手续费",
		"滑点预估",
		"安全缓冲",
		"binance maker 1.00 bps + aster maker 1.20 bps",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected pnl breakdown to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderHeader_IncludesHardLimits(t *testing.T) {
	m := NewModel(NewClient("http://127.0.0.1:8080", "", 0), 8*time.Second)
	m.data.System.Strategy = StrategyStatus{
		MinNetPNL:     1.5,
		MaxSpreadBps:  12,
		EntryLeadTime: "45m0s",
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "5m0s",
	}

	got := m.renderHeader(240)
	for _, needle := range []string{
		"min_pnl 1.500U",
		"max_spread 12.00bps",
		"entry_lead 45m0s",
		"close_grace 5m0s",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected header to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderOverviewDetail_ShowsLegsBeforePnLBreakdown(t *testing.T) {
	m := NewModel(nil, 0)
	item := OpportunityListItem{
		Symbol:                 "BTC",
		LongExchange:           "binance",
		ShortExchange:          "aster",
		LongVenueSymbol:        "BTCUSDT",
		ShortVenueSymbol:       "BTCUSDT",
		NetExpectedPNL:         6.6,
		NetExpectedBps:         12,
		LongFundingTimeMs:      time.Now().Add(time.Hour).UnixMilli(),
		ShortFundingTimeMs:     time.Now().Add(2 * time.Hour).UnixMilli(),
		ProjectedFundingTimeMs: time.Now().Add(2 * time.Hour).UnixMilli(),
		FundingWindowHours:     2,
		FundingComputationMode: "event_window",
	}

	got := m.renderOverviewDetail(item, nil, false, false, entity.ExecutionPlan{}, false, entity.ExecutionRecord{}, false, 180)
	legsIdx := strings.Index(got, "双腿信息")
	pnlIdx := strings.Index(got, "收益构成")
	if legsIdx < 0 || pnlIdx < 0 {
		t.Fatalf("expected overview to contain both sections, got %q", got)
	}
	if legsIdx > pnlIdx {
		t.Fatalf("expected legs section before pnl breakdown, got %q", got)
	}
}

func TestRenderLegsDetail_RendersComparisonTable(t *testing.T) {
	item := OpportunityListItem{
		LongExchange:           "binance",
		ShortExchange:          "aster",
		LongVenueSymbol:        "BTCUSDT",
		ShortVenueSymbol:       "BTCUSDT",
		LongFundingRate:        0.0012,
		ShortFundingRate:       0.0023,
		LongFutureFundingRate:  0.0011,
		ShortFutureFundingRate: 0.0021,
		LongFundingHourly:      0.0003,
		ShortFundingHourly:     0.0005,
		LongFundingTimeMs:      time.Now().Add(time.Hour).UnixMilli(),
		ShortFundingTimeMs:     time.Now().Add(2 * time.Hour).UnixMilli(),
	}

	got := NewModel(nil, 0).renderLegsDetail(item, nil, false, false, 180)
	for _, needle := range []string{"指标", "做多腿", "做空腿", "交易所", "当前费率", "标记价"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected legs detail table to contain %q, got %q", needle, got)
		}
	}
}
