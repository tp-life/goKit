package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"
)

const (
	defaultOpportunityLimit = 5000
	defaultPlanLimit        = 100
	defaultExecutionLimit   = 50
)

type apiEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type StrategyStatus struct {
	Enabled                        bool                          `json:"enabled"`
	HoldHours                      float64                       `json:"hold_hours"`
	EffectiveNotional              float64                       `json:"effective_notional"`
	MinNetPNL                      float64                       `json:"min_net_pnl"`
	EntryMode                      string                        `json:"entry_mode"`
	ExitMode                       string                        `json:"exit_mode"`
	MaxDataAge                     string                        `json:"max_data_age"`
	MaxSpreadBps                   float64                       `json:"max_spread_bps"`
	DynamicMaxSpreadMultiplier     float64                       `json:"dynamic_max_spread_multiplier"`
	DynamicMaxSpreadReferenceHours float64                       `json:"dynamic_max_spread_reference_hours"`
	EntryLeadTime                  string                        `json:"entry_lead_time"`
	EntryCutoffTime                string                        `json:"entry_cutoff_time"`
	CapitalTotalUSDT               float64                       `json:"capital_total_usdt"`
	CapitalUtilization             float64                       `json:"capital_utilization"`
	Leverage                       float64                       `json:"leverage"`
	FeesByExchange                 map[string]exchange.FeeConfig `json:"fees_by_exchange"`
	FundingHistoryLookback         string                        `json:"funding_history_lookback"`
	FundingSmoothingCurrentWeight  float64                       `json:"funding_smoothing_current_weight"`
	DynamicCandidateLimit          int                           `json:"dynamic_candidate_limit"`
	RotationBatchSize              int                           `json:"rotation_batch_size"`
	RotationInterval               string                        `json:"rotation_interval"`
	DeepScanHoldDuration           string                        `json:"deep_scan_hold_duration"`
	CoreSymbols                    []string                      `json:"core_symbols"`
}

type ExecutionStatus struct {
	LiveTradingEnabled          bool    `json:"live_trading_enabled"`
	AutoEntry                   bool    `json:"auto_entry"`
	AutoClose                   bool    `json:"auto_close"`
	CloseGracePeriod            string  `json:"close_grace_period"`
	LoopInterval                string  `json:"loop_interval"`
	MaxLatestPlans              int     `json:"max_latest_plans"`
	AutoAllocateCapital         bool    `json:"auto_allocate_capital"`
	MaxLivePlans                int     `json:"max_live_plans"`
	MaxAutoOpenPerLoop          int     `json:"max_auto_open_per_loop"`
	ActiveLivePlans             int     `json:"active_live_plans"`
	ActiveAllocatedNotionalUSDT float64 `json:"active_allocated_notional_usdt"`
	RemainingAutoBudgetUSDT     float64 `json:"remaining_auto_budget_usdt"`
	RemainingLiveSlots          int     `json:"remaining_live_slots"`
}

type SystemStatus struct {
	Watchlist         []string                   `json:"watchlist"`
	DeepScanWatchlist []string                   `json:"deep_scan_watchlist"`
	Connectors        []exchange.ConnectorStatus `json:"connectors"`
	Strategy          StrategyStatus             `json:"strategy"`
	Execution         ExecutionStatus            `json:"execution"`
}

type DashboardData struct {
	System            SystemStatus
	AutoClose         service.AutoCloseInspection
	Opportunities     []repository.OpportunitySummary
	BatchPlans        []entity.ExecutionPlan
	AllPlans          []entity.ExecutionPlan
	Executions        []entity.ExecutionRecord
	Stats             repository.SnapshotStats
	Market            service.SymbolMarketState
	CurrentBatchID    string
	SelectedMarketSym string
}

type Client struct {
	baseURL          string
	token            string
	opportunityLimit int
	httpClient       *http.Client
}

func NewClient(baseURL, token string, opportunityLimit int) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	if opportunityLimit <= 0 {
		opportunityLimit = defaultOpportunityLimit
	}
	return &Client{
		baseURL:          base,
		token:            strings.TrimSpace(token),
		opportunityLimit: opportunityLimit,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) GetMarket(ctx context.Context, symbol string) (service.SymbolMarketState, error) {
	if strings.TrimSpace(symbol) == "" {
		return service.SymbolMarketState{}, nil
	}
	path := fmt.Sprintf("/api/v1/market/%s", url.PathEscape(strings.ToUpper(strings.TrimSpace(symbol))))
	return doJSON[service.SymbolMarketState](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, path, nil)
}

