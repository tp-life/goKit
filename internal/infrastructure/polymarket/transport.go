package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/gorilla/websocket"
	"log/slog"
)

// runWSLoop 负责维持 websocket 订阅，直到调用方取消上下文。
func (c *Client) runWSLoop(ctx context.Context, endpoint string, fn func(conn *websocket.Conn) error) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 连接失败后自动重拨，让上层把订阅视为长生命周期流即可。
		conn, _, err := c.dialer.DialContext(ctx, endpoint, nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.logger.Warn("polymarket_ws_dial_failed", slog.String("endpoint", endpoint), slog.Any("err", err))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// 具体消息消费逻辑交给调用方提供的订阅处理函数。
		err = fn(conn)
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			c.logger.Warn("polymarket_ws_loop_failed", slog.String("endpoint", endpoint), slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// doJSON 发起 HTTP 请求，并把 JSON 响应解码到目标对象中。
func (c *Client) doJSON(ctx context.Context, method, rawURL string, body []byte, headers http.Header, out any) error {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}

	// 请求要绑定调用方上下文，这样超时和取消信号才能正确透传。
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPStatusError{
			Method:     method,
			URL:        rawURL,
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(raw)),
		}
	}

	// 调用方只关心成功与否时，也要把响应体读完再返回。
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// HTTPStatusError 表示 HTTP 非 2xx 响应，便于上层按状态码做兼容重试。
type HTTPStatusError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

// Error 输出统一的 HTTP 失败文案。
func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s %s failed with status %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// isInvalidAPIKeyError 判断错误是否属于失效的 L2 API key。
func isInvalidAPIKeyError(err error) bool {
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		return false
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(httpErr.Body))
	return strings.Contains(body, "invalid api key") || strings.Contains(body, "unauthorized")
}

// marshalCompact 生成紧凑 JSON，避免多余换行和 HTML 转义。
func marshalCompact(v any) ([]byte, error) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

// floatValue 尝试把值转成 float64，失败时返回 0。
func floatValue(v any) float64 {
	if p := maybeFloat(v); p != nil {
		return *p
	}
	return 0
}

// maybeFloat 尽量把常见 JSON 标量转换成 float64 指针。
func maybeFloat(v any) *float64 {
	switch value := v.(type) {
	case float64:
		return &value
	case float32:
		f := float64(value)
		return &f
	case int:
		f := float64(value)
		return &f
	case int64:
		f := float64(value)
		return &f
	case json.Number:
		if f, err := value.Float64(); err == nil {
			return &f
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return &f
		}
	}
	return nil
}

// mustPack 执行 ABI 编码；只有静态类型定义本身非法时才会 panic。
func mustPack(args []abi.Argument, values ...any) []byte {
	packed, err := abi.Arguments(args).Pack(values...)
	if err != nil {
		panic(err)
	}
	return packed
}

// mustABIType 构造可复用的 ABI 类型描述，用于包级静态 schema。
func mustABIType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}
