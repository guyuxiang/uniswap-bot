package executor

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"uniswap-bot/config"
)

type ExecutionResult struct {
	Success        bool
	TxHash         string
	Error          error
	ErrorCode      string
	GasUsed        uint64
	Timestamp      time.Time
	TokenID        *big.Int
	Amount0        *big.Int
	Amount1        *big.Int
	PoolAddress    string
	BalanceChange0 *big.Int
	BalanceChange1 *big.Int
}

type Executor struct {
	cfg           *config.Config
	ethClient     *ethclient.Client
	rpcClient     *http.Client
	chainID       *big.Int
	privateKey    *ecdsa.PrivateKey
	maxRetries    int
	walletAddress common.Address
}

func NewExecutor(cfg *config.Config) (*Executor, error) {
	client, err := ethclient.Dial(cfg.Uniswap.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethclient: %w", err)
	}

	rpcClient := &http.Client{Timeout: 30 * time.Second}

	privateKey, err := crypto.HexToECDSA(cfg.Bot.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	return &Executor{
		cfg:           cfg,
		ethClient:     client,
		rpcClient:     rpcClient,
		chainID:       big.NewInt(cfg.Uniswap.ChainID),
		privateKey:    privateKey,
		maxRetries:    cfg.Execution.RetryTimes,
		walletAddress: walletAddress,
	}, nil
}

func (e *Executor) GetWalletAddress() common.Address {
	return e.walletAddress
}

func (e *Executor) rpcCall(method string, params map[string]interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  []interface{}{params},
		"id":      1,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := e.rpcClient.Post(e.cfg.Uniswap.RPCURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var respBody map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, err
	}

	if errMsg, ok := respBody["error"]; ok {
		return nil, fmt.Errorf("rpc error: %s", string(errMsg))
	}

	result, ok := respBody["result"]
	if !ok {
		return nil, fmt.Errorf("no result in response")
	}

	return result, nil
}

func (e *Executor) queryPoolAddress(ctx context.Context, token0, token1 common.Address, fee uint32) (common.Address, error) {
	methodID := crypto.Keccak256([]byte("getPool(address,address,uint24)"))[:4]
	
	data := make([]byte, 0, 4+32*3)
	data = append(data, methodID...)
	data = append(data, common.LeftPadBytes(token0.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(token1.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32)...)

	result, callErr := e.rpcCall("eth_call", map[string]interface{}{
		"to":   e.cfg.Uniswap.FactoryAddress,
		"data": fmt.Sprintf("0x%x", data),
	})
	
	if callErr != nil {
		return common.Address{}, fmt.Errorf("pool not found - query failed: %v", callErr)
	}

	addrStr := string(result)
	addrStr = addrStr[len(addrStr)-42:]
	
	if len(addrStr) != 42 {
		return common.Address{}, fmt.Errorf("invalid address length: %d", len(addrStr))
	}

	addr := common.HexToAddress(addrStr)
	
	if addr == (common.Address{}) {
		return common.Address{}, fmt.Errorf("pool not found - zero address")
	}

	code, err := e.rpcCall("eth_getCode", map[string]interface{}{
		"address": addr.Hex(),
		"block":   "latest",
	})
	if err != nil || string(code) == "0x" {
		return common.Address{}, fmt.Errorf("pool not found - no code at address")
	}

	return addr, nil
}

func (e *Executor) CreatePool(ctx context.Context, token0, token1 common.Address, fee uint32) (*ExecutionResult, error) {
	existingPool, err := e.queryPoolAddress(ctx, token0, token1, fee)
	if err == nil && existingPool != (common.Address{}) {
		return &ExecutionResult{
			Success:     true,
			PoolAddress: existingPool.Hex(),
			Timestamp:  time.Now(),
		}, nil
	}

	methodID := crypto.Keccak256([]byte("createPool(address,address,uint24)"))[:4]
	
	token0Bytes := common.LeftPadBytes(token0.Bytes(), 32)
	token1Bytes := common.LeftPadBytes(token1.Bytes(), 32)
	feeBytes := common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32)
	
	input := append(methodID, append(token0Bytes, append(token1Bytes, feeBytes...)...)...)

	return e.sendTransaction(ctx, common.HexToAddress(e.cfg.Uniswap.FactoryAddress), big.NewInt(0), 500000, input, "createPool")
}

func (e *Executor) AddLiquidity(ctx context.Context, token0, token1 common.Address, fee uint32, amount0, amount1 *big.Int, tickLower, tickUpper int32) (*ExecutionResult, error) {
	methodID := crypto.Keccak256([]byte("mint((address,address,uint24,int24,int24,uint256,uint256,uint256,uint256,address,uint256))"))[:4]

	recipient := e.walletAddress
	deadline := big.NewInt(time.Now().Unix() + 300)

	data := []byte{}
	data = append(data, methodID...)
	data = append(data, common.LeftPadBytes(token0.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(token1.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(tickLower)).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(tickUpper)).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount0.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount1.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(0).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(0).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(recipient.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(deadline.Bytes(), 32)...)

	return e.sendTransaction(ctx, common.HexToAddress(e.cfg.Uniswap.PositionManager), big.NewInt(0), 800000, data, "addLiquidity")
}

func (e *Executor) ExecuteSwap(ctx context.Context, tokenIn, tokenOut common.Address, amountIn *big.Int, amountOutMin *big.Int, sqrtPriceLimitX96 *big.Int) (*ExecutionResult, error) {
	methodID := crypto.Keccak256([]byte("exactInputSingle((address,address,uint24,address,uint256,uint256,uint160))"))[:4]

	recipient := e.walletAddress
	deadline := big.NewInt(time.Now().Unix() + 300)

	data := []byte{}
	data = append(data, methodID...)
	data = append(data, common.LeftPadBytes(tokenIn.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(tokenOut.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(e.cfg.Uniswap.FeeTier)).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(recipient.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(deadline.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amountIn.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amountOutMin.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(sqrtPriceLimitX96.Bytes(), 32)...)

	return e.sendTransaction(ctx, common.HexToAddress(e.cfg.Uniswap.SwapRouter), big.NewInt(0), 300000, data, "swap")
}

func (e *Executor) sendTransaction(ctx context.Context, to common.Address, value *big.Int, gasLimit uint64, input []byte, txType string) (*ExecutionResult, error) {
	gasPrice, err := e.ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(int64(e.cfg.Execution.GasPriceMultiplier*100)))
	gasPrice = new(big.Int).Div(gasPrice, big.NewInt(100))

	nonce, err := e.ethClient.PendingNonceAt(ctx, e.walletAddress)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	tx := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, input)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(e.chainID), e.privateKey)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	err = e.ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	receipt, err := waitForReceipt(ctx, e.ethClient, signedTx)
	if err != nil {
		return &ExecutionResult{Success: false, TxHash: signedTx.Hash().Hex(), Error: err, Timestamp: time.Now()}, err
	}

	result := &ExecutionResult{
		Success:   receipt.Status == types.ReceiptStatusSuccessful,
		TxHash:    signedTx.Hash().Hex(),
		GasUsed:   receipt.GasUsed,
		Timestamp: time.Now(),
	}

	if receipt.Status == types.ReceiptStatusSuccessful {
		for _, rlog := range receipt.Logs {
			if len(rlog.Data) >= 96 {
				result.TokenID = new(big.Int).SetBytes(rlog.Data[0:32])
				result.Amount0 = new(big.Int).SetBytes(rlog.Data[32:64])
				result.Amount1 = new(big.Int).SetBytes(rlog.Data[64:96])
			}
			if len(rlog.Data) >= 12 && len(rlog.Data) <= 32 {
				poolAddr := common.BytesToAddress(rlog.Data[12:32])
				if common.IsHexAddress(poolAddr.Hex()) {
					result.PoolAddress = poolAddr.Hex()
				}
			}
		}
	} else {
		result.Error = fmt.Errorf("transaction reverted")
		result.ErrorCode = "EXECUTION_REVERTED"
	}

	return result, nil
}

func waitForReceipt(ctx context.Context, client *ethclient.Client, tx *types.Transaction) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				time.Sleep(2 * time.Second)
				continue
			}
		}
		return receipt, nil
	}
}

