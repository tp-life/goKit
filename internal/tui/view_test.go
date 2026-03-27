package tui

import (
	"strings"
	"testing"
	"time"

	"goKit/internal/application/service"
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
		MinNetPNL:         1.5,
		MaxSpreadBps:      12,
		EntryLeadTime:     "45m0s",
		HoldSelectionMode: "latest_profitable",
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "5m0s",
	}

	got := m.renderHeader(240)
	for _, needle := range []string{
		"最小收益 1.500U",
		"最大价差 12.00bps",
		"持有模式 最晚盈利窗口",
		"提前开仓 45m0s",
		"平仓缓冲 5m0s",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected header to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderConfigPanel_ShowsHoldSelectionMode(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		Mode:                                 service.StrategyModeRollingCycleAligned,
		Enabled:                              true,
		HoldHours:                            4,
		HoldSelectionMode:                    "latest_profitable",
		Leverage:                             2,
		MinNetPNL:                            1.5,
		EffectiveNotional:                    1600,
		EntryMode:                            "taker",
		ExitMode:                             "taker",
		MaxSpreadBps:                         12,
		MaxDataAge:                           "15s",
		RollingReviewSettleGracePeriod:       "15s",
		RollingReviewFreshSnapshotMaxWait:    "20s",
		RollingReviewContinueOnSameDirection: true,
		RollingReviewCloseOnUnprofitable:     true,
		RollingReviewMinIncrementalNetPNL:    1.5,
		RollingFlipEnabled:                   true,
		RollingFlipRequireNetPositive:        true,
		RollingFlipMinNetPNL:                 1.8,
		RollingFlipSlippageMultiplier:        1.5,
		RollingFlipExtraSafetyBufferUSDT:     0.3,
	}
	m.data.System.Execution = ExecutionStatus{
		LiveTradingEnabled:    false,
		AutoEntry:             true,
		AutoClose:             true,
		LoopInterval:          "3s",
		CloseGracePeriod:      "15s",
		MaxLatestPlans:        20,
		AutoAllocateCapital:   true,
		ActiveRollingRecords:  1,
		ActiveRollingGroups:   1,
		RollingDueReviews:     1,
		RollingWaitingReviews: 2,
	}

	got := m.renderConfigPanel(180, 12)
	for _, needle := range []string{
		"模式=按结算段滚动",
		"持有上限=4.0h",
		"持有模式=最晚盈利窗口",
		"结算后缓冲=15s",
		"Rolling 监控: 记录=1  分组=1  待 Review=1  未到 Review=2",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected config panel to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderOverviewDetail_ShowsLegsBeforePnLBreakdown(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		Mode:              service.StrategyModeRollingCycleAligned,
		HoldHours:         4,
		HoldSelectionMode: "latest_profitable",
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "15s",
	}
	item := OpportunityListItem{
		Symbol:                 "BTC",
		LongExchange:           "binance",
		ShortExchange:          "aster",
		StrategyMode:           service.StrategyModeRollingCycleAligned,
		LongVenueSymbol:        "BTCUSDT",
		ShortVenueSymbol:       "BTCUSDT",
		NetExpectedPNL:         6.6,
		NetExpectedBps:         12,
		LongFundingTimeMs:      time.Now().Add(time.Hour).UnixMilli(),
		ShortFundingTimeMs:     time.Now().Add(2 * time.Hour).UnixMilli(),
		ProjectedFundingTimeMs: time.Now().Add(2 * time.Hour).UnixMilli(),
		NextReviewTimeMs:       time.Now().Add(time.Hour).UnixMilli(),
		SyncBoundaryTimeMs:     time.Now().Add(2 * time.Hour).UnixMilli(),
		EntryPathSegmentCount:  2,
		EntryPathStopReason:    "sync_boundary",
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
	for _, needle := range []string{
		"持有模式=最晚盈利窗口",
		"持有上限=4.0h",
		"策略模式=按结算段滚动",
		"下次 Review=",
		"共享 Boundary=",
		"预计平仓=",
		"预计总持有=",
		"结算后缓冲=15s",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected overview detail to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderOverviewDetail_PrefersDetailedBestProjectionCarry(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		HoldHours:         4,
		HoldSelectionMode: "latest_profitable",
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "15s",
	}

	item := OpportunityListItem{
		Symbol:                 "KAT",
		LongExchange:           "aster",
		ShortExchange:          "binance",
		NetExpectedPNL:         6.6,
		NetExpectedBps:         12,
		LongFundingRate:        -0.0070,
		ShortFundingRate:       -0.0200,
		GrossEdgeHourly:        0.0030,
		ProjectedFundingTimeMs: time.Now().Add(time.Hour).UnixMilli(),
	}
	detail := &entity.Opportunity{
		ProjectionDetails: []entity.OpportunityProjection{
			{
				IsBestProjection:          true,
				CarryRate:                 0.0061409,
				CarryRateHourlyEquivalent: 0.0133977,
			},
		},
	}

	got := m.renderOverviewDetail(item, detail, true, false, entity.ExecutionPlan{}, false, entity.ExecutionRecord{}, false, 180)
	for _, needle := range []string{
		"优选 Carry率=0.61409%",
		"优选时均边际=1.33977%",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected overview detail to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderPlanDetail_ShowsHoldModeAndTiming(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		Mode:              service.StrategyModeRollingCycleAligned,
		HoldHours:         4,
		HoldSelectionMode: "latest_profitable",
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "15s",
	}

	now := time.Now()
	plan := entity.ExecutionPlan{
		PlanKey:                "plan-1",
		StrategyMode:           service.StrategyModeRollingCycleAligned,
		Status:                 "ready",
		ReadyNow:               true,
		NetExpectedPNL:         6.5,
		ProjectedFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(),
		NextReviewTimeMs:       now.Add(time.Hour).UnixMilli(),
		SyncBoundaryTimeMs:     now.Add(2 * time.Hour).UnixMilli(),
		EntryPathSegmentCount:  2,
		EntryPathStopReason:    "sync_boundary",
		EntryWindowOpenMs:      now.Add(-10 * time.Minute).UnixMilli(),
		EntryWindowCloseMs:     now.Add(-5 * time.Minute).UnixMilli(),
		TargetCloseTimeMs:      now.Add(2*time.Hour + 15*time.Second).UnixMilli(),
	}

	got := m.renderPlanDetail(OpportunityListItem{Status: "eligible"}, plan, true, entity.ExecutionRecord{}, false, 180)
	for _, needle := range []string{
		"持有模式=最晚盈利窗口",
		"持有上限=4.0h",
		"策略模式=按结算段滚动",
		"下次 Review=",
		"共享 Boundary=",
		"计划持仓=",
		"预计 funding 兑现=",
		"目标平仓=",
		"结算后缓冲=15s",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected plan detail to contain %q, got %q", needle, got)
		}
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

func TestRenderProjectionDetail_IncludesFundingSegmentSections(t *testing.T) {
	m := NewModel(nil, 0)
	detail := &entity.Opportunity{
		ProjectionDetails: []entity.OpportunityProjection{{
			ProjectionRank:            1,
			IsBestProjection:          true,
			ProjectedFundingTimeMs:    time.Now().Add(time.Hour).UnixMilli(),
			FundingWindowHours:        1,
			LongFundingEventCount:     1,
			ShortFundingEventCount:    0,
			CarryRate:                 0.00356,
			CarryRateHourlyEquivalent: 0.00356,
			GrossFundingPNL:           5.696,
			NetExpectedPNL:            4.9,
		}},
		EntryPathSegments: []entity.OpportunityFundingSegment{{
			SegmentRank:          1,
			SettlementTimeMs:     time.Now().Add(time.Hour).UnixMilli(),
			SegmentType:          "single_real",
			OptimalLongExchange:  "aster",
			OptimalShortExchange: "binance",
			CarryRate:            0.00356,
			LongLegSettles:       true,
			DirectionMatchesHeld: true,
		}},
		FundingSegments: []entity.OpportunityFundingSegment{{
			SegmentRank:          2,
			SettlementTimeMs:     time.Now().Add(2 * time.Hour).UnixMilli(),
			SegmentType:          "shared_real",
			OptimalLongExchange:  "binance",
			OptimalShortExchange: "aster",
			CarryRate:            0.00844,
			SharedSettlement:     true,
			DirectionMatchesHeld: false,
		}},
	}

	got := m.renderProjectionDetail(OpportunityListItem{}, detail, true, false, 180)
	for _, needle := range []string{
		"Entry Path Settlement 段",
		"完整 Settlement 段",
		"说明: “反向”表示该段最优方向已经不同于当前持仓方向",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected projection detail to contain %q, got %q", needle, got)
		}
	}
}
