package polymarket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// CreateOrDeriveAPIKey 优先返回预置/缓存凭证，否则使用 L1 鉴权向 Polymarket 申请或派生 API Key。
func (c *Client) CreateOrDeriveAPIKey(ctx context.Context, nonce int) (*APIKeyCreds, error) {
	// 没有钱包私钥就无法构造 L1 签名，也就无法访问需要鉴权的接口。
	if !c.HasPrivateKey() {
		return nil, errors.New("private key is required for authenticated endpoints")
	}

	c.credsMu.Lock()
	defer c.credsMu.Unlock()

	// 优先复用已经派生好的凭证，避免每次请求都重复走昂贵的鉴权握手。
	if c.creds != nil && c.creds.Key != "" {
		creds := *c.creds
		return &creds, nil
	}

	// 先尝试创建新凭证，失败后再回退到派生流程，兼容已经在服务端绑定过凭证的钱包。
	creds, err := c.createAPIKey(ctx, http.MethodPost, "/auth/api-key", nonce)
	if err != nil || creds == nil || creds.Key == "" {
		creds, err = c.createAPIKey(ctx, http.MethodGet, "/auth/derive-api-key", nonce)
		if err != nil {
			return nil, err
		}
	}

	c.creds = creds
	copyCreds := *creds
	return &copyCreds, nil
}

// RefreshAPIKey 丢弃当前缓存凭证，并强制重新创建或派生一组可用的 L2 API key。
func (c *Client) RefreshAPIKey(ctx context.Context, nonce int) (*APIKeyCreds, error) {
	c.invalidateAPIKey("refresh requested")
	return c.CreateOrDeriveAPIKey(ctx, nonce)
}

// createAPIKey 调用 L1 鉴权接口，创建或派生 API 凭证。
func (c *Client) createAPIKey(ctx context.Context, method, path string, nonce int) (*APIKeyCreds, error) {
	// 凭证引导请求本身就需要使用钱包签名头完成认证。
	headers, err := c.buildL1Headers(nonce)
	if err != nil {
		return nil, err
	}
	var creds APIKeyCreds
	if err := c.doJSON(ctx, method, c.cfg.Host+path, nil, headers, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// buildL1Headers 构造鉴权接口要求的钱包签名请求头。
func (c *Client) buildL1Headers(nonce int) (http.Header, error) {
	if !c.HasPrivateKey() {
		return nil, errors.New("private key is required")
	}

	// Polymarket 会把签名与短时效时间戳和调用方 nonce 绑定在一起。
	ts := time.Now().Unix()
	sig, err := c.signDigest(c.clobAuthDigest(ts, nonce))
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("POLY_ADDRESS", c.address.Hex())
	headers.Set("POLY_SIGNATURE", sig)
	headers.Set("POLY_TIMESTAMP", strconv.FormatInt(ts, 10))
	headers.Set("POLY_NONCE", strconv.Itoa(nonce))
	return headers, nil
}

// buildL2Headers 为 CLOB API Key 鉴权构造标准化请求签名头。
func (c *Client) buildL2Headers(ctx context.Context, creds *APIKeyCreds, method, path, body string) (http.Header, error) {
	// 调用方可以不显式传入凭证，这里会按需自动补齐。
	if creds == nil {
		var err error
		creds, err = c.CreateOrDeriveAPIKey(ctx, 0)
		if err != nil {
			return nil, err
		}
	}

	// Polymarket 会对实际发送的 body 字节串签名，因此这里必须使用最终出线的紧凑 JSON。
	ts := time.Now().Unix()
	sig, err := buildHMACSignature(creds.Secret, ts, method, path, body)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("POLY_ADDRESS", c.address.Hex())
	headers.Set("POLY_SIGNATURE", sig)
	headers.Set("POLY_TIMESTAMP", strconv.FormatInt(ts, 10))
	headers.Set("POLY_API_KEY", creds.Key)
	headers.Set("POLY_PASSPHRASE", creds.Passphrase)
	return headers, nil
}

// clobAuthDigest 构造 Polymarket L1 鉴权要求的 EIP-712 摘要。
// 其中 timestamp 和 message 作为动态类型，需先做一次哈希再参与编码。
func (c *Client) clobAuthDigest(timestamp int64, nonce int) common.Hash {
	// 先编码类型化结构体，确保最终摘要与链上 EIP-712 校验规则完全一致。
	encoded := mustPack(
		[]abi.Argument{
			{Type: bytes32Type},
			{Type: addressType},
			{Type: bytes32Type},
			{Type: uint256Type},
			{Type: bytes32Type},
		},
		clobAuthTypeHash,
		c.address,
		crypto.Keccak256Hash([]byte(strconv.FormatInt(timestamp, 10))),
		big.NewInt(int64(nonce)),
		crypto.Keccak256Hash([]byte(messageToSign)),
	)
	structHash := crypto.Keccak256Hash(encoded)

	// 域分隔符会把签名明确绑定到 Polymarket 的鉴权域和链 ID 上。
	domainSeparator := crypto.Keccak256Hash(mustPack(
		[]abi.Argument{
			{Type: bytes32Type},
			{Type: bytes32Type},
			{Type: bytes32Type},
			{Type: uint256Type},
		},
		clobDomainTypeHash,
		clobDomainNameHash,
		clobVersionHash,
		big.NewInt(c.cfg.ChainID),
	))
	return crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), structHash.Bytes())
}

// signDigest 对已构造好的摘要签名，并把 v 标准化到以太坊 27/28 形式。
func (c *Client) signDigest(digest common.Hash) (string, error) {
	sig, err := crypto.Sign(digest.Bytes(), c.privateKey)
	if err != nil {
		return "", err
	}
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig), nil
}

// buildHMACSignature 复现 Polymarket 的标准 HMAC 请求签名流程。
func buildHMACSignature(secret string, timestamp int64, method, requestPath, body string) (string, error) {
	key, err := decodeBase64Secret(secret)
	if err != nil {
		return "", err
	}

	// 待签名串按官方客户端规则直接拼接，中间没有额外分隔符。
	message := strconv.FormatInt(timestamp, 10) + method + requestPath + body
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(message))
	return toURLSafeBase64(mac.Sum(nil)), nil
}

// decodeBase64Secret 同时兼容 base64 与 base64url，并剔除非 base64 字符。
// 这样可以对齐官方客户端较为宽松的兼容行为。
func decodeBase64Secret(secret string) ([]byte, error) {
	var b strings.Builder
	for _, ch := range secret {
		// 先把 base64url 归一化为 base64，再丢掉历史兼容路径里允许出现的杂质字符。
		switch {
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch)
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == '+', ch == '/', ch == '=':
			b.WriteRune(ch)
		case ch == '-':
			b.WriteRune('+')
		case ch == '_':
			b.WriteRune('/')
		}
	}
	normalized := b.String()
	if m := len(normalized) % 4; m != 0 {
		// 解码前补齐标准 base64 padding，避免长度不合法。
		normalized += strings.Repeat("=", 4-m)
	}
	return base64.StdEncoding.DecodeString(normalized)
}

// toURLSafeBase64 保留 `=` padding，但把字符表改写成 URL-safe 形式。
func toURLSafeBase64(data []byte) string {
	s := base64.StdEncoding.EncodeToString(data)
	s = strings.ReplaceAll(s, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}
