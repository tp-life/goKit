package polymarket

import (
	"encoding/json"
	"math"
	"testing"
)

// TestExtractMarketUpdatesUsesNestedAssetID 确认 price_change 事件会读取内部 change 的 asset_id。
func TestExtractMarketUpdatesUsesNestedAssetID(t *testing.T) {
	payload := []map[string]any{
		{
			"event_type": "price_change",
			"price_changes": []map[string]any{
				{
					"asset_id": "token-up-1",
					"best_bid": 0.95,
					"best_ask": 0.98,
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	updates := extractMarketUpdates(raw)
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].AssetID != "token-up-1" {
		t.Fatalf("expected nested asset id to be used, got %q", updates[0].AssetID)
	}
	if updates[0].Bid == nil || updates[0].Ask == nil {
		t.Fatalf("expected best bid and ask to be populated, got %+v", updates[0])
	}
}

// TestMarketQuoteStateFallsBackToLastTradeOnWideSpread 确认宽价差不会再把 0.968 硬算成 0.45。
func TestMarketQuoteStateFallsBackToLastTradeOnWideSpread(t *testing.T) {
	state := marketQuoteState{}

	_, _, _, emit := state.apply(marketUpdate{
		AssetID:   "token-up-1",
		LastTrade: floatPtr(0.968),
	})
	if !emit {
		t.Fatalf("expected last trade update to seed display price")
	}

	_, _, display, emit := state.apply(marketUpdate{
		AssetID: "token-up-1",
		Bid:     floatPtr(0.001),
		Ask:     floatPtr(0.899),
	})
	if !emit {
		t.Fatalf("expected wide-spread quote to still emit a display price")
	}
	if math.Abs(display-0.968) > 1e-9 {
		t.Fatalf("expected display price to stay at last trade 0.968, got %.6f", display)
	}
}

// TestMarketQuoteStateUsesMidpointOnTightSpread 确认正常盘口仍然继续显示 midpoint。
func TestMarketQuoteStateUsesMidpointOnTightSpread(t *testing.T) {
	state := marketQuoteState{}

	_, _, display, emit := state.apply(marketUpdate{
		AssetID: "token-up-1",
		Bid:     floatPtr(0.95),
		Ask:     floatPtr(0.98),
	})
	if !emit {
		t.Fatalf("expected tight-spread quote to emit display price")
	}
	if math.Abs(display-0.965) > 1e-9 {
		t.Fatalf("expected midpoint 0.965, got %.6f", display)
	}
}

func floatPtr(v float64) *float64 {
	value := v
	return &value
}
