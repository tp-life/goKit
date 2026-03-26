package service

import "testing"

func TestConfigNormalize_DefaultsTradingModesToTaker(t *testing.T) {
	cfg := Config{}

	normalized := cfg.normalize()

	if normalized.EntryMode != "taker" {
		t.Fatalf("expected default entry mode taker, got %s", normalized.EntryMode)
	}
	if normalized.ExitMode != "taker" {
		t.Fatalf("expected default exit mode taker, got %s", normalized.ExitMode)
	}
}
