package executor

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"uniswap-bot/config"
	"uniswap-bot/pkg/contracts"
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
	chainID       *big.Int
	privateKey    *ecdsa.PrivateKey
	maxRetries    int
	walletAddress common.Address

	factory     *contracts.Uniswapv3Factory
	positionMgr *contracts.Uniswapv3NFTPositionManager
	swapRouter  *contracts.Uniswapv3RouterV2
	quoter      *contracts.Uniswapv3Quoter
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

	walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	factory, err := contracts.NewUniswapv3Factory(common.HexToAddress(cfg.Uniswap.FactoryAddress), client)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory: %w", err)
	}

	positionMgr, err := contracts.NewUniswapv3NFTPositionManager(common.HexToAddress(cfg.Uniswap.PositionManager), client)
	if err != nil {
		return nil, fmt.Errorf("failed to create position manager: %w", err)
	}

	swapRouter, err := contracts.NewUniswapv3RouterV2(common.HexToAddress(cfg.Uniswap.SwapRouter), client)
	if err != nil {
		return nil, fmt.Errorf("failed to create swap router: %w", err)
	}

	quoter, err := contracts.NewUniswapv3Quoter(common.HexToAddress(cfg.Uniswap.Quoter), client)
	if err != nil {
		return nil, fmt.Errorf("failed to create quoter: %w", err)
	}

	return &Executor{
		cfg:           cfg,
		ethClient:     client,
		chainID:       big.NewInt(cfg.Uniswap.ChainID),
		privateKey:    privateKey,
		maxRetries:    cfg.Execution.RetryTimes,
		walletAddress: walletAddress,
		factory:       factory,
		positionMgr:    positionMgr,
		swapRouter:    swapRouter,
		quoter:        quoter,
	}, nil
}

func (e *Executor) GetWalletAddress() common.Address {
	return e.walletAddress
}

func (e *Executor) GetTokenBalance(ctx context.Context, tokenAddr, ownerAddr common.Address) (*big.Int, error) {
	return GetTokenBalance(ctx, e.ethClient, tokenAddr, ownerAddr)
}

func (e *Executor) GetEthBalance(ctx context.Context, address common.Address) (*big.Int, error) {
	return e.ethClient.BalanceAt(ctx, address, nil)
}

func (e *Executor) QueryPoolAddress(ctx context.Context, token0, token1 common.Address, fee uint32) (common.Address, error) {
	return e.factory.GetPool(nil, token0, token1, big.NewInt(int64(fee)))
}

func (e *Executor) CreatePool(ctx context.Context, token0, token1 common.Address, fee uint32) (*ExecutionResult, error) {
	existingPool, err := e.QueryPoolAddress(ctx, token0, token1, fee)
	if err == nil && existingPool != (common.Address{}) {
		return &ExecutionResult{
			Success:     true,
			PoolAddress: existingPool.Hex(),
			Timestamp:   time.Now(),
		}, nil
	}

	auth, err := bind.NewKeyedTransactorWithChainID(e.privateKey, e.chainID)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	tx, err := e.factory.CreatePool(auth, token0, token1, big.NewInt(int64(fee)))
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	receipt, err := waitForReceipt(ctx, e.ethClient, tx)
	if err != nil {
		return &ExecutionResult{Success: false, TxHash: tx.Hash().Hex(), Error: err, Timestamp: time.Now()}, err
	}

	poolAddress, _ := e.factory.GetPool(nil, token0, token1, big.NewInt(int64(fee)))

	return &ExecutionResult{
		Success:     receipt.Status == types.ReceiptStatusSuccessful,
		TxHash:      tx.Hash().Hex(),
		GasUsed:     receipt.GasUsed,
		PoolAddress: poolAddress.Hex(),
		Timestamp:   time.Now(),
	}, nil
}

func (e *Executor) AddLiquidity(ctx context.Context, token0, token1 common.Address, fee uint32, amount0, amount1 *big.Int, tickLower, tickUpper int32) (*ExecutionResult, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(e.privateKey, e.chainID)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	params := contracts.INonfungiblePositionManagerMintParams{
		Token0:         token0,
		Token1:         token1,
		Fee:            big.NewInt(int64(fee)),
		TickLower:      big.NewInt(int64(tickLower)),
		TickUpper:      big.NewInt(int64(tickUpper)),
		Amount0Desired: amount0,
		Amount1Desired: amount1,
		Amount0Min:     big.NewInt(0),
		Amount1Min:     big.NewInt(0),
		Recipient:      e.walletAddress,
		Deadline:       big.NewInt(time.Now().Unix() + 300),
	}

	tx, err := e.positionMgr.Mint(auth, params)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	receipt, err := waitForReceipt(ctx, e.ethClient, tx)
	if err != nil {
		return &ExecutionResult{Success: false, TxHash: tx.Hash().Hex(), Error: err, Timestamp: time.Now()}, err
	}

	var tokenID *big.Int
	for _, rlog := range receipt.Logs {
		if len(rlog.Data) >= 96 {
			tokenID = new(big.Int).SetBytes(rlog.Data[0:32])
			break
		}
	}

	return &ExecutionResult{
		Success:   receipt.Status == types.ReceiptStatusSuccessful,
		TxHash:    tx.Hash().Hex(),
		GasUsed:   receipt.GasUsed,
		TokenID:   tokenID,
		Timestamp: time.Now(),
	}, nil
}

