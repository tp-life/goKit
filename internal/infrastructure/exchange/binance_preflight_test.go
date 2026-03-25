package exchange

import (
	"strings"
	"testing"
)

func TestClassifyBinancePreflightSpotSignatureMismatch(t *testing.T) {
	code := int64(-1022)
	result := BinancePreflightResult{
		Exchange:      "binance",
		AuthMode:      binanceLikeTradeAuthLegacyHMAC,
		KeyPresent:    true,
		SecretPresent: true,
		SpotAccount: BinanceSignedCheck{
			Name:      "spot_account",
			OK:        false,
			ErrorCode: &code,
		},
	}

	cause, hints := classifyBinancePreflight(result)
	if cause == "" {
		t.Fatal("expected non-empty cause")
	}
	if len(hints) == 0 {
		t.Fatal("expected at least one hint")
	}
}

func TestClassifyBinancePreflightSpotRSASignatureMismatch(t *testing.T) {
	code := int64(-1022)
	result := BinancePreflightResult{
		Exchange:          "binance",
		AuthMode:          binanceLikeTradeAuthRSA,
		KeyPresent:        true,
		PrivateKeyPresent: true,
		SpotAccount: BinanceSignedCheck{
			Name:      "spot_account",
			OK:        false,
			ErrorCode: &code,
		},
	}

	cause, hints := classifyBinancePreflight(result)
	if !strings.Contains(cause, "RSA") {
		t.Fatalf("expected RSA-specific cause, got %q", cause)
	}
	if len(hints) == 0 {
		t.Fatal("expected at least one hint")
	}
}