func (e *Executor) GetEthClient() *ethclient.Client {
	return e.ethClient
}

func (e *Executor) GetChainID() int64 {
	return e.cfg.Uniswap.ChainID
}

func (e *Executor) Close() {
	e.ethClient.Close()
}

func (e *Executor) GetBalance(ctx context.Context, address common.Address) (*big.Int, error) {
	return e.ethClient.BalanceAt(ctx, address, nil)
}

func (e *Executor) GetTokenBalance(ctx context.Context, tokenAddr, ownerAddr common.Address) (*big.Int, error) {
	return GetTokenBalance(ctx, e.ethClient, tokenAddr, ownerAddr)
}

func PriceToTick(price float64) int32 {
	if price <= 0 {
		return 0
	}
	tick := math.Log(price) / math.Log(1.0001)
	return int32(tick)
}

func TickToPrice(tick int32) float64 {
	return math.Pow(1.0001, float64(tick))
}

func CalculateTickRange(refPrice float64, rangeBps int) (int32, int32) {
	lower := refPrice * (1 - float64(rangeBps)/10000)
	upper := refPrice * (1 + float64(rangeBps)/10000)
	return PriceToTick(lower), PriceToTick(upper)
}

func (e *Executor) VerifyTransaction(ctx context.Context, result *ExecutionResult, token0, token1 common.Address) (bool, string) {
	if result.Success {
		return true, "transaction successful"
	}

	if result.ErrorCode != "" {
		return false, result.ErrorCode
	}

	return false, result.Error.Error()
}
