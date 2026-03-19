package exchange

import "strings"

type SymbolRegistry struct {
	assetAliases     map[string]string
	quoteSuffixes    []string
	contractSuffixes []string
}

func NewSymbolRegistry(assetAliases map[string]string, quoteSuffixes, contractSuffixes []string) *SymbolRegistry {
	registry := &SymbolRegistry{
		assetAliases:     make(map[string]string, len(assetAliases)),
		quoteSuffixes:    append([]string(nil), quoteSuffixes...),
		contractSuffixes: append([]string(nil), contractSuffixes...),
	}
	for alias, canonical := range assetAliases {
		aliasKey := strings.ToUpper(strings.TrimSpace(alias))
		canonicalKey := strings.ToUpper(strings.TrimSpace(canonical))
		if aliasKey == "" || canonicalKey == "" {
			continue
		}
		registry.assetAliases[aliasKey] = canonicalKey
	}
	return registry
}

var defaultSymbolRegistry = NewSymbolRegistry(
	map[string]string{
		"XBT": "BTC",
	},
	[]string{"USDT", "USDC", "USD", "FDUSD"},
	[]string{"-PERP", "_PERP", "PERP", "-SWAP", "_SWAP", "SWAP"},
)

func (r *SymbolRegistry) NormalizeAsset(symbol string) string {
	key := strings.TrimSpace(strings.ToUpper(symbol))
	if key == "" {
		return ""
	}
	if r != nil {
		if canonical, ok := r.assetAliases[key]; ok {
			return canonical
		}
	}
	return key
}

func (r *SymbolRegistry) AllowedLookupKeys(symbol string) []string {
	raw := strings.TrimSpace(strings.ToUpper(symbol))
	if raw == "" {
		return nil
	}
	canonical := r.NormalizeAsset(raw)
	if canonical == raw {
		return []string{raw}
	}
	return []string{raw, canonical}
}

func (r *SymbolRegistry) CanonicalAsset(raw, base string) string {
	base = r.NormalizeAsset(base)
	if base != "" {
		return base
	}

	symbol := strings.TrimSpace(strings.ToUpper(raw))
	if symbol == "" {
		return ""
	}
	symbol = r.stripContractSuffixes(symbol)
	symbol = r.stripQuoteSuffixes(symbol)
	symbol = strings.Trim(symbol, "-_ ")
	return r.NormalizeAsset(symbol)
}

func (r *SymbolRegistry) stripContractSuffixes(symbol string) string {
	out := symbol
	if r == nil {
		return out
	}
	for {
		updated := false
		for _, suffix := range r.contractSuffixes {
			suffix = strings.ToUpper(strings.TrimSpace(suffix))
			if suffix == "" || !strings.HasSuffix(out, suffix) {
				continue
			}
			out = strings.TrimSuffix(out, suffix)
			out = strings.Trim(out, "-_ ")
			updated = true
		}
		if !updated {
			return out
		}
	}
}

func (r *SymbolRegistry) stripQuoteSuffixes(symbol string) string {
	out := symbol
	if r == nil {
		return out
	}
	for _, suffix := range r.quoteSuffixes {
		suffix = strings.ToUpper(strings.TrimSpace(suffix))
		if suffix == "" {
			continue
		}
		for _, candidate := range []string{"-" + suffix, "_" + suffix, suffix} {
			if strings.HasSuffix(out, candidate) {
				out = strings.TrimSuffix(out, candidate)
				out = strings.Trim(out, "-_ ")
				return out
			}
		}
	}
	return out
}
