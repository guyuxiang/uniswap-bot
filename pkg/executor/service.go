package executor

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"uniswap-bot/config"
)

type ExecutionResult struct {
	Success     bool
	TxHash      string
	Error       error
	GasUsed     uint64
	Timestamp   time.Time
	TokenID     *big.Int
	Amount0     *big.Int
	Amount1     *big.Int
	PoolAddress string
}

type Executor struct {
	cfg        *config.Config
	ethClient  *ethclient.Client
	transactor *types.Transaction
	chainID    *big.Int
	privateKey *ecdsa.PrivateKey
	maxRetries int
	nonce      uint64
}

func NewExecutor(cfg *config.Config) (*Executor, error) {
	client, err := ethclient.Dial(cfg.Uniswap.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethclient: %w", err)
	}

	privateKey, err := crypto.HexToECDSA(cfg.Bot.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return &Executor{
		cfg:        cfg,
		ethClient:  client,
		chainID:    big.NewInt(cfg.Uniswap.ChainID),
		privateKey: privateKey,
		maxRetries: cfg.Execution.RetryTimes,
	}, nil
}

func (e *Executor) CreatePool(ctx context.Context, token0, token1 common.Address, fee uint32) (*ExecutionResult, error) {
	methodID := crypto.Keccak256([]byte("createPool(address,address,uint24)"))[:4]
	
	token0Bytes := common.LeftPadBytes(token0.Bytes(), 32)
	token1Bytes := common.LeftPadBytes(token1.Bytes(), 32)
	feeBytes := common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32)
	
	input := append(methodID, append(token0Bytes, append(token1Bytes, feeBytes...)...)...)

	return e.sendTransaction(ctx, common.HexToAddress(e.cfg.Uniswap.FactoryAddress), big.NewInt(0), 500000, input)
}

func (e *Executor) AddLiquidity(ctx context.Context, token0, token1 common.Address, fee uint32, amount0, amount1 *big.Int, tickLower, tickUpper int32) (*ExecutionResult, error) {
	methodID := crypto.Keccak256([]byte("mint((address,address,uint24,int24,int24,uint256,uint256,uint256,uint256,address,uint256))"))[:4]

	recipient := crypto.PubkeyToAddress(e.privateKey.PublicKey)
	deadline := big.NewInt(time.Now().Unix() + 300)

	data := []byte{}
	data = append(data, methodID...)

	token0Bytes := common.LeftPadBytes(token0.Bytes(), 32)
	data = append(data, token0Bytes...)

	token1Bytes := common.LeftPadBytes(token1.Bytes(), 32)
	data = append(data, token1Bytes...)

	feeBytes := common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32)
	data = append(data, feeBytes...)

	tickLowerBytes := common.LeftPadBytes(big.NewInt(int64(tickLower)).Bytes(), 32)
	data = append(data, tickLowerBytes...)

	tickUpperBytes := common.LeftPadBytes(big.NewInt(int64(tickUpper)).Bytes(), 32)
	data = append(data, tickUpperBytes...)

	amount0Bytes := common.LeftPadBytes(amount0.Bytes(), 32)
	data = append(data, amount0Bytes...)

	amount1Bytes := common.LeftPadBytes(amount1.Bytes(), 32)
	data = append(data, amount1Bytes...)

	amount0MinBytes := common.LeftPadBytes(big.NewInt(0).Bytes(), 32)
	data = append(data, amount0MinBytes...)

	amount1MinBytes := common.LeftPadBytes(big.NewInt(0).Bytes(), 32)
	data = append(data, amount1MinBytes...)

	recipientBytes := common.LeftPadBytes(recipient.Bytes(), 32)
	data = append(data, recipientBytes...)

	deadlineBytes := common.LeftPadBytes(deadline.Bytes(), 32)
	data = append(data, deadlineBytes...)

	return e.sendTransaction(ctx, common.HexToAddress(e.cfg.Uniswap.PositionManager), big.NewInt(0), 800000, data)
}

func (e *Executor) ExecuteSwap(ctx context.Context, tokenIn, tokenOut common.Address, amountIn *big.Int, amountOutMin *big.Int, sqrtPriceLimitX96 *big.Int) (*ExecutionResult, error) {
	methodID := crypto.Keccak256([]byte("exactInputSingle((address,address,uint24,address,uint256,uint256,uint160))"))[:4]

	recipient := crypto.PubkeyToAddress(e.privateKey.PublicKey)
	deadline := big.NewInt(time.Now().Unix() + 300)

	data := []byte{}
	data = append(data, methodID...)

	tokenInBytes := common.LeftPadBytes(tokenIn.Bytes(), 32)
	data = append(data, tokenInBytes...)

	tokenOutBytes := common.LeftPadBytes(tokenOut.Bytes(), 32)
	data = append(data, tokenOutBytes...)

	feeBytes := common.LeftPadBytes(big.NewInt(int64(e.cfg.Uniswap.FeeTier)).Bytes(), 32)
	data = append(data, feeBytes...)

	recipientBytes := common.LeftPadBytes(recipient.Bytes(), 32)
	data = append(data, recipientBytes...)

	deadlineBytes := common.LeftPadBytes(deadline.Bytes(), 32)
	data = append(data, deadlineBytes...)

	amountInBytes := common.LeftPadBytes(amountIn.Bytes(), 32)
	data = append(data, amountInBytes...)

	amountOutMinBytes := common.LeftPadBytes(amountOutMin.Bytes(), 32)
	data = append(data, amountOutMinBytes...)

	sqrtPriceLimitX96Bytes := common.LeftPadBytes(sqrtPriceLimitX96.Bytes(), 32)
	data = append(data, sqrtPriceLimitX96Bytes...)

	return e.sendTransaction(ctx, common.HexToAddress(e.cfg.Uniswap.SwapRouter), big.NewInt(0), 300000, data)
}

func (e *Executor) sendTransaction(ctx context.Context, to common.Address, value *big.Int, gasLimit uint64, input []byte) (*ExecutionResult, error) {
	gasPrice, err := e.ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(int64(e.cfg.Execution.GasPriceMultiplier*100)))
	gasPrice = new(big.Int).Div(gasPrice, big.NewInt(100))

	nonce, err := e.ethClient.PendingNonceAt(ctx, crypto.PubkeyToAddress(e.privateKey.PublicKey))
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
	return big.NewInt(0), nil
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
