package polymarket

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	relayerTxTypeProxy       = "PROXY"
	relayerTxTypeSafe        = "SAFE"
	relayerStateMined        = "STATE_MINED"
	relayerStateConfirmed    = "STATE_CONFIRMED"
	relayerStateFailed       = "STATE_FAILED"
	relayerStateInvalid      = "STATE_INVALID"
	relayerPollInterval      = 2 * time.Second
	defaultProxyRelayGas     = 10_000_000
	proxyFactoryAddress      = "0xaB45c5A4B0c941a2F231C04C3f49182e1A254052"
	proxyRelayHubAddress     = "0xD216153c06E857cD7f72665E0aF1d7D82172F494"
	safeFactoryAddress       = "0xaacFeEa03eb1561C4e67d661e40682Bd20E3541b"
	safeMultiSendAddress     = "0xA238CBeb142c10Ef7Ad8442C6D1f9E89e07e7761"
	proxyInitCodeHashHex     = "0xd21df8dc65880a8606f09fe0ce3df9b8869287ab0b058be05aa9e8af6330a00b"
	safeInitCodeHashHex      = "0x2bce2127ff07fb632d16c8347c4ebf501f4841168bed00d9e6ef715ddb6fcecf"
	relayerProxyFunctionABI  = `[{"constant":false,"inputs":[{"components":[{"name":"typeCode","type":"uint8"},{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}],"name":"calls","type":"tuple[]"}],"name":"proxy","outputs":[{"name":"returnValues","type":"bytes[]"}],"payable":true,"stateMutability":"payable","type":"function"}]`
	safeOperationCall        = "0"
	relaySignaturePrefixText = "rlx:"
)

var (
	safeDomainTypeHash = ethcrypto.Keccak256Hash([]byte("EIP712Domain(uint256 chainId,address verifyingContract)"))
	safeTxTypeHash     = ethcrypto.Keccak256Hash([]byte("SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)"))
)

type relayerPayloadResponse struct {
	Address string `json:"address"`
	Nonce   string `json:"nonce"`
}

type relayerNonceResponse struct {
	Nonce string `json:"nonce"`
}

type relayerSubmitResponse struct {
	TransactionID   string `json:"transactionID"`
	TransactionHash string `json:"transactionHash"`
	State           string `json:"state"`
}

type relayerTransaction struct {
	TransactionID   string `json:"transactionID"`
	TransactionHash string `json:"transactionHash"`
	ProxyAddress    string `json:"proxyAddress"`
	State           string `json:"state"`
	ErrorMsg        string `json:"errorMsg"`
	Metadata        string `json:"metadata"`
}

type relayerSignatureParams struct {
	GasPrice       string `json:"gasPrice,omitempty"`
	GasLimit       string `json:"gasLimit,omitempty"`
	RelayerFee     string `json:"relayerFee,omitempty"`
	RelayHub       string `json:"relayHub,omitempty"`
	Relay          string `json:"relay,omitempty"`
	Operation      string `json:"operation,omitempty"`
	SafeTxnGas     string `json:"safeTxnGas,omitempty"`
	BaseGas        string `json:"baseGas,omitempty"`
	GasToken       string `json:"gasToken,omitempty"`
	RefundReceiver string `json:"refundReceiver,omitempty"`
}

type relayerTransactionRequest struct {
	Type            string                 `json:"type"`
	From            string                 `json:"from"`
	To              string                 `json:"to"`
	ProxyWallet     string                 `json:"proxyWallet"`
	Data            string                 `json:"data"`
	Nonce           string                 `json:"nonce"`
	Signature       string                 `json:"signature"`
	SignatureParams relayerSignatureParams `json:"signatureParams"`
	Metadata        string                 `json:"metadata,omitempty"`
}

type proxyRelayerCall struct {
	TypeCode uint8          `abi:"typeCode"`
	To       common.Address `abi:"to"`
	Value    *big.Int       `abi:"value"`
	Data     []byte         `abi:"data"`
}

// relayerAPIURL 构造 relayer API 请求地址，并拼接查询参数。
func (c *Client) relayerAPIURL(path string, params url.Values) string {
	endpoint, err := url.Parse(strings.TrimRight(c.cfg.RelayerURL, "/") + path)
	if err != nil {
		return strings.TrimRight(c.cfg.RelayerURL, "/") + path
	}
	if params != nil {
		endpoint.RawQuery = params.Encode()
	}
	return endpoint.String()
}

