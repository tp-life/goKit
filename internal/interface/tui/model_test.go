package tui

import (
	"strings"
	"testing"

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
