package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigLoadsDotEnvAndOverridesConfig(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	configContent := []byte("" +
		"web:\n" +
		"  port: \":8080\"\n" +
		"  execution_api_token_env: \"TOKEN_FROM_YAML\"\n")
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	envPath := filepath.Join(tempDir, ".env")
	envContent := []byte("" +
		"WEB_EXECUTION_API_TOKEN_ENV=TOKEN_FROM_DOTENV\n" +
		"FUNDING_ARBITRAGE_TEST_DOTENV_VALUE=from_dotenv\n")
	if err := os.WriteFile(envPath, envContent, 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
		os.Unsetenv("WEB_EXECUTION_API_TOKEN_ENV")
		os.Unsetenv("FUNDING_ARBITRAGE_TEST_DOTENV_VALUE")
	})
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Web.ExecutionAPITokenEnv != "TOKEN_FROM_DOTENV" {
		t.Fatalf("expected env override, got %q", cfg.Web.ExecutionAPITokenEnv)
	}
	if got := os.Getenv("FUNDING_ARBITRAGE_TEST_DOTENV_VALUE"); got != "from_dotenv" {
		t.Fatalf("expected dotenv env var to be loaded, got %q", got)
	}
}
