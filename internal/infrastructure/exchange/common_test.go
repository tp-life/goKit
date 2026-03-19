package exchange

import "testing"

func TestCanonicalFrom_UsesAliasForBaseAsset(t *testing.T) {
	got := canonicalFrom("XBTUSDT", "XBT")
	if got != "BTC" {
		t.Fatalf("expected XBT base asset to normalize to BTC, got %s", got)
	}
}

func TestCanonicalFrom_FallsBackToRawAndNormalizesAlias(t *testing.T) {
	got := canonicalFrom("XBTUSD", "")
	if got != "BTC" {
		t.Fatalf("expected XBTUSD raw symbol to normalize to BTC, got %s", got)
	}
}

func TestNormalizeAllowed_MatchesCanonicalAlias(t *testing.T) {
	allowed := map[string]struct{}{
		"BTC": {},
	}
	if !normalizeAllowed(allowed, "XBTUSD", "XBT") {
		t.Fatalf("expected allowlist BTC to match XBT alias")
	}
}

func TestNormalizeAllowed_StillMatchesRawSymbolWhenConfigured(t *testing.T) {
	allowed := map[string]struct{}{
		"XBT": {},
	}
	if !normalizeAllowed(allowed, "XBTUSD", "XBT") {
		t.Fatalf("expected allowlist XBT to keep matching raw alias symbol")
	}
}
