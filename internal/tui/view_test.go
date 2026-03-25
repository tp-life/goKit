package tui

import (
	"strings"
	"testing"

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
