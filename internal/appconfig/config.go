package appconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"goKit/internal/application/service"
	"goKit/internal/infrastructure/exchange"
	"goKit/pkg/kit/db"
	appLog "goKit/pkg/kit/log"
	"goKit/pkg/kit/rpc"
	"goKit/pkg/kit/web"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

type AppConfig struct {
	Web       web.Config         `mapstructure:"web"`
	RPC       rpc.Config         `mapstructure:"rpc"`
	Database  db.Config          `mapstructure:"database"`
	Log       appLog.Config      `mapstructure:"log"`
	Strategy  service.Config     `mapstructure:"strategy"`
	Exchanges exchange.ConfigSet `mapstructure:"exchanges"`
}

func loadDotEnvFiles(paths ...string) error {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := gotenv.Load(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("load env file %s: %w", path, err)
		}
	}
	return nil
}

func Load() (*AppConfig, error) {
	viper.Reset()
	originalEnv := snapshotEnv()
	dotEnvValues, _ := gotenv.Read(".env")
	if err := loadDotEnvFiles(".env"); err != nil {
		return nil, err
	}
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	var cfg AppConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	harmonizeCredentialEnvPairs(&cfg, originalEnv, dotEnvValues)
	return &cfg, nil
}

func snapshotEnv() map[string]string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func harmonizeCredentialEnvPairs(cfg *AppConfig, originalEnv map[string]string, dotEnvValues gotenv.Env) {
	for _, exchangeCfg := range cfg.Exchanges.Items() {
		harmonizeEnvPair(exchangeCfg.Auth.APIKeyEnv, exchangeCfg.Auth.APISecretEnv, originalEnv, dotEnvValues)
		harmonizeEnvPair(exchangeCfg.Auth.AccountAddressEnv, exchangeCfg.Auth.PrivateKeyEnv, originalEnv, dotEnvValues)
		harmonizeEnvPair(exchangeCfg.Auth.APIKeyEnv, exchangeCfg.Auth.PrivateKeyEnv, originalEnv, dotEnvValues)
	}
}

func harmonizeEnvPair(primaryName, secondaryName string, originalEnv map[string]string, dotEnvValues gotenv.Env) {
	primaryName = strings.TrimSpace(primaryName)
	secondaryName = strings.TrimSpace(secondaryName)
	if primaryName == "" || secondaryName == "" {
		return
	}

	primaryOriginal := strings.TrimSpace(originalEnv[primaryName])
	secondaryOriginal := strings.TrimSpace(originalEnv[secondaryName])
	if primaryOriginal != "" && secondaryOriginal != "" {
		return
	}
	if primaryOriginal == "" && secondaryOriginal == "" {
		return
	}

	primaryDotEnv := strings.TrimSpace(dotEnvValues[primaryName])
	secondaryDotEnv := strings.TrimSpace(dotEnvValues[secondaryName])
	if primaryDotEnv == "" || secondaryDotEnv == "" {
		return
	}

	_ = os.Setenv(primaryName, primaryDotEnv)
	_ = os.Setenv(secondaryName, secondaryDotEnv)
}