// relayerTransactionType 把签名类型映射成 relayer 认识的钱包类型。
func (c *Client) relayerTransactionType() (string, error) {
	switch c.cfg.SignatureType {
	case 1:
		return relayerTxTypeProxy, nil
	case 2:
		return relayerTxTypeSafe, nil
	default:
		return "", errors.New("当前签名类型不需要走 relayer")
	}
}

// buildRelayerHeaders 为 /submit 生成 Builder 或 Relayer API Key 请求头。
func (c *Client) buildRelayerHeaders(method, path, body string) (http.Header, error) {
	headers := http.Header{}

	// Builder API 凭证优先级更高，因为它能与官方 SDK 保持完全一致。
	if c.cfg.HasBuilderRelayerAuth() {
		ts := time.Now().Unix()
		sig, err := buildHMACSignature(c.cfg.BuilderSecret, ts, method, path, body)
		if err != nil {
			return nil, err
		}
		headers.Set("POLY_BUILDER_API_KEY", c.cfg.BuilderAPIKey)
		headers.Set("POLY_BUILDER_TIMESTAMP", big.NewInt(ts).String())
		headers.Set("POLY_BUILDER_PASSPHRASE", c.cfg.BuilderPassphrase)
		headers.Set("POLY_BUILDER_SIGNATURE", sig)
		return headers, nil
	}

	if c.cfg.HasRelayerAPIKeyAuth() {
		owner := strings.TrimSpace(c.cfg.RelayerAPIKeyAddress)
		if owner == "" {
			owner = c.AddressHex()
		}
		if strings.TrimSpace(owner) == "" {
			return nil, errors.New("缺少 POLYMARKET_RELAYER_API_KEY_ADDRESS 或 PRIVATE_KEY，无法生成 relayer headers")
		}
		headers.Set("RELAYER_API_KEY", c.cfg.RelayerAPIKey)
		headers.Set("RELAYER_API_KEY_ADDRESS", owner)
		return headers, nil
	}

	return nil, errors.New("缺少 Builder API 凭证或 Relayer API Key，无法调用 relayer")
}

// signMessageHash 复现官方 SDK 的 signMessage 行为，对 32 字节摘要做 personal_sign。
func (c *Client) signMessageHash(hash common.Hash) (string, error) {
	if !c.HasPrivateKey() {
		return "", errors.New("private key is required")
	}
	sig, err := ethcrypto.Sign(accounts.TextHash(hash.Bytes()), c.privateKey)
	if err != nil {
		return "", err
	}
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig), nil
}

