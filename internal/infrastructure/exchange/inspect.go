package exchange

import "strings"

type CredentialPresence struct {
	Key     string `json:"key"`
	Env     string `json:"env,omitempty"`
	Present bool   `json:"present"`
}

type TradeAdapterInspection struct {
	Exchange       string               `json:"exchange"`
	Label          string               `json:"label"`
	AdapterKind    string               `json:"adapter_kind"`
	AuthMode       string               `json:"auth_mode"`
	ConfigEnabled  bool                 `json:"config_enabled"`
	RuntimeEnabled bool                 `json:"runtime_enabled"`
	RestBaseURL    string               `json:"rest_base_url"`
	ProxyEnabled   bool                 `json:"proxy_enabled"`
	ProxyURL       string               `json:"proxy_url,omitempty"`
	Capabilities   TradeCapabilities    `json:"capabilities"`
	Credentials    []CredentialPresence `json:"credentials"`
}

func InspectTradeAdapters(cfg ConfigSet, adapters []TradeAdapter) []TradeAdapterInspection {
	items := cfg.Items()
	adapterMap := BuildTradeMap(adapters)
	names := sortedExchangeNames(items)
	out := make([]TradeAdapterInspection, 0, len(names))
	for _, name := range names {
		out = append(out, InspectTradeAdapter(name, items[name], adapterMap[name]))
	}
	return out
}

func InspectTradeAdapter(name string, cfg ExchangeConfig, adapter TradeAdapter) TradeAdapterInspection {
	cfg = normalizeExchangeConfig(name, cfg)
	inspection := TradeAdapterInspection{
		Exchange:       normalizeExchangeName(name),
		Label:          cfg.Label,
		AdapterKind:    cfg.AdapterKind,
		ConfigEnabled:  cfg.Enabled,
		RuntimeEnabled: adapter != nil && adapter.Enabled(),
		RestBaseURL:    cfg.RestBaseURL,
		ProxyEnabled:   cfg.Proxy.Enabled,
		ProxyURL:       strings.TrimSpace(cfg.Proxy.URL),
	}
	if adapter != nil {
		inspection.Capabilities = adapter.Capabilities()
	}

	switch normalizeExchangeName(name) {
	case "aster":
		authMode := tradeAuthMode(name, cfg)
		inspection.AuthMode = authMode
		inspection.Credentials = append(inspection.Credentials,
			credentialPresence("api_key", cfg.Auth.APIKeyEnv),
			credentialPresence("api_secret", cfg.Auth.APISecretEnv),
			credentialPresence("account_address", cfg.Auth.AccountAddressEnv),
			credentialPresence("private_key", cfg.Auth.PrivateKeyEnv),
		)
		if _, signerAddr := loadAsterSigner(cfg.Auth); signerAddr != "" {
			inspection.Credentials = append(inspection.Credentials, CredentialPresence{
				Key:     "signer_address",
				Present: true,
			})
		} else {
			inspection.Credentials = append(inspection.Credentials, CredentialPresence{
				Key:     "signer_address",
				Present: false,
			})
		}
	case "binance":
		inspection.AuthMode = tradeAuthMode(name, cfg)
		inspection.Credentials = append(inspection.Credentials,
			credentialPresence("api_key", cfg.Auth.APIKeyEnv),
			credentialPresence("api_secret", cfg.Auth.APISecretEnv),
			credentialPresence("private_key", cfg.Auth.PrivateKeyEnv),
		)
	case "bybit":
		inspection.AuthMode = "v5_hmac"
		inspection.Credentials = append(inspection.Credentials,
			credentialPresence("api_key", cfg.Auth.APIKeyEnv),
			credentialPresence("api_secret", cfg.Auth.APISecretEnv),
		)
		if envName := strings.TrimSpace(cfg.Auth.ExtraEnv["referer"]); envName != "" {
			inspection.Credentials = append(inspection.Credentials, credentialPresence("referer", envName))
		}
	case "hyperliquid":
		inspection.AuthMode = "l1_action_signer"
		inspection.Credentials = append(inspection.Credentials,
			credentialPresence("private_key", cfg.Auth.PrivateKeyEnv),
			credentialPresence("account_address", cfg.Auth.AccountAddressEnv),
			credentialPresence("vault_address", cfg.Auth.VaultAddressEnv),
		)
	default:
		inspection.AuthMode = "legacy_hmac"
		inspection.Credentials = append(inspection.Credentials,
			credentialPresence("api_key", cfg.Auth.APIKeyEnv),
			credentialPresence("api_secret", cfg.Auth.APISecretEnv),
		)
	}

	return inspection
}

func credentialPresence(key, envName string) CredentialPresence {
	return CredentialPresence{
		Key:     key,
		Env:     strings.TrimSpace(envName),
		Present: strings.TrimSpace(readEnvByName(envName)) != "",
	}
}
