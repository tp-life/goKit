package service

import (
	"testing"

	"goKit/internal/infrastructure/exchange"
)

func TestBuildVenueProfileRegistry_MapsBybitAdapterKindToBybitVenue(t *testing.T) {
	registry := BuildVenueProfileRegistry(exchange.ConfigSet{
		Additional: map[string]exchange.ExchangeConfig{
			"bybit": {
				Enabled:     true,
				AdapterKind: exchange.AdapterKindBybitV5,
			},
		},
	})

	floor, cap, source := registry.FundingClamp("bybit", 8)
	if source != "bybit_cap" {
		t.Fatalf("expected bybit venue clamp source, got %q", source)
	}
	if floor != -0.00375 || cap != 0.00375 {
		t.Fatalf("expected bybit 8h clamp +/-0.00375, got floor=%.6f cap=%.6f", floor, cap)
	}
}