func (e *Executor) ExecuteSwap(ctx context.Context, tokenIn, tokenOut common.Address, amountIn *big.Int, amountOutMin *big.Int, sqrtPriceLimitX96 *big.Int) (*ExecutionResult, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(e.privateKey, e.chainID)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	params := contracts.IV3SwapRouterExactInputSingleParams{
		TokenIn:           tokenIn,
		TokenOut:          tokenOut,
		Fee:               big.NewInt(int64(e.cfg.Uniswap.FeeTier)),
		Recipient:         e.walletAddress,
		AmountIn:          amountIn,
		AmountOutMinimum:  amountOutMin,
		SqrtPriceLimitX96: sqrtPriceLimitX96,
	}

	tx, err := e.swapRouter.ExactInputSingle(auth, params)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err, Timestamp: time.Now()}, err
	}

	receipt, err := waitForReceipt(ctx, e.ethClient, tx)
	if err != nil {
		return &ExecutionResult{Success: false, TxHash: tx.Hash().Hex(), Error: err, Timestamp: time.Now()}, err
	}

	return &ExecutionResult{
		Success:   receipt.Status == types.ReceiptStatusSuccessful,
		TxHash:    tx.Hash().Hex(),
		GasUsed:   receipt.GasUsed,
		Timestamp: time.Now(),
	}, nil
}

func (e *Executor) QuoteSwap(ctx context.Context, tokenIn, tokenOut common.Address, amountIn *big.Int) (*big.Int, error) {
	result, err := e.quoter.QuoteExactInputSingle(nil, tokenIn, tokenOut, big.NewInt(int64(e.cfg.Uniswap.FeeTier)), amountIn, big.NewInt(0))
	if err != nil {
		return nil, err
	}

	receipt, err := waitForReceipt(ctx, e.ethClient, result)
	if err != nil {
		return nil, err
	}

	var amountOut *big.Int
	for _, rlog := range receipt.Logs {
		if len(rlog.Data) >= 32 {
			amountOut = new(big.Int).SetBytes(rlog.Data)
			break
		}
	}

	return amountOut, nil
}

func (e *Executor) GetPosition(ctx context.Context, tokenID *big.Int) (struct {
	Nonce                    *big.Int
	Operator                 common.Address
	Token0                   common.Address
	Token1                   common.Address
	Fee                      *big.Int
	TickLower                *big.Int
	TickUpper                *big.Int
	Liquidity                *big.Int
	FeeGrowthInside0LastX128 *big.Int
	FeeGrowthInside1LastX128 *big.Int
	TokensOwed0              *big.Int
	TokensOwed1              *big.Int
}, error) {
	return e.positionMgr.Positions(nil, tokenID)
}

func (e *Executor) GetFactory() *contracts.Uniswapv3Factory {
	return e.factory
}

func (e *Executor) GetPositionManager() *contracts.Uniswapv3NFTPositionManager {
	return e.positionMgr
}

func (e *Executor) GetSwapRouter() *contracts.Uniswapv3RouterV2 {
	return e.swapRouter
}

func (e *Executor) GetChainID() int64 {
	return e.cfg.Uniswap.ChainID
}

func (e *Executor) Close() {
	e.ethClient.Close()
}

func waitForReceipt(ctx context.Context, client *ethclient.Client, tx *types.Transaction) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			if err == ethereum.NotFound {
				time.Sleep(2 * time.Second)
				continue
			}
			return nil, err
		}
		return receipt, nil
	}
}

func PriceToTick(price float64) int32 {
	if price <= 0 {
		return 0
	}
	tick := 0
	return int32(tick)
}

func TickToPrice(tick int32) float64 {
	return 1.0
}

func uint24(v uint32) *big.Int {
	return big.NewInt(int64(v))
}

func CalculateTickRange(refPrice float64, rangeBps int) (int32, int32) {
	lower := refPrice * (1 - float64(rangeBps)/10000)
	upper := refPrice * (1 + float64(rangeBps)/10000)
	return PriceToTick(lower), PriceToTick(upper)
}
