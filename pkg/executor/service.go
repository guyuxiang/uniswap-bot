package executor

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"uniswap-bot/config"
)

type ExecutionResult struct {
	Success   bool
	TxHash    string
	Error     error
	GasUsed   uint64
	Timestamp time.Time
}

type Executor struct {
	cfg        *config.Config
	transactor *bind.TransactOpts
	chainID    *big.Int
	maxRetries int
}

func NewExecutor(cfg *config.Config, chainID int64) (*Executor, error) {
	privateKey, err := crypto.HexToECDSA(cfg.Bot.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	transactor, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(chainID))
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	transactor.GasLimit = cfg.Execution.GasLimit

	return &Executor{
		cfg:        cfg,
		transactor: transactor,
		chainID:    big.NewInt(chainID),
		maxRetries: cfg.Execution.RetryTimes,
	}, nil
}

func (e *Executor) ExecuteSwap(ctx context.Context, tokenIn, tokenOut common.Address, amountIn *big.Int, amountOutMin *big.Int) (*ExecutionResult, error) {
	var lastErr error

	for i := 0; i < e.maxRetries; i++ {
		tx, err := e.sendSwap(ctx, tokenIn, tokenOut, amountIn, amountOutMin)
		if err != nil {
			lastErr = err
			continue
		}

		receipt, err := e.waitForReceipt(ctx, tx.Hash())
		if err != nil {
			lastErr = err
			continue
		}

		if receipt.Status == types.ReceiptStatusSuccessful {
			return &ExecutionResult{
				Success:   true,
				TxHash:    tx.Hash().Hex(),
				GasUsed:   receipt.GasUsed,
				Timestamp: time.Now(),
			}, nil
		}

		lastErr = fmt.Errorf("transaction reverted")
	}

	return &ExecutionResult{
		Success:   false,
		TxHash:    "",
		Error:     lastErr,
		Timestamp: time.Now(),
	}, lastErr
}

func (e *Executor) sendSwap(ctx context.Context, tokenIn, tokenOut common.Address, amountIn, amountOutMin *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("swap not implemented - need router contract")
}

func (e *Executor) waitForReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	return nil, fmt.Errorf("waitForReceipt not implemented")
}

func (e *Executor) CalculateAmountOut(amountIn, reserveIn, reserveOut *big.Int, feeBps int) *big.Int {
	amountInWithFee := new(big.Int).Mul(amountIn, big.NewInt(int64(10000-feeBps)))
	amountInWithFee = new(big.Int).Div(amountInWithFee, big.NewInt(10000))

	numerator := new(big.Int).Mul(amountInWithFee, reserveOut)
	denominator := new(big.Int).Add(reserveIn, amountInWithFee)

	return new(big.Int).Div(numerator, denominator)
}

func (e *Executor) ValidateSlippage(amountOut, amountOutMin *big.Int) bool {
	return amountOut.Cmp(amountOutMin) >= 0
}

func (e *Executor) SetGasPrice(gasPrice *big.Int) {
	e.transactor.GasPrice = gasPrice
}

func (e *Executor) SetNonce(nonce uint64) {
	e.transactor.Nonce = big.NewInt(int64(nonce))
}
