package polymarket

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestBuildHMACSignatureFixtures(t *testing.T) {
	signature, err := buildHMACSignature(
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		1000000,
		"test-sign",
		"/orders",
		`{"hash": "0x123"}`,
	)
	if err != nil {
		t.Fatalf("buildHMACSignature returned error: %v", err)
	}
	if signature != "ZwAdJKvoYRlEKDkNMwd5BuwNNtg93kNaR_oU2HrfVvc=" {
		t.Fatalf("unexpected signature: %s", signature)
	}
}

func TestBuildHMACSignatureBase64URLCompatibility(t *testing.T) {
	base64Sig, err := buildHMACSignature(
		"++/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		1000000,
		"test-sign",
		"/orders",
		`{"hash": "0x123"}`,
	)
	if err != nil {
		t.Fatalf("buildHMACSignature returned error: %v", err)
	}

	base64URLSig, err := buildHMACSignature(
		"--_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		1000000,
		"test-sign",
		"/orders",
		`{"hash": "0x123"}`,
	)
	if err != nil {
		t.Fatalf("buildHMACSignature returned error: %v", err)
	}

	if base64Sig != base64URLSig {
		t.Fatalf("expected matching signatures, got %s and %s", base64Sig, base64URLSig)
	}
}

func TestBuildHMACSignatureIgnoresInvalidSymbols(t *testing.T) {
	signature, err := buildHMACSignature(
		"AAAAAAAAA^^AAAAAAAA<>AAAAA||AAAAAAAAAAAAAAAAAAAAA=",
		1000000,
		"test-sign",
		"/orders",
		`{"hash": "0x123"}`,
	)
	if err != nil {
		t.Fatalf("buildHMACSignature returned error: %v", err)
	}
	if signature != "ZwAdJKvoYRlEKDkNMwd5BuwNNtg93kNaR_oU2HrfVvc=" {
		t.Fatalf("unexpected signature: %s", signature)
	}
}

func TestCreateOrDeriveAPIKeyPrefersConfiguredCreds(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := NewClient(Config{
		PrivateKey:    "4c0883a69102937d6231471b5dbb6204fe512961708279273d4f4f2b2d0f7e5f",
		APIKey:        "api-key-1",
		APISecret:     "api-secret-1",
		APIPassphrase: "passphrase-1",
	}, logger)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	creds, err := client.CreateOrDeriveAPIKey(context.Background(), 0)
	if err != nil {
		t.Fatalf("CreateOrDeriveAPIKey returned error: %v", err)
	}
	if creds.Key != "api-key-1" || creds.Secret != "api-secret-1" || creds.Passphrase != "passphrase-1" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}
