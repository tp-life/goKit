package polymarket

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const redeemABIJSON = `[{"name":"redeemPositions","type":"function","stateMutability":"nonpayable","inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"indexSets","type":"uint256[]"}],"outputs":[]}]`

// GetRedeemableConditions 查询指定钱包当前可统一兑奖的 condition 列表。
func (c *Client) GetRedeemableConditions(ctx context.Context, user string) ([]string, int, error) {
	normalizedUser := strings.ToLower(strings.TrimSpace(user))
	if normalizedUser == "" {
		return []string{}, 0, nil
	}

	params := url.Values{}
	params.Set("user", normalizedUser)
	params.Set("sizeThreshold", "0")

	var rows []DataPositionResponse
	if err := c.doJSON(ctx, http.MethodGet, c.dataAPIURL("/positions", params), nil, nil, &rows); err != nil {
		return nil, 0, err
	}

	claimable := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	pendingCount := 0
	for _, row := range rows {
		size := row.Size.OrZero()
		if size <= 0 {
			continue
		}
		// 当前实现只支持 resolved 后的 redeemPositions；merge-only 需要另一条链路，先不混进来。
		if !row.Redeemable {
			continue
		}
		// 跳过极小的尘埃仓位，避免反复触发 relayer 但实际没有业务价值。
		if size < c.cfg.AutoRedeemMinSize {
			continue
		}

		pendingCount++
		conditionID := normalizeConditionID(row.ConditionID)
		if conditionID == "" {
			continue
		}
		if _, ok := seen[conditionID]; ok {
			continue
		}
		seen[conditionID] = struct{}{}
		claimable = append(claimable, conditionID)
	}
	return claimable, pendingCount, nil
}

// RedeemCondition 直接向 CTF 合约发送 redeemPositions 交易，并等待链上回执。
func (c *Client) RedeemCondition(ctx context.Context, conditionID string) (*RedeemResult, error) {
	if !c.HasPrivateKey() {
		return nil, errors.New("未配置 PRIVATE_KEY，无法执行兑奖")
	}

	normalizedConditionID := normalizeConditionID(conditionID)
	if normalizedConditionID == "" {
		return nil, errors.New("condition_id 非法")
	}

	parsedABI, err := abi.JSON(strings.NewReader(redeemABIJSON))
	if err != nil {
		return nil, err
	}

	var zeroCollectionID [32]byte
	var conditionBytes [32]byte
	copy(conditionBytes[:], common.FromHex(normalizedConditionID))

	data, err := parsedABI.Pack(
		"redeemPositions",
		common.HexToAddress(c.cfg.USDCCollateral),
		zeroCollectionID,
		conditionBytes,
		[]*big.Int{big.NewInt(1), big.NewInt(2)},
	)
	if err != nil {
		return nil, err
	}

	switch c.cfg.SignatureType {
	case 1:
		return c.redeemConditionViaProxyRelayer(ctx, normalizedConditionID, data)
	case 2:
		return c.redeemConditionViaSafeRelayer(ctx, normalizedConditionID, data)
	}

	if strings.TrimSpace(c.cfg.PolygonRPCURL) == "" {
		return nil, errors.New("未配置 POLYGON_RPC_URL，无法执行兑奖")
	}
	if funder := strings.TrimSpace(c.FunderHex()); funder != "" && !strings.EqualFold(funder, c.AddressHex()) {
		return nil, errors.New("EOA 兑奖要求签名地址与 FUNDER_ADDRESS 一致")
	}
	return c.redeemConditionDirect(ctx, data)
}

// redeemConditionDirect 直接向 CTF 合约发送 redeemPositions 交易，并等待链上回执。
func (c *Client) redeemConditionDirect(ctx context.Context, data []byte) (*RedeemResult, error) {
	client, err := DialProxyEthClient(ctx, c.cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, c.address)
	if err != nil {
		return nil, err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil || gasPrice == nil || gasPrice.Sign() <= 0 {
		gasPrice = big.NewInt(30_000_000_000)
	}

	ctfAddress := common.HexToAddress(c.cfg.CTFContract)
	gasLimit, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From:  c.address,
		To:    &ctfAddress,
		Value: big.NewInt(0),
		Data:  data,
	})
	if err != nil || gasLimit == 0 {
		gasLimit = 300000
	} else {
		gasLimit = gasLimit*120/100 + 5000
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &ctfAddress,
		Value:    big.NewInt(0),
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     data,
	})
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(c.cfg.ChainID)), c.privateKey)
	if err != nil {
		return nil, err
	}
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return &RedeemResult{TxHash: signedTx.Hash().Hex()}, err
	}

	// 发送成功后等待回执，只有真正上链成功才视为兑奖完成。
	receipt, err := bind.WaitMined(ctx, client, signedTx)
	result := &RedeemResult{TxHash: signedTx.Hash().Hex()}
	if receipt != nil {
		result.BlockNumber = receipt.BlockNumber.Uint64()
		result.GasUsed = receipt.GasUsed
		result.Status = receipt.Status
	}
	if err != nil {
		return result, err
	}
	if receipt == nil {
		return result, errors.New("未获取到兑奖交易回执")
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return result, errors.New("兑奖交易已上链但执行失败")
	}
	return result, nil
}

