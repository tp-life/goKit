package tui

import (
	"strings"
	"testing"
	"time"

	"goKit/internal/domain/entity"
)

// TestBuildTradeFormReq 确认交易表单可以稳定组装出手动下单请求。
func TestBuildTradeFormReq(t *testing.T) {
	m := model{
		trade: tradeForm{
			action:           "BUY",
			outcome:          "UP",
			amountInput:      "12.50",
			probabilityInput: "0.6234",
		},
	}

	req, label, err := m.buildTradeFormReq()
	if err != nil {
		t.Fatalf("expected trade form request to be valid, got %v", err)
	}
	if req.Action != "BUY" || req.Outcome != "UP" {
		t.Fatalf("unexpected req side: %+v", req)
	}
	if req.Amount != 12.50 || req.Probability != 0.6234 {
		t.Fatalf("unexpected req payload: %+v", req)
	}
	if label == "" {
		t.Fatalf("expected non-empty confirmation label")
	}
}

// TestBuildTradeFormReqRejectsInvalidProbability 确认表单会拦截越界概率。
func TestBuildTradeFormReqRejectsInvalidProbability(t *testing.T) {
	m := model{
		trade: tradeForm{
			action:           "BUY",
			outcome:          "DOWN",
			amountInput:      "5",
			probabilityInput: "1.20",
		},
	}

	if _, _, err := m.buildTradeFormReq(); err == nil {
		t.Fatalf("expected invalid probability to be rejected")
	}
}

// TestRenderAutoRedeemIncludesDisableReason 确认关闭状态会把具体原因展示出来。
func TestRenderAutoRedeemIncludesDisableReason(t *testing.T) {
	text := renderAutoRedeem(entity.AutoRedeemStatus{
		Enabled:   false,
		LastError: "当前 Go 版兑奖仅支持签名地址与 FUNDER_ADDRESS 一致",
	})
	if !strings.Contains(text, "已关闭") {
		t.Fatalf("expected disabled marker, got %q", text)
	}
	if !strings.Contains(text, "FUNDE") {
		t.Fatalf("expected disable reason to be rendered, got %q", text)
	}
}

// TestRenderLastOrderIncludesError 确认最近下单摘要会带出最后一次失败原因。
func TestRenderLastOrderIncludesError(t *testing.T) {
	text := renderLastOrder(&entity.LastOrder{
		Key:        "btc-updown|UP",
		RetryCount: 2,
		LastPrice:  0.81,
		Time:       "2026-03-31T18:00:00Z",
		Error:      "order rejected by clob",
	})
	if !strings.Contains(text, "err=") {
		t.Fatalf("expected last order error to be rendered, got %q", text)
	}
	if !strings.Contains(text, "order rejected by clob") {
		t.Fatalf("expected concrete error message to be rendered, got %q", text)
	}
}

// TestRenderPositionBalanceCheckShowsWalletShortfall 确认 TUI 会直接展示钱包可卖仓位不足。
func TestRenderPositionBalanceCheckShowsWalletShortfall(t *testing.T) {
	m := model{
		state: entity.DashboardState{
			Position: &entity.Position{
				Slug: "btc-up-or-down-mar-31-0800",
				Side: "UP",
				Size: 7.93,
			},
			WalletPositions: []entity.WalletPosition{
				{
					Slug:    "btc-up-or-down-mar-31-0800",
					Outcome: "UP",
					Size:    7.71875,
				},
			},
		},
	}

	text := m.renderPositionBalanceCheck()
	if !strings.Contains(text, "本地=7.9300") {
		t.Fatalf("expected local size to be rendered, got %q", text)
	}
	if !strings.Contains(text, "钱包=7.7188") {
		t.Fatalf("expected wallet size to be rendered, got %q", text)
	}
	if !strings.Contains(text, "差值=-0.2112") {
		t.Fatalf("expected diff to be rendered, got %q", text)
	}
}

// TestWalletPositionSizeForPositionUsesOutcomeFallback 确认钱包侧只返回 outcome 时仍可正确匹配。
func TestWalletPositionSizeForPositionUsesOutcomeFallback(t *testing.T) {
	position := &entity.Position{
		Slug: "eth-up-or-down-mar-31-0815",
		Side: "DOWN",
	}

	size, matched := walletPositionSizeForPosition(position, []entity.WalletPosition{
		{
			Slug:    "eth-up-or-down-mar-31-0815",
			Outcome: "DOWN",
			Size:    3.25,
		},
	})

	if !matched {
		t.Fatalf("expected wallet position to match local side")
	}
	if size != 3.25 {
		t.Fatalf("expected matched size 3.25, got %.4f", size)
	}
}

