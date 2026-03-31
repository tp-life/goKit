package polymarket

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
	"log/slog"
)

const (
	messageToSign   = "This message attests that I control the given wallet"
	zeroAddress     = "0x0000000000000000000000000000000000000000"
	polygonExchange = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	negRiskExchange = "0xC5d563A36AE78145C45a50134d48A1215220f80a"
)

var (
	bytes32Type = mustABIType("bytes32")
	addressType = mustABIType("address")
	stringType  = mustABIType("string")
	uint256Type = mustABIType("uint256")
	uint8Type   = mustABIType("uint8")
)

var (
	clobDomainTypeHash  = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId)"))
	clobAuthTypeHash    = crypto.Keccak256Hash([]byte("ClobAuth(address address,string timestamp,uint256 nonce,string message)"))
	orderDomainTypeHash = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	orderTypeHash       = crypto.Keccak256Hash([]byte("Order(uint256 salt,address maker,address signer,address taker,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint256 expiration,uint256 nonce,uint256 feeRateBps,uint8 side,uint8 signatureType)"))
	clobDomainNameHash  = crypto.Keccak256Hash([]byte("ClobAuthDomain"))
	clobVersionHash     = crypto.Keccak256Hash([]byte("1"))
	orderDomainNameHash = crypto.Keccak256Hash([]byte("Polymarket CTF Exchange"))
	orderVersionHash    = crypto.Keccak256Hash([]byte("1"))
)

// APIKeyCreds 表示通过钱包派生出来的 Polymarket L2 凭证。
type APIKeyCreds struct {
	Key        string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// OrderStatus 表示服务层直接消费的订单成交状态视图。
type OrderStatus struct {
	Status       string
	OriginalSize float64
	SizeMatched  float64
	Filled       bool
}

// SignedOrder 表示可直接提交到 CLOB 的标准化 EIP-712 订单载荷。
type SignedOrder struct {
	Salt          *big.Int
	Maker         string
	Signer        string
	Taker         string
	TokenID       string
	MakerAmount   string
	TakerAmount   string
	Expiration    string
	Nonce         string
	FeeRateBps    string
	Side          string
	SignatureType int
	Signature     string
}

type signedOrderPayload struct {
	Order struct {
		Salt          json.Number `json:"salt"`
		Maker         string      `json:"maker"`
		Signer        string      `json:"signer"`
		Taker         string      `json:"taker"`
		TokenID       string      `json:"tokenId"`
		MakerAmount   string      `json:"makerAmount"`
		TakerAmount   string      `json:"takerAmount"`
		Expiration    string      `json:"expiration"`
		Nonce         string      `json:"nonce"`
		FeeRateBps    string      `json:"feeRateBps"`
		Side          string      `json:"side"`
		SignatureType int         `json:"signatureType"`
		Signature     string      `json:"signature"`
	} `json:"order"`
	Owner     string `json:"owner"`
	OrderType string `json:"orderType"`
	DeferExec bool   `json:"deferExec"`
	PostOnly  bool   `json:"postOnly,omitempty"`
}

// Client 是 Polymarket 基础设施 SDK 的具体实现。
type Client struct {
	cfg    Config
	logger *slog.Logger
	http   *http.Client
	dialer *websocket.Dialer

	privateKey *ecdsa.PrivateKey
	address    common.Address
	funder     common.Address

	credsMu sync.Mutex
	creds   *APIKeyCreds

	cacheMu    sync.RWMutex
	tickSizes  map[string]string
	negRisk    map[string]bool
	feeRateBps map[string]int
}

// NewClient 创建供上层服务使用的 Polymarket SDK 实例。
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	httpClient, err := NewProxyHTTPClient(cfg, 12*time.Second)
	if err != nil {
		return nil, err
	}
	dialer, err := NewProxyWebsocketDialer(cfg, 10*time.Second)
	if err != nil {
		return nil, err
	}

	client := &Client{
		cfg:        cfg,
		logger:     logger,
		http:       httpClient,
		dialer:     dialer,
		tickSizes:  map[string]string{},
		negRisk:    map[string]bool{},
		feeRateBps: map[string]int{},
	}

	// 如果调用方已经准备好了固定的 L2 API 凭证，就直接注入，避免每次启动都重新派生。
	if cfg.APIKey != "" && cfg.APISecret != "" && cfg.APIPassphrase != "" {
		client.creds = &APIKeyCreds{
			Key:        cfg.APIKey,
			Secret:     cfg.APISecret,
			Passphrase: cfg.APIPassphrase,
		}
	} else if logger != nil {
		// 只配置了部分字段时给出提示，并继续回退到自动派生流程。
		partialCount := 0
		for _, value := range []string{cfg.APIKey, cfg.APISecret, cfg.APIPassphrase} {
			if strings.TrimSpace(value) != "" {
				partialCount++
			}
		}
		if partialCount > 0 {
			logger.Warn("polymarket_api_creds_partial_config", slog.Int("configured_fields", partialCount))
		}
	}

	// 允许在没有钱包的情况下创建只读客户端，方便监控模式正常启动。
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return client, nil
	}

	// 先标准化私钥字符串，再推导签名地址和资金地址。
	pk := strings.TrimPrefix(strings.TrimSpace(cfg.PrivateKey), "0x")
	privateKey, err := crypto.HexToECDSA(pk)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	client.privateKey = privateKey
	client.address = crypto.PubkeyToAddress(privateKey.PublicKey)
	client.funder = client.address

	// 代理钱包或 magic-link 钱包的出资地址可能与签名地址不同。
	if strings.TrimSpace(cfg.FunderAddress) != "" {
		client.funder = common.HexToAddress(cfg.FunderAddress)
	}

	return client, nil
}

// AddressHex 返回签名钱包地址的十六进制字符串。
func (c *Client) AddressHex() string {
	if c.privateKey == nil {
		return ""
	}
	return c.address.Hex()
}

// FunderHex 返回订单载荷里使用的出资地址。
func (c *Client) FunderHex() string {
	if c.privateKey == nil {
		return ""
	}
	return c.funder.Hex()
}

// HasPrivateKey 返回当前 SDK 是否具备可执行签名操作的私钥。
func (c *Client) HasPrivateKey() bool {
	return c.privateKey != nil
}

// invalidateAPIKey 清空当前缓存的 L2 凭证，供 401 失效场景触发重新派生。
func (c *Client) invalidateAPIKey(reason string) {
	c.credsMu.Lock()
	c.creds = nil
	c.credsMu.Unlock()
	if c.logger != nil {
		c.logger.Warn("polymarket_api_key_invalidated", slog.String("reason", reason))
	}
}
