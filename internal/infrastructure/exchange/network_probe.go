package exchange

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type NetworkProbe struct {
	Exchange     string `json:"exchange"`
	RestBaseURL  string `json:"rest_base_url"`
	ProxyEnabled bool   `json:"proxy_enabled"`
	ProxyURL     string `json:"proxy_url,omitempty"`
	EgressIP     string `json:"egress_ip,omitempty"`
	ServiceURL   string `json:"service_url,omitempty"`
}

func DetectTradeEgressIP(ctx context.Context, exchangeName string, cfg ExchangeConfig, logger *slog.Logger) (NetworkProbe, error) {
	cfg = normalizeExchangeConfig(exchangeName, cfg)
	probe := NetworkProbe{
		Exchange:     normalizeExchangeName(exchangeName),
		RestBaseURL:  strings.TrimSpace(cfg.RestBaseURL),
		ProxyEnabled: cfg.Proxy.Enabled,
		ProxyURL:     strings.TrimSpace(cfg.Proxy.URL),
	}
	client := newHTTPClient(cfg, loadAppConfig(), logger, exchangeName+"-egress-probe")

	services := []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://ifconfig.me/ip",
	}
	var errs []string
	for _, serviceURL := range services {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", serviceURL, err))
			continue
		}
		req.Header.Set("User-Agent", "goKit-tradeprobe/1.0")
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", serviceURL, err))
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", serviceURL, readErr))
			continue
		}
		if resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("%s: status=%d body=%s", serviceURL, resp.StatusCode, strings.TrimSpace(string(body))))
			continue
		}
		ip := strings.TrimSpace(string(body))
		if ip == "" {
			errs = append(errs, fmt.Sprintf("%s: empty response", serviceURL))
			continue
		}
		probe.EgressIP = ip
		probe.ServiceURL = serviceURL
		return probe, nil
	}
	return probe, fmt.Errorf("detect egress ip failed: %s", strings.Join(errs, "; "))
}