// TestRenderAutoTradeDiagnosticsLines 确认自动交易诊断面板会输出关键统计项。
func TestRenderAutoTradeDiagnosticsLines(t *testing.T) {
	m := model{
		state: entity.DashboardState{
			Market: entity.DashboardMarket{
				Slug: "btc-updown-15m",
			},
			AutoTrade: entity.AutoTradeDiagnostics{
				MarketSlug:           "btc-updown-15m",
				SampleCount:          120,
				TriggerCount:         6,
				DiffMissCount:        40,
				ProbabilityMissCount: 18,
				DataLagCount:         3,
				LastReason:           "当前价差 22.15 未达到配置阈值",
				LastReasonAt:         "2026-03-31T18:10:00Z",
				LastTriggerAt:        "2026-03-31T18:09:30Z",
			},
		},
	}

	lines := m.renderAutoTradeDiagnosticsLines()
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "样本=120") {
		t.Fatalf("expected sample count to be rendered, got %q", text)
	}
	if !strings.Contains(text, "命中=6") {
		t.Fatalf("expected trigger count to be rendered, got %q", text)
	}
	if !strings.Contains(text, "价差不足=40") {
		t.Fatalf("expected diff miss count to be rendered, got %q", text)
	}
	if !strings.Contains(text, "最近原因: 当前价差 22.15 未达到配置阈值") {
		t.Fatalf("expected last reason to be rendered, got %q", text)
	}
}

// TestRecordSnapshotTracksFlashesForNonSelectedMarket 确认非焦点市场也会记录独立价格高亮。
func TestRecordSnapshotTracksFlashesForNonSelectedMarket(t *testing.T) {
	prevUp := 0.61
	nextUp := 0.63
	prevBinance := 3200.0
	nextBinance := 3210.0
	prevDiff := 12.5
	nextDiff := 14.5

	m := model{
		state: entity.DashboardState{
			SelectedMarketKey: "btc-15m",
			Markets: []entity.TrackedMarketView{
				{
					Key:   "btc-15m",
					Label: "BTC 15m",
					Prices: entity.DashboardPrices{
						UpPrice:      floatPtr(0.82),
						BinanceBTC:   floatPtr(82000),
						ChainlinkBTC: floatPtr(82005),
					},
				},
				{
					Key:   "eth-15m",
					Label: "ETH 15m",
					Prices: entity.DashboardPrices{
						UpPrice:    &prevUp,
						BinanceBTC: &prevBinance,
						Diff:       &prevDiff,
					},
				},
			},
		},
		flashes: map[string]flashMarker{},
	}

	next := m.state
	next.Markets = []entity.TrackedMarketView{
		m.state.Markets[0],
		{
			Key:   "eth-15m",
			Label: "ETH 15m",
			Prices: entity.DashboardPrices{
				UpPrice:    &nextUp,
				BinanceBTC: &nextBinance,
				Diff:       &nextDiff,
			},
		},
	}

	m.recordSnapshot(next)

	if marker, ok := m.flashes[trackedFlashKey("eth-15m", "up")]; !ok || marker.direction != 1 {
		t.Fatalf("expected eth up flash to be recorded, got %+v exists=%v", marker, ok)
	}
	if marker, ok := m.flashes[trackedFlashKey("eth-15m", "binance")]; !ok || marker.direction != 1 {
		t.Fatalf("expected eth binance flash to be recorded, got %+v exists=%v", marker, ok)
	}
	if marker, ok := m.flashes[trackedFlashKey("eth-15m", "diff")]; !ok || marker.direction != 1 {
		t.Fatalf("expected eth diff flash to be recorded, got %+v exists=%v", marker, ok)
	}
}

// TestRenderTrackedMarketMetricUsesPerMarketFlash 确认 header 和市场列表里的非焦点市场也会按自身涨跌高亮。
func TestRenderTrackedMarketMetricUsesPerMarketFlash(t *testing.T) {
	value := 0.63
	diff := 14.5
	now := time.Now()
	item := entity.TrackedMarketView{
		Key:   "eth-15m",
		Label: "ETH 15m",
		Prices: entity.DashboardPrices{
			UpPrice: &value,
			Diff:    &diff,
		},
	}

	m := model{
		state: entity.DashboardState{
			SelectedMarketKey: "btc-15m",
			Markets:           []entity.TrackedMarketView{item},
		},
		flashes: map[string]flashMarker{
			trackedFlashKey("eth-15m", "up"):   {direction: 1, changedAt: now},
			trackedFlashKey("eth-15m", "diff"): {direction: -1, changedAt: now},
		},
	}

	upText := m.renderTrackedMarketMetric(item, "up", item.Prices.UpPrice)
	diffText := m.renderTrackedMarketDiff(item)
	lines := strings.Join(m.renderTrackedMarketsLines(), "\n")

	if !strings.Contains(upText, "0.6300 +") {
		t.Fatalf("expected per-market up highlight, got %q", upText)
	}
	if !strings.Contains(diffText, "+14.5000 -") {
		t.Fatalf("expected per-market diff highlight, got %q", diffText)
	}
	if !strings.Contains(lines, "0.6300 +") {
		t.Fatalf("expected market list to reuse highlighted up price, got %q", lines)
	}
	if !strings.Contains(lines, "+14.5000 -") {
		t.Fatalf("expected market list to reuse highlighted diff, got %q", lines)
	}
}

func floatPtr(v float64) *float64 {
	return &v
}
