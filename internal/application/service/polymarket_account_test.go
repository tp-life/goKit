package service

import (
	"math"
	"testing"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/polymarket"
)

func TestBuildWalletPositions(t *testing.T) {
	rows := []polymarket.DataPositionResponse{
		{
			ProxyWallet: "0xabc",
			ConditionID: "cond-1",
			Slug:        "btc-updown-1",
			Outcome:     "YES",
			Size:        polymarket.FlexibleFloat{Valid: true, Value: 12.5},
			AvgPrice:    polymarket.FlexibleFloat{Valid: true, Value: 0.42},
			CurPrice:    polymarket.FlexibleFloat{Valid: true, Value: 0.58},
			RealizedPnL: polymarket.FlexibleFloat{Valid: true, Value: 1.25},
		},
	}

	out := buildWalletPositions(rows)
	if len(out) != 1 {
		t.Fatalf("expected 1 wallet position, got %d", len(out))
	}
	if out[0].Outcome != "UP" {
		t.Fatalf("expected normalized outcome UP, got %q", out[0].Outcome)
	}
	if out[0].AvgPrice == nil || *out[0].AvgPrice != 0.42 {
		t.Fatalf("expected avg price 0.42, got %#v", out[0].AvgPrice)
	}
	if got := computeWalletUnrealizedPnL(rows); math.Abs(got-2.0) > 1e-9 {
		t.Fatalf("unexpected unrealized pnl: %.6f", got)
	}
}

func TestBuildMarketAggregatedTrades(t *testing.T) {
	rows := []polymarket.DataActivityResponse{
		{
			Type:        polymarket.FlexibleText("TRADE"),
			Side:        polymarket.FlexibleText("BUY"),
			Outcome:     "YES",
			ConditionID: polymarket.FlexibleText("cond-1"),
			EventSlug:   "btc-updown-1",
			Title:       "BTC market",
			Price:       polymarket.FlexibleFloat{Valid: true, Value: 0.40},
			Size:        polymarket.FlexibleFloat{Valid: true, Value: 10},
			USDCSize:    polymarket.FlexibleFloat{Valid: true, Value: 4},
			Timestamp:   polymarket.FlexibleText("2026-03-31T10:00:00Z"),
		},
		{
			Type:        polymarket.FlexibleText("TRADE"),
			Side:        polymarket.FlexibleText("SELL"),
			Outcome:     "YES",
			ConditionID: polymarket.FlexibleText("cond-1"),
			EventSlug:   "btc-updown-1",
			Title:       "BTC market",
			Price:       polymarket.FlexibleFloat{Valid: true, Value: 0.60},
			SizeMatched: polymarket.FlexibleFloat{Valid: true, Value: 6},
			USDCSize:    polymarket.FlexibleFloat{Valid: true, Value: 3.6},
			Timestamp:   polymarket.FlexibleText("2026-03-31T10:05:00Z"),
		},
		{
			Type:        polymarket.FlexibleText("REDEEM"),
			ConditionID: polymarket.FlexibleText("cond-1"),
			EventSlug:   "btc-updown-1",
			Title:       "BTC market",
			USDCSize:    polymarket.FlexibleFloat{Valid: true, Value: 4},
			Timestamp:   polymarket.FlexibleText("2026-03-31T10:06:00Z"),
		},
	}

	out := buildMarketAggregatedTrades(rows)
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated trade, got %d", len(out))
	}
	item := out[0]
	if item.Direction != "UP" {
		t.Fatalf("expected direction UP, got %q", item.Direction)
	}
	if item.BuyCount != 1 || item.SellCount != 1 || item.RedeemCount != 1 {
		t.Fatalf("unexpected counts: %+v", item)
	}
	if item.Size != 6 {
		t.Fatalf("expected display size 6, got %.4f", item.Size)
	}
	if math.Abs(item.Profit-3.6) > 1e-9 {
		t.Fatalf("expected profit 3.6, got %.4f", item.Profit)
	}
	if item.Result != "CLOSED" || item.Status != "AGG" {
		t.Fatalf("unexpected result/status: %+v", item)
	}
}

func TestComputeWalletRealizedPnL(t *testing.T) {
	rows := []polymarket.DataClosedPositionResponse{
		{RealizedPnL: polymarket.FlexibleFloat{Valid: true, Value: 1.2}},
		{RealizedPnLAlt: polymarket.FlexibleFloat{Valid: true, Value: -0.2}},
	}
	if got := computeWalletRealizedPnL(rows); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("expected realized pnl 1.0, got %.4f", got)
	}
}

func TestNextAutoRedeemRun(t *testing.T) {
	now := time.Date(2026, 3, 31, 2, 30, 0, 0, time.Local)
	next := nextAutoRedeemRun(now, nil, 3)
	if !next.Equal(time.Date(2026, 3, 31, 3, 0, 0, 0, time.Local)) {
		t.Fatalf("expected same-day scheduled run, got %s", next.Format(time.RFC3339))
	}

	now = time.Date(2026, 3, 31, 8, 0, 0, 0, time.Local)
	next = nextAutoRedeemRun(now, nil, 3)
	if !next.Equal(now) {
		t.Fatalf("expected immediate run after missed schedule, got %s", next.Format(time.RFC3339))
	}

	lastRun := time.Date(2026, 3, 31, 8, 5, 0, 0, time.Local)
	next = nextAutoRedeemRun(now, &lastRun, 3)
	want := time.Date(2026, 4, 1, 3, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("expected next-day scheduled run, got %s", next.Format(time.RFC3339))
	}
}

func TestAutoRedeemAccountAllowsProxyWalletWithRelayerKey(t *testing.T) {
	svc := &PolymarketService{
		cfg: polymarket.Config{
			AutoRedeem:    true,
			SignatureType: 1,
			RelayerAPIKey: "relay-key-1",
			FunderAddress: "0x1771ce21dD09805cCD43f7851f7089f0b085974d",
			PrivateKey:    "configured",
			PolygonRPCURL: "",
			RelayerURL:    "https://relayer-v2.polymarket.com",
		},
		client: &stubPolymarketSDK{
			addressHex:       "0x1111111111111111111111111111111111111111",
			funderHex:        "0x1771ce21dD09805cCD43f7851f7089f0b085974d",
			hasPrivateKey:    true,
			hasPrivateKeySet: true,
		},
		dashboard: entity.NewDashboardState(),
	}

	account, enabled, reason := svc.autoRedeemAccount()
	if !enabled {
		t.Fatalf("expected proxy wallet auto redeem to be enabled, got reason %q", reason)
	}
	if account != "0x1771ce21dD09805cCD43f7851f7089f0b085974d" {
		t.Fatalf("unexpected redeem account %q", account)
	}
	if reason != "" {
		t.Fatalf("expected empty disable reason, got %q", reason)
	}
}
