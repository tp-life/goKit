package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	client      *http.Client
	logger      *slog.Logger
	retryConfig *RetryConfig
	proxyURL    string
}

type RetryConfig struct {
	MaxAttempts int
	Backoff     time.Duration
	Retryable   func(*http.Response, error) bool
}

func NewClient(logger *slog.Logger, proxyURL string) *Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURLParsed)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &Client{
		client:   client,
		logger:   logger,
		proxyURL: proxyURL,
		retryConfig: &RetryConfig{
			MaxAttempts: 3,
			Backoff:     1 * time.Second,
			Retryable:   defaultRetryable,
		},
	}
}

func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	return c.Do(ctx, "GET", url, nil, nil)
}

func (c *Client) Post(ctx context.Context, url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	return c.Do(ctx, "POST", url, body, headers)
}

func (c *Client) Do(ctx context.Context, method, url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt < c.retryConfig.MaxAttempts; attempt++ {
		if attempt > 0 {
			backoff := c.retryConfig.Backoff * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, lastErr = c.client.Do(req)
		if lastErr == nil && resp != nil {
			if !c.retryConfig.Retryable(resp, nil) {
				return resp, nil
			}
			resp.Body.Close()
		}

		if attempt < c.retryConfig.MaxAttempts-1 {
			c.logger.Warn("http_request_retry",
				slog.Int("attempt", attempt+1),
				slog.String("url", url),
				slog.Any("err", lastErr),
			)
		}
	}

	return resp, lastErr
}

func defaultRetryable(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp.StatusCode >= 500 || resp.StatusCode == 429
}

func (c *Client) SetTimeout(timeout time.Duration) {
	c.client.Timeout = timeout
}

func (c *Client) SetRetryConfig(config *RetryConfig) {
	c.retryConfig = config
}
