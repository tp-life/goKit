package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"

	"github.com/charmbracelet/lipgloss"
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

func TestFeeBreakdownText_UsesTakerFeesAfterJSONDecode(t *testing.T) {
	// 这个用例覆盖真实链路里最容易被忽略的一步：
	// system status API 会把 fees_by_exchange 以 snake_case JSON 返回给 TUI，
	// TUI 再把它解到 StrategyStatus.FeesByExchange。若 FeeConfig 缺少 json tag，
	// maker_bps / taker_bps 会静默变成 0，最终让“收益构成”里的手续费说明失真。
	raw := []byte(`{
		"strategy": {
			"fees_by_exchange": {
				"binance": {"maker_bps": 1.00, "taker_bps": 4.00},
				"aster": {"maker_bps": 1.20, "taker_bps": 4.20}
			}
		}
	}`)

	var payload struct {
		Strategy StrategyStatus `json:"strategy"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal strategy status: %v", err)
	}

	item := OpportunityListItem{
		LongExchange:  "binance",
		ShortExchange: "aster",
	}
	got := feeBreakdownText(item, payload.Strategy, "taker")
	want := "binance taker 4.00 bps + aster taker 4.20 bps"
	if got != want {
		t.Fatalf("unexpected taker fee breakdown: got %q want %q", got, want)
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

func TestRenderPanel_ClampsLongContentHeight(t *testing.T) {
	content := strings.Join([]string{
		"line-1",
		"line-2",
		"line-3",
		"line-4",
		"line-5",
		"line-6",
	}, "\n")

	got := renderPanel(60, 6, content)
	if height := lipgloss.Height(got); height != 6 {
		t.Fatalf("expected rendered panel height 6, got %d", height)
	}
	if !strings.Contains(got, "line-1") {
		t.Fatalf("expected clamped panel to keep top content, got %q", got)
	}
	if strings.Contains(got, "line-6") {
		t.Fatalf("expected clamped panel to truncate overflowing tail, got %q", got)
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

	got := m.renderConfigPanel(180, 16)
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
		"当前共享结算边界=",
		"预计平仓=",
		"预计总持有=",
		"结算后缓冲=15s",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected overview detail to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderSameExchangePlanExecutionDetail_ShowsRiskSummary(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		ArbitrageMode:                    service.ArbitrageModeSameExchangeSpotPerp,
		SameExchangeMax1hPriceShockRatio: 0.08,
	}
	plan := entity.ExecutionPlan{
		PlanKey:          "plan-risk",
		Symbol:           "BTC",
		LongExchange:     "binance_spot",
		ShortExchange:    "binance",
		LongVenueSymbol:  "BTCUSDT",
		ShortVenueSymbol: "BTCUSDT",
		NetExpectedPNL:   5.2,
	}
	rec := entity.ExecutionRecord{
		PlanKey:     "plan-risk",
		Status:      "opened",
		LiveTrading: true,
		AutoClose:   true,
		OpenedAtMs:  time.Now().Add(-time.Hour).UnixMilli(),
		ClosedAtMs:  0,
	}
	m.data.LivePositions.Candidates = []service.LivePositionCandidate{{
		Execution: rec,
		Plan:      &plan,
		Risk: service.SameExchangeLiveRiskInspection{
			Enabled:                     true,
			PriceShockAllowed:           false,
			PriceShockReason:            "max_1h_price_shock",
			PriceShockRatio:             0.12,
			PriceShockThresholdRatio:    0.08,
			PriceShockCurrentMarkPrice:  112,
			PriceShockBaselineMarkPrice: 100,
			LiquidationPrice:            121,
			LiquidationDistanceRatio:    0.10,
			ReduceLiqDistanceRatio:      0.10,
			EmergencyLiqDistanceRatio:   0.08,
			ProtectiveOrderArmed:        true,
			ProtectiveOrderStatus:       "NEW",
			ProtectiveOrderStopPrice:    118,
		},
	}}

	got := m.renderSameExchangePlanExecutionDetail(plan, rec, true, 180)
	for _, needle := range []string{"风险视角", "1h 价格冲击", "爆仓距离", "已挂保护单"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected same-exchange execution detail to contain %q, got %q", needle, got)
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
		"当前 Carry率=0.61409%",
		"当前时均边际=1.33977%",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected overview detail to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderOverviewDetail_RollingPrefersCurrentRealCarry(t *testing.T) {
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
		Symbol:                 "DOOD",
		LongExchange:           "aster",
		ShortExchange:          "binance",
		NetExpectedPNL:         3.1,
		NetExpectedBps:         10,
		LongFundingRate:        -0.00062908,
		ShortFundingRate:       -0.01031913,
		LongFundingTimeMs:      time.Now().Add(time.Hour).UnixMilli(),
		ShortFundingTimeMs:     time.Now().Add(4 * time.Hour).UnixMilli(),
		GrossEdgeHourly:        0.00212,
		ProjectedFundingTimeMs: time.Now().Add(3 * time.Hour).UnixMilli(),
	}
	detail := &entity.Opportunity{
		StrategyMode:       service.StrategyModeRollingCycleAligned,
		LongFundingRate:    -0.00062908,
		ShortFundingRate:   -0.01031913,
		LongFundingTimeMs:  time.Now().Add(time.Hour).UnixMilli(),
		ShortFundingTimeMs: time.Now().Add(4 * time.Hour).UnixMilli(),
		ProjectionDetails: []entity.OpportunityProjection{
			{
				IsBestProjection:          true,
				CarryRate:                 0.0047769,
				CarryRateHourlyEquivalent: 0.0021222,
			},
		},
	}

	got := m.renderOverviewDetail(item, detail, true, false, entity.ExecutionPlan{}, false, entity.ExecutionRecord{}, false, 180)
	if !strings.Contains(got, "当前 Carry率=0.06291%") {
		t.Fatalf("expected rolling overview to explain current real carry, got %q", got)
	}
	if strings.Contains(got, "当前 Carry率=0.47769%") {
		t.Fatalf("expected rolling overview not to headline forecast carry, got %q", got)
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
		"当前共享结算边界=",
		"计划持仓=",
		"当前 Entry Path 终点=",
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

	got := NewModel(nil, 0).renderLegsDetail(item, nil, false, false, 260)
	for _, needle := range []string{"指标", "做多腿", "做空腿", "交易所", "当前 next", "下一事件预", "标记价"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected legs detail table to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderOpportunityList_UsesCarryRateInsteadOfHourlyEdge(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		EffectiveNotional: 1600,
	}
	m.data.Opportunities = []OpportunityListItem{
		{
			Symbol:                 "KAT",
			LongExchange:           "aster",
			ShortExchange:          "binance",
			LongVenueSymbol:        "KATUSDT",
			ShortVenueSymbol:       "KATUSDT",
			NetExpectedPNL:         6.6,
			GrossFundingPNL:        8.0,
			GrossEdgeHourly:        0.0133977,
			BasisBps:               1.9,
			ProjectedFundingTimeMs: time.Now().Add(time.Hour).UnixMilli(),
			Status:                 "eligible",
		},
	}
	m.normalizeSelections()

	got := m.renderOpportunityList(180, 18)
	if !strings.Contains(got, "carry") {
		t.Fatalf("expected opportunity list to show carry label, got %q", got)
	}
	if !strings.Contains(got, "0.50000%") {
		t.Fatalf("expected opportunity list to show carry derived from gross funding pnl, got %q", got)
	}
}

func TestRenderOpportunityList_SameExchangeUsesModeSpecificLabels(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		ArbitrageMode:     service.ArbitrageModeSameExchangeSpotPerp,
		EffectiveNotional: 1600,
	}
	m.data.Opportunities = []OpportunityListItem{
		{
			Symbol:                                "BTC",
			BatchID:                               "batch-1",
			LongExchange:                          "bspot",
			ShortExchange:                         "bperp",
			LongVenueSymbol:                       "BTCUSDT",
			ShortVenueSymbol:                      "BTCUSDT",
			NetExpectedPNL:                        6.6,
			GrossFundingPNL:                       8.0,
			ShortFundingRate:                      0.01,
			BasisBps:                              1.2,
			SameExchangeBasisUsesPaybackModel:     true,
			SameExchangeBasisCostBps:              18.4,
			SameExchangeBasisCarryPerEventBps:     10.0,
			SameExchangeBasisPaybackFundingEvents: 1.84,
			SameExchangeBasisAllowed:              true,
			SameExchangeBasisReason:               "extreme_payback",
			SameExchangeBasisRiskSizeMultiplier:   0.65,
			ProjectedFundingTimeMs:                time.Now().Add(time.Hour).UnixMilli(),
			Status:                                "eligible",
		},
	}
	m.data.AllPlans = []entity.ExecutionPlan{
		{
			PlanKey:                               "plan-same-list",
			OpportunityBatchID:                    "batch-1",
			Symbol:                                "BTC",
			LongExchange:                          "bspot",
			ShortExchange:                         "bperp",
			LongVenueSymbol:                       "BTCUSDT",
			ShortVenueSymbol:                      "BTCUSDT",
			PerpFundingRank:                       2,
			PerpFundingRankTotal:                  50,
			PerpFundingHistorySampleCount:         20,
			PerpFundingHistoryPositiveRatio:       0.40,
			PerpFundingHistoryNegativeRatio:       0.60,
			PerpFundingEstimatedAnnualizedNetRate: 0.42,
			SameExchangeLongHoldEligible:          false,
			SameExchangeLongHoldReason:            "support_ratio_low",
		},
	}
	m.normalizeSortMode()
	m.normalizeSelections()

	got := m.renderOpportunityList(180, 18)
	for _, needle := range []string{
		"现货对冲机会",
		"sorted by 永续费率",
		"bspot 现货多 / bperp 永续空",
		"单轮毛收 +16.000 USDT",
		"主窗毛收 +8.000 USDT",
		"回本 1.84 轮",
		"极端基差，降仓至 65%",
		"净收 +6.600 USDT",
		"粗年化净 42.00%",
		"历史+ 40.0% / 20",
		"历史- 60.0% / 20",
		"长持 不建议长期持有",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected same-exchange opportunity list to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderSameExchangeOpportunityList_FitsRequestedHeight(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		ArbitrageMode:     service.ArbitrageModeSameExchangeSpotPerp,
		EffectiveNotional: 1600,
	}
	now := time.Now()
	m.data.Opportunities = []OpportunityListItem{
		{Symbol: "BTC", LongExchange: "bspot", ShortExchange: "bperp", LongVenueSymbol: "BTCUSDT", ShortVenueSymbol: "BTCUSDT", ShortFundingRate: 0.01, GrossFundingPNL: 8, NetExpectedPNL: 6.6, ProjectedFundingTimeMs: now.Add(time.Hour).UnixMilli()},
		{Symbol: "ETH", LongExchange: "bspot", ShortExchange: "bperp", LongVenueSymbol: "ETHUSDT", ShortVenueSymbol: "ETHUSDT", ShortFundingRate: 0.009, GrossFundingPNL: 7, NetExpectedPNL: 5.4, ProjectedFundingTimeMs: now.Add(time.Hour).UnixMilli()},
		{Symbol: "SOL", LongExchange: "bspot", ShortExchange: "bperp", LongVenueSymbol: "SOLUSDT", ShortVenueSymbol: "SOLUSDT", ShortFundingRate: 0.008, GrossFundingPNL: 6, NetExpectedPNL: 4.3, ProjectedFundingTimeMs: now.Add(time.Hour).UnixMilli()},
	}
	m.normalizeSortMode()
	m.normalizeSelections()

	got := m.renderSameExchangeOpportunityList(120, 12)
	if height := lipgloss.Height(got); height != 12 {
		t.Fatalf("expected same-exchange list height 12, got %d", height)
	}
	if !strings.Contains(got, "现货对冲机会") {
		t.Fatalf("expected same-exchange list title, got %q", got)
	}
}

func TestFilteredOpportunities_SameExchangeDefaultsToFundingOrder(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		ArbitrageMode: service.ArbitrageModeSameExchangeSpotPerp,
	}
	m.data.Opportunities = []OpportunityListItem{
		{
			Symbol:           "LOW",
			LongExchange:     "bspot",
			ShortExchange:    "bperp",
			LongVenueSymbol:  "LOWUSDT",
			ShortVenueSymbol: "LOWUSDT",
			ShortFundingRate: 0.002,
			NetExpectedPNL:   10,
		},
		{
			Symbol:           "HIGH",
			LongExchange:     "bspot",
			ShortExchange:    "bperp",
			LongVenueSymbol:  "HIGHUSDT",
			ShortVenueSymbol: "HIGHUSDT",
			ShortFundingRate: 0.010,
			NetExpectedPNL:   3,
		},
	}

	m.normalizeSortMode()
	items := m.filteredOpportunities()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Symbol != "HIGH" {
		t.Fatalf("expected higher funding rate symbol first, got %q then %q", items[0].Symbol, items[1].Symbol)
	}
}

func TestRenderPlanDetail_SameExchangeShowsFundingContext(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		ArbitrageMode:                         service.ArbitrageModeSameExchangeSpotPerp,
		HoldHours:                             4,
		HoldSelectionMode:                     "dynamic_profit",
		SameExchangeRequireLongHoldEligible:   true,
		SameExchangeMinHistorySampleCount:     10,
		SameExchangeMinHistoricalSupportRatio: 0.55,
		SameExchangeMinAnnualizedNetRate:      0.15,
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "15s",
	}

	now := time.Now()
	plan := entity.ExecutionPlan{
		PlanKey:                                 "plan-same",
		ArbitrageMode:                           service.ArbitrageModeSameExchangeSpotPerp,
		Status:                                  "ready",
		ReadyNow:                                true,
		NetExpectedPNL:                          5.8,
		LongExchange:                            "bspot",
		ShortExchange:                           "bperp",
		LongVenueSymbol:                         "BTCUSDT",
		ShortVenueSymbol:                        "BTCUSDT",
		PerpFundingRank:                         2,
		PerpFundingRankTotal:                    50,
		PerpFundingHistorySampleCount:           20,
		PerpFundingHistoryMeanRate:              0.0015,
		PerpFundingHistoryPositiveRatio:         0.40,
		PerpFundingHistoryNegativeRatio:         0.60,
		PerpFundingHistoricalSupportRatio:       0.40,
		PerpFundingCurrentHistoricalPercentile:  0.90,
		PerpFundingEstimatedAnnualizedCarryRate: 0.50,
		PerpFundingEstimatedAnnualizedNetRate:   0.42,
		SameExchangeLongHoldEligible:            false,
		SameExchangeLongHoldReason:              "support_ratio_low",
		ProjectedFundingTimeMs:                  now.Add(2 * time.Hour).UnixMilli(),
		EntryWindowOpenMs:                       now.Add(-10 * time.Minute).UnixMilli(),
		EntryWindowCloseMs:                      now.Add(-5 * time.Minute).UnixMilli(),
		TargetCloseTimeMs:                       now.Add(2*time.Hour + 15*time.Second).UnixMilli(),
	}

	got := m.renderPlanDetail(OpportunityListItem{Status: "eligible"}, plan, true, entity.ExecutionRecord{}, false, 180)
	for _, needle := range []string{
		"模式=同所现货对冲",
		"永续横向排名=#2/50",
		"历史正费率=40.0% / 20",
		"历史负费率=60.0% / 20",
		"历史分位=90.0%",
		"粗年化净收益=42.00%",
		"长持判定=不建议开仓",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected same-exchange plan detail to contain %q, got %q", needle, got)
		}
	}
}

func TestRenderOverviewDetail_SameExchangeUsesDedicatedFundingContext(t *testing.T) {
	m := NewModel(nil, 0)
	m.data.System.Strategy = StrategyStatus{
		ArbitrageMode:                            service.ArbitrageModeSameExchangeSpotPerp,
		HoldSelectionMode:                        "dynamic_profit",
		SameExchangeRequireLongHoldEligible:      true,
		SameExchangeMinHistorySampleCount:        10,
		SameExchangeMinHistoricalSupportRatio:    0.55,
		SameExchangeMinAnnualizedNetRate:         0.15,
		SameExchangeCloseOnNegativeFunding:       true,
		SameExchangeHistoryNegativeExitThreshold: 0.5,
		SameExchangeExitRequirePositiveClosePNL:  true,
		SameExchangeExitMinClosePNL:              0.5,
	}
	m.data.System.Execution = ExecutionStatus{
		CloseGracePeriod: "15s",
	}

	now := time.Now()
	item := OpportunityListItem{
		Symbol:                 "BTC",
		BatchID:                "batch-1",
		LongExchange:           "bspot",
		ShortExchange:          "bperp",
		LongVenueSymbol:        "BTCUSDT",
		ShortVenueSymbol:       "BTCUSDT",
		NetExpectedPNL:         6.6,
		NetExpectedBps:         12,
		BasisBps:               1.2,
		ShortFundingRate:       0.01,
		ProjectedFundingTimeMs: now.Add(2 * time.Hour).UnixMilli(),
		ShortFundingTimeMs:     now.Add(time.Hour).UnixMilli(),
		Status:                 "eligible",
	}
	detail := &entity.Opportunity{
		ShortFundingRate: 0.01,
		GrossFundingPNL:  8.0,
		LongFundingRule: entity.OpportunityFundingRule{
			Exchange:          "bspot",
			VenueSymbol:       "BTCUSDT",
			ClampSource:       "spot_synthetic_zero",
			NextFundingTimeMs: now.Add(time.Hour).UnixMilli(),
		},
		ShortFundingRule: entity.OpportunityFundingRule{
			Exchange:                     "bperp",
			VenueSymbol:                  "BTCUSDT",
			CurrentFundingRank:           2,
			CurrentFundingRankTotal:      50,
			HistorySampleCount:           20,
			HistoryMeanRate:              0.0015,
			HistoryPositiveRatio:         0.40,
			HistoryNegativeRatio:         0.60,
			HistoricalSupportRatio:       0.40,
			CurrentHistoricalPercentile:  0.90,
			EstimatedAnnualizedCarryRate: 0.50,
			EstimatedAnnualizedNetRate:   0.42,
			LongHoldEligible:             false,
			LongHoldReason:               "support_ratio_low",
			HistorySeries: []entity.OpportunityFundingHistoryPoint{
				{FundingTimeMs: now.Add(-8 * time.Hour).UnixMilli(), FundingRate: 0.0018, MarkPrice: 62500},
				{FundingTimeMs: now.Add(-16 * time.Hour).UnixMilli(), FundingRate: -0.0005, MarkPrice: 61800},
			},
		},
	}

	got := m.renderOverviewDetail(item, detail, true, false, entity.ExecutionPlan{}, false, entity.ExecutionRecord{}, false, 180)
	for _, needle := range []string{
		"同所现货多 / 永续空",
		"选标视角",
		"单轮 funding 毛收益=+8.000 USDT",
		"主窗口 funding 收益=+8.000 USDT",
		"执行视角",
		"历史资金费率画像",
		"历史样本=20 个周期",
		"历史正费率占比=40.0% / 20",
		"历史负费率占比=60.0% / 20",
		"历史资金费率明细",
		"现货腿最近 funding",
		"永续腿最近 funding",
		"0.18000%",
		"-0.05000%",
		"长期持有评估",
		"历史均值 funding=0.15000%",
		"同向历史支持=40.0% / 20",
		"粗略年化净收益=42.00%",
		"长持判定=不建议开仓",
		"判定原因=同向历史支持 < 55.0%",
		"持有逻辑=收益驱动动态持有",
		"离场守则=永续 funding 转负即复核 / 历史负费率>=50.0% / 平仓净收益>=",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected same-exchange overview detail to contain %q, got %q", needle, got)
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
		"当前 Entry Path 段",
		"当前 Boundary 内全部结算段",
		"说明: “反向”表示该段最优方向已经不同于当前持仓方向",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected projection detail to contain %q, got %q", needle, got)
		}
	}
}
