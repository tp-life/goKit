package polymarket

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetWalletPositions 查询指定钱包当前仍持有的仓位。
func (c *Client) GetWalletPositions(ctx context.Context, user string) ([]DataPositionResponse, error) {
	normalizedUser := strings.ToLower(strings.TrimSpace(user))
	if normalizedUser == "" {
		return []DataPositionResponse{}, nil
	}

	params := url.Values{}
	params.Set("user", normalizedUser)
	params.Set("sizeThreshold", "0")

	var rows []DataPositionResponse
	if err := c.doJSON(ctx, http.MethodGet, c.dataAPIURL("/positions", params), nil, nil, &rows); err != nil {
		return nil, err
	}

	// 只保留仍然有效的可交易仓位，过滤掉已兑奖或可 merge 的项目。
	out := make([]DataPositionResponse, 0, len(rows))
	for _, row := range rows {
		if row.Size.OrZero() <= 0 {
			continue
		}
		if row.Redeemable || row.Mergeable {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// GetWalletClosedPositions 查询指定钱包的已关闭仓位历史。
func (c *Client) GetWalletClosedPositions(ctx context.Context, user string) ([]DataClosedPositionResponse, error) {
	normalizedUser := strings.ToLower(strings.TrimSpace(user))
	if normalizedUser == "" {
		return []DataClosedPositionResponse{}, nil
	}

	params := url.Values{}
	params.Set("user", normalizedUser)
	params.Set("limit", "200")
	params.Set("offset", "0")
	params.Set("sortBy", "TIMESTAMP")
	params.Set("sortDirection", "DESC")

	var rows []DataClosedPositionResponse
	if err := c.doJSON(ctx, http.MethodGet, c.dataAPIURL("/closed-positions", params), nil, nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetTradeActivity 查询指定钱包的活动流水，并兼容多种历史查询参数写法。
func (c *Client) GetTradeActivity(ctx context.Context, user string, limit int) ([]DataActivityResponse, error) {
	normalizedUser := strings.ToLower(strings.TrimSpace(user))
	if normalizedUser == "" {
		return []DataActivityResponse{}, nil
	}

	if limit < 50 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	paramSets := []url.Values{
		{"user": []string{normalizedUser}, "limit": []string{strconv.Itoa(limit)}, "offset": []string{"0"}},
		{"user": []string{normalizedUser}},
		{"address": []string{normalizedUser}, "limit": []string{strconv.Itoa(limit)}, "offset": []string{"0"}},
		{"wallet": []string{normalizedUser}, "limit": []string{strconv.Itoa(limit)}, "offset": []string{"0"}},
	}

	var lastErr error
	for _, params := range paramSets {
		var rows []DataActivityResponse
		if err := c.doJSON(ctx, http.MethodGet, c.dataAPIURL("/activity", params), nil, nil, &rows); err != nil {
			lastErr = err
			continue
		}
		return rows, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return []DataActivityResponse{}, nil
}

// dataAPIURL 构造 Data API 请求 URL，并拼上查询参数。
func (c *Client) dataAPIURL(path string, params url.Values) string {
	endpoint, err := url.Parse(strings.TrimRight(c.cfg.DataAPI, "/") + path)
	if err != nil {
		return strings.TrimRight(c.cfg.DataAPI, "/") + path
	}
	endpoint.RawQuery = params.Encode()
	return endpoint.String()
}