func (c *Client) GetOrders(ctx context.Context, planKey string) ([]entity.OrderRecord, error) {
	if strings.TrimSpace(planKey) == "" {
		return nil, nil
	}
	path := fmt.Sprintf("/api/v1/executions/%s/orders", url.PathEscape(strings.TrimSpace(planKey)))
	return doJSON[[]entity.OrderRecord](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, path, nil)
}

func (c *Client) GetOpportunityDetail(ctx context.Context, id uint) (entity.Opportunity, error) {
	if id == 0 {
		return entity.Opportunity{}, nil
	}
	path := fmt.Sprintf("/api/v1/opportunities/%d", id)
	return doJSON[entity.Opportunity](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, path, nil)
}

func (c *Client) OpenPlan(ctx context.Context, planKey string) error {
	if strings.TrimSpace(planKey) == "" {
		return fmt.Errorf("missing plan key")
	}
	path := fmt.Sprintf("/api/v1/executions/%s/open", url.PathEscape(strings.TrimSpace(planKey)))
	_, err := doJSON[map[string]any](ctx, c.httpClient, c.baseURL, c.token, http.MethodPost, path, map[string]any{})
	return err
}

func (c *Client) ClosePlan(ctx context.Context, planKey string) error {
	if strings.TrimSpace(planKey) == "" {
		return fmt.Errorf("missing plan key")
	}
	path := fmt.Sprintf("/api/v1/executions/%s/close", url.PathEscape(strings.TrimSpace(planKey)))
	_, err := doJSON[map[string]any](ctx, c.httpClient, c.baseURL, c.token, http.MethodPost, path, map[string]any{})
	return err
}

func (c *Client) getSystemStatus(ctx context.Context) (SystemStatus, error) {
	return doJSON[SystemStatus](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, "/api/v1/system/status", nil)
}

func (c *Client) getOpportunitySummaries(ctx context.Context, limit int) ([]repository.OpportunitySummary, error) {
	path := fmt.Sprintf("/api/v1/opportunities/summary?limit=%d", limit)
	return doJSON[[]repository.OpportunitySummary](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, path, nil)
}

func (c *Client) getExecutions(ctx context.Context, limit int) ([]entity.ExecutionRecord, error) {
	path := fmt.Sprintf("/api/v1/executions?limit=%d", limit)
	return doJSON[[]entity.ExecutionRecord](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, path, nil)
}

func (c *Client) getAutoCloseCandidates(ctx context.Context) (service.AutoCloseInspection, error) {
	return doJSON[service.AutoCloseInspection](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, "/api/v1/executions/auto-close-candidates", nil)
}

func (c *Client) getSnapshotStats(ctx context.Context) (repository.SnapshotStats, error) {
	return doJSON[repository.SnapshotStats](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, "/api/v1/snapshot-stats", nil)
}

func (c *Client) getPlans(ctx context.Context, limit int, batchID string) ([]entity.ExecutionPlan, error) {
	path := fmt.Sprintf("/api/v1/plans?limit=%d", limit)
	if strings.TrimSpace(batchID) != "" {
		path = fmt.Sprintf("%s&opportunity_batch_id=%s", path, url.QueryEscape(strings.TrimSpace(batchID)))
	}
	return doJSON[[]entity.ExecutionPlan](ctx, c.httpClient, c.baseURL, c.token, http.MethodGet, path, nil)
}

func doJSON[T any](ctx context.Context, httpClient *http.Client, baseURL string, token string, method string, path string, payload any) (T, error) {
	var zero T

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return zero, err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}

	var envelope apiEnvelope[T]
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err == nil {
			if resp.StatusCode >= http.StatusBadRequest {
				msg := strings.TrimSpace(envelope.Message)
				if msg == "" {
					msg = strings.TrimSpace(string(raw))
				}
				return zero, fmt.Errorf("http %d: %s", resp.StatusCode, msg)
			}
			return envelope.Data, nil
		}
	}

	if resp.StatusCode >= http.StatusBadRequest {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return zero, fmt.Errorf("http %d: %s", resp.StatusCode, msg)
	}

	if len(raw) == 0 {
		return zero, nil
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}