// redeemConditionViaProxyRelayer 通过官方 relayer 代表 Proxy 钱包执行兑奖。
func (c *Client) redeemConditionViaProxyRelayer(ctx context.Context, conditionID string, redeemData []byte) (*RedeemResult, error) {
	if !c.cfg.HasAnyRelayerAuth() {
		return nil, errors.New("POLY_PROXY 兑奖需要配置 Builder API 凭证或 POLYMARKET_RELAYER_API_KEY")
	}

	expectedProxy := deriveProxyWalletAddress(c.address)
	if err := c.validateExpectedProxyWallet(expectedProxy); err != nil {
		return nil, err
	}

	proxyData, err := encodeProxyRelayData(common.HexToAddress(c.cfg.CTFContract), redeemData)
	if err != nil {
		return nil, err
	}

	payload, err := c.fetchRelayerPayload(ctx, relayerTxTypeProxy)
	if err != nil {
		return nil, err
	}
	gasLimit := c.estimateProxyRelayGas(ctx, proxyData)
	digest, err := proxyRelayDigest(
		c.address,
		common.HexToAddress(proxyFactoryAddress),
		proxyData,
		"0",
		gasLimit,
		payload.Nonce,
		proxyRelayHubAddress,
		payload.Address,
	)
	if err != nil {
		return nil, err
	}
	signature, err := c.signMessageHash(digest)
	if err != nil {
		return nil, err
	}

	request := relayerTransactionRequest{
		Type:        relayerTxTypeProxy,
		From:        c.AddressHex(),
		To:          proxyFactoryAddress,
		ProxyWallet: expectedProxy.Hex(),
		Data:        "0x" + hex.EncodeToString(proxyData),
		Nonce:       payload.Nonce,
		Signature:   signature,
		Metadata:    "auto redeem positions " + conditionID,
		SignatureParams: relayerSignatureParams{
			GasPrice:   "0",
			GasLimit:   gasLimit,
			RelayerFee: "0",
			RelayHub:   proxyRelayHubAddress,
			Relay:      payload.Address,
		},
	}
	return c.executeRelayerRedeem(ctx, request)
}

// redeemConditionViaSafeRelayer 通过官方 relayer 代表 Safe 钱包执行兑奖。
func (c *Client) redeemConditionViaSafeRelayer(ctx context.Context, conditionID string, redeemData []byte) (*RedeemResult, error) {
	if !c.cfg.HasAnyRelayerAuth() {
		return nil, errors.New("GNOSIS_SAFE 兑奖需要配置 Builder API 凭证或 POLYMARKET_RELAYER_API_KEY")
	}

	expectedSafe := deriveSafeWalletAddress(c.address)
	if err := c.validateExpectedProxyWallet(expectedSafe); err != nil {
		return nil, err
	}

	noncePayload, err := c.fetchRelayerNonce(ctx, relayerTxTypeSafe)
	if err != nil {
		return nil, err
	}
	digest, err := safeRelayDigest(c.cfg.ChainID, expectedSafe, common.HexToAddress(c.cfg.CTFContract), redeemData, noncePayload.Nonce)
	if err != nil {
		return nil, err
	}
	signature, err := c.signMessageHash(digest)
	if err != nil {
		return nil, err
	}
	packedSignature, err := packSafeRelayerSignature(signature)
	if err != nil {
		return nil, err
	}

	request := relayerTransactionRequest{
		Type:        relayerTxTypeSafe,
		From:        c.AddressHex(),
		To:          c.cfg.CTFContract,
		ProxyWallet: expectedSafe.Hex(),
		Data:        "0x" + hex.EncodeToString(redeemData),
		Nonce:       noncePayload.Nonce,
		Signature:   packedSignature,
		Metadata:    "auto redeem positions " + conditionID,
		SignatureParams: relayerSignatureParams{
			GasPrice:       "0",
			Operation:      safeOperationCall,
			SafeTxnGas:     "0",
			BaseGas:        "0",
			GasToken:       zeroAddress,
			RefundReceiver: zeroAddress,
		},
	}
	return c.executeRelayerRedeem(ctx, request)
}

// executeRelayerRedeem 提交 relayer 请求并轮询直到进入成功或失败终态。
func (c *Client) executeRelayerRedeem(ctx context.Context, request relayerTransactionRequest) (*RedeemResult, error) {
	submitResp, err := c.submitRelayerTransaction(ctx, request)
	if err != nil {
		return nil, err
	}

	result := &RedeemResult{
		TxHash:        strings.TrimSpace(submitResp.TransactionHash),
		TransactionID: strings.TrimSpace(submitResp.TransactionID),
		State:         strings.TrimSpace(submitResp.State),
		ProxyWallet:   request.ProxyWallet,
	}
	if result.TransactionID == "" {
		return result, errors.New("relayer 未返回 transactionID")
	}

	tx, waitErr := c.waitRelayerTransaction(ctx, result.TransactionID)
	if tx != nil {
		result.TxHash = firstNonEmptyString(strings.TrimSpace(tx.TransactionHash), result.TxHash)
		result.State = firstNonEmptyString(strings.TrimSpace(tx.State), result.State)
		result.ProxyWallet = firstNonEmptyString(strings.TrimSpace(tx.ProxyAddress), result.ProxyWallet)
		result.ErrorMessage = firstNonEmptyString(strings.TrimSpace(tx.ErrorMsg), result.ErrorMessage)
	}
	if waitErr != nil {
		return result, waitErr
	}
	return result, nil
}

// normalizeConditionID 把 condition_id 统一归一成 0x 开头的 32 字节十六进制字符串。
func normalizeConditionID(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "0x")
	if len(normalized) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return ""
	}
	return "0x" + normalized
}