// fetchRelayerPayload 获取 Proxy 模式所需的 relayer 地址和 nonce。
func (c *Client) fetchRelayerPayload(ctx context.Context, txType string) (*relayerPayloadResponse, error) {
	params := url.Values{}
	params.Set("address", c.AddressHex())
	params.Set("type", txType)

	var payload relayerPayloadResponse
	if err := c.doJSON(ctx, http.MethodGet, c.relayerAPIURL("/relay-payload", params), nil, nil, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// fetchRelayerNonce 获取 Safe 模式当前应使用的钱包 nonce。
func (c *Client) fetchRelayerNonce(ctx context.Context, txType string) (*relayerNonceResponse, error) {
	params := url.Values{}
	params.Set("address", c.AddressHex())
	params.Set("type", txType)

	var payload relayerNonceResponse
	if err := c.doJSON(ctx, http.MethodGet, c.relayerAPIURL("/nonce", params), nil, nil, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// submitRelayerTransaction 向官方 relayer 发送一笔已经签好的请求。
func (c *Client) submitRelayerTransaction(ctx context.Context, request relayerTransactionRequest) (*relayerSubmitResponse, error) {
	body, err := marshalCompact(request)
	if err != nil {
		return nil, err
	}
	headers, err := c.buildRelayerHeaders(http.MethodPost, "/submit", string(body))
	if err != nil {
		return nil, err
	}

	var response relayerSubmitResponse
	if err := c.doJSON(ctx, http.MethodPost, c.relayerAPIURL("/submit", nil), body, headers, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// waitRelayerTransaction 轮询 relayer 事务状态，直到成功、失败或调用方超时。
func (c *Client) waitRelayerTransaction(ctx context.Context, transactionID string) (*relayerTransaction, error) {
	params := url.Values{}
	params.Set("id", transactionID)

	ticker := time.NewTicker(relayerPollInterval)
	defer ticker.Stop()

	for {
		var rows []relayerTransaction
		if err := c.doJSON(ctx, http.MethodGet, c.relayerAPIURL("/transaction", params), nil, nil, &rows); err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			row := rows[0]
			switch strings.ToUpper(strings.TrimSpace(row.State)) {
			case relayerStateMined, relayerStateConfirmed:
				return &row, nil
			case relayerStateFailed, relayerStateInvalid:
				return &row, errors.New(formatRelayerFailure(row))
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// deriveProxyWalletAddress 按官方 Proxy factory 规则推导 signer 对应的钱包地址。
func deriveProxyWalletAddress(signer common.Address) common.Address {
	return create2AddressFromHash(
		common.HexToAddress(proxyFactoryAddress),
		ethcrypto.Keccak256Hash(signer.Bytes()),
		common.HexToHash(proxyInitCodeHashHex),
	)
}

// deriveSafeWalletAddress 按官方 Safe factory 规则推导 signer 对应的钱包地址。
func deriveSafeWalletAddress(signer common.Address) common.Address {
	encodedAddress := mustPack([]abi.Argument{{Type: addressType}}, signer)
	return create2AddressFromHash(
		common.HexToAddress(safeFactoryAddress),
		ethcrypto.Keccak256Hash(encodedAddress),
		common.HexToHash(safeInitCodeHashHex),
	)
}

// create2AddressFromHash 基于已经给定的 bytecode hash 直接推导 CREATE2 地址。
func create2AddressFromHash(factory common.Address, salt common.Hash, bytecodeHash common.Hash) common.Address {
	payload := make([]byte, 0, 1+20+32+32)
	payload = append(payload, 0xff)
	payload = append(payload, factory.Bytes()...)
	payload = append(payload, salt.Bytes()...)
	payload = append(payload, bytecodeHash.Bytes()...)
	hash := ethcrypto.Keccak256(payload)
	return common.BytesToAddress(hash[12:])
}

// validateExpectedProxyWallet 确认配置里的 FUNDER_ADDRESS 与 signer 推导出的目标钱包一致。
func (c *Client) validateExpectedProxyWallet(expected common.Address) error {
	if strings.TrimSpace(c.cfg.FunderAddress) == "" {
		return nil
	}
	if !strings.EqualFold(expected.Hex(), c.FunderHex()) {
		return errors.New("FUNDER_ADDRESS 与 signer 推导出的 relayer 钱包地址不一致")
	}
	return nil
}

// encodeProxyRelayData 把一条目标交易封装成 Proxy factory.proxy(calls) 的 calldata。
func encodeProxyRelayData(target common.Address, data []byte) ([]byte, error) {
	parsedABI, err := abi.JSON(strings.NewReader(relayerProxyFunctionABI))
	if err != nil {
		return nil, err
	}
	calls := []proxyRelayerCall{{
		TypeCode: 1,
		To:       target,
		Value:    big.NewInt(0),
		Data:     data,
	}}
	return parsedABI.Pack("proxy", calls)
}

// estimateProxyRelayGas 尽量复用链上估算；失败时回退到官方 SDK 的默认 gasLimit。
func (c *Client) estimateProxyRelayGas(ctx context.Context, proxyData []byte) string {
	if strings.TrimSpace(c.cfg.PolygonRPCURL) == "" {
		return big.NewInt(defaultProxyRelayGas).String()
	}

	client, err := DialProxyEthClient(ctx, c.cfg)
	if err != nil {
		return big.NewInt(defaultProxyRelayGas).String()
	}
	defer client.Close()

	proxyFactory := common.HexToAddress(proxyFactoryAddress)
	limit, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From: c.address,
		To:   &proxyFactory,
		Data: proxyData,
	})
	if err != nil || limit == 0 {
		return big.NewInt(defaultProxyRelayGas).String()
	}
	return new(big.Int).SetUint64(limit).String()
}

// proxyRelayDigest 复现官方 Proxy relayer 客户端的交易摘要算法。
func proxyRelayDigest(from, to common.Address, data []byte, gasPrice, gasLimit, nonce, relayHub, relay string) (common.Hash, error) {
	txFee := big.NewInt(0)
	gasPriceInt, err := decimalStringToBigInt(gasPrice)
	if err != nil {
		return common.Hash{}, err
	}
	gasLimitInt, err := decimalStringToBigInt(gasLimit)
	if err != nil {
		return common.Hash{}, err
	}
	nonceInt, err := decimalStringToBigInt(nonce)
	if err != nil {
		return common.Hash{}, err
	}

	payload := make([]byte, 0, 4+20+20+len(data)+32+32+32+32+20+20)
	payload = append(payload, []byte(relaySignaturePrefixText)...)
	payload = append(payload, from.Bytes()...)
	payload = append(payload, to.Bytes()...)
	payload = append(payload, data...)
	payload = append(payload, common.LeftPadBytes(txFee.Bytes(), 32)...)
	payload = append(payload, common.LeftPadBytes(gasPriceInt.Bytes(), 32)...)
	payload = append(payload, common.LeftPadBytes(gasLimitInt.Bytes(), 32)...)
	payload = append(payload, common.LeftPadBytes(nonceInt.Bytes(), 32)...)
	payload = append(payload, common.HexToAddress(relayHub).Bytes()...)
	payload = append(payload, common.HexToAddress(relay).Bytes()...)
	return ethcrypto.Keccak256Hash(payload), nil
}

// safeRelayDigest 复现官方 Safe relayer 客户端生成 SafeTx typed-data hash 的流程。
func safeRelayDigest(chainID int64, safeAddress, target common.Address, data []byte, nonce string) (common.Hash, error) {
	nonceInt, err := decimalStringToBigInt(nonce)
	if err != nil {
		return common.Hash{}, err
	}

	structEncoded := mustPack(
		[]abi.Argument{
			{Type: bytes32Type},
			{Type: addressType},
			{Type: uint256Type},
			{Type: bytes32Type},
			{Type: uint8Type},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: addressType},
			{Type: addressType},
			{Type: uint256Type},
		},
		safeTxTypeHash,
		target,
		big.NewInt(0),
		ethcrypto.Keccak256Hash(data),
		uint8(0),
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
		common.HexToAddress(zeroAddress),
		common.HexToAddress(zeroAddress),
		nonceInt,
	)
	structHash := ethcrypto.Keccak256Hash(structEncoded)

	domainSeparator := ethcrypto.Keccak256Hash(mustPack(
		[]abi.Argument{
			{Type: bytes32Type},
			{Type: uint256Type},
			{Type: addressType},
		},
		safeDomainTypeHash,
		big.NewInt(chainID),
		safeAddress,
	))
	return ethcrypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), structHash.Bytes()), nil
}

// packSafeRelayerSignature 把标准签名转换成 Safe 接受的打包格式。
func packSafeRelayerSignature(signature string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil {
		return "", err
	}
	if len(raw) != 65 {
		return "", errors.New("safe signature 长度非法")
	}

	v := raw[64]
	switch v {
	case 0, 1:
		v += 31
	case 27, 28:
		v += 4
	default:
		return "", errors.New("invalid safe signature")
	}

	packed := make([]byte, 0, 65)
	packed = append(packed, raw[:64]...)
	packed = append(packed, v)
	return "0x" + hex.EncodeToString(packed), nil
}

// decimalStringToBigInt 把十进制字符串稳定转换为大整数。
func decimalStringToBigInt(value string) (*big.Int, error) {
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimSpace(value), 10); !ok {
		return nil, errors.New("invalid decimal integer: " + value)
	}
	return n, nil
}

// firstNonEmptyString 返回第一个非空字符串，方便兼容 relayer 各阶段回包字段。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// formatRelayerFailure 把 relayer 返回的失败态压成一条适合日志和 TUI 展示的错误消息。
func formatRelayerFailure(row relayerTransaction) string {
	parts := []string{"relayer 交易执行失败: " + row.State}
	if strings.TrimSpace(row.ErrorMsg) != "" {
		parts = append(parts, row.ErrorMsg)
	}
	if strings.TrimSpace(row.TransactionHash) != "" {
		parts = append(parts, "tx="+row.TransactionHash)
	}
	if strings.TrimSpace(row.TransactionID) != "" {
		parts = append(parts, "id="+row.TransactionID)
	}
	return strings.Join(parts, " | ")
}
