package exchange

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReadCredentialPairPrefersCompleteDotEnvPairOverPartialProcessEnv(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "shell-key")
	t.Setenv("BINANCE_API_SECRET", "")

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("BINANCE_API_KEY=file-key\nBINANCE_API_SECRET=file-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	t.Chdir(tmpDir)
	resetDotEnvValuesForTest()

	key, secret := readCredentialPair("BINANCE_API_KEY", "BINANCE_API_SECRET")
	if key != "file-key" || secret != "file-secret" {
		t.Fatalf("unexpected credential pair key=%q secret=%q", key, secret)
	}
}

func TestReadCredentialPairPrefersCompleteProcessEnvPair(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "shell-key")
	t.Setenv("BINANCE_API_SECRET", "shell-secret")

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("BINANCE_API_KEY=file-key\nBINANCE_API_SECRET=file-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	t.Chdir(tmpDir)
	resetDotEnvValuesForTest()

	key, secret := readCredentialPair("BINANCE_API_KEY", "BINANCE_API_SECRET")
	if key != "shell-key" || secret != "shell-secret" {
		t.Fatalf("unexpected credential pair key=%q secret=%q", key, secret)
	}
}

func resetDotEnvValuesForTest() {
	dotEnvValuesOnce = sync.Once{}
	dotEnvValues = nil
}
