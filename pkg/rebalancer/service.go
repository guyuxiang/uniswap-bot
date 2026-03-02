package rebalancer

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"uniswap-bot/config"
	"uniswap-bot/pkg/executor"
	"uniswap-bot/pkg/position"
	"uniswap-bot/pkg/risk"
	"uniswap-bot/pkg/uniswap"
)

type Rebalancer struct {
	cfg             *config.Config
	positionService *position.PositionService
	riskEngine      *risk.RiskEngine
	executor        *executor.Executor
	uniswapClient  *uniswap.Client
	currentPrice    *big.Float
	twapPrice       *big.Float
	refPrice        *big.Float
	lastRebalance   time.Time
	lastStabilize   time.Time
	isRunning       bool
}

func NewRebalancer(cfg *config.Config, positionService *position.PositionService, riskEngine *risk.RiskEngine, exec *executor.Executor) *Rebalancer {
	refPrice := big.NewFloat(cfg.Oracle.RefPrice)
	return &Rebalancer{
		cfg:             cfg,
		positionService: positionService,
		riskEngine:      riskEngine,
		executor:        exec,
		currentPrice:    big.NewFloat(1.0),
		twapPrice:       big.NewFloat(1.0),
		refPrice:        refPrice,
		lastRebalance:   time.Now(),
		lastStabilize:   time.Now(),
		isRunning:       false,
	}
}

type PriceInfo struct {
	CurrentPrice *big.Float
	TwapPrice    *big.Float
	RefPrice     *big.Float
}

func (r *Rebalancer) UpdatePrices(info PriceInfo) {
	r.currentPrice = info.CurrentPrice
	r.twapPrice = info.TwapPrice
}

func (r *Rebalancer) ShouldRebalance() (bool, string) {
	if !r.isRunning {
		return false, "rebalancer is not running"
	}

	timeSinceRebalance := time.Since(r.lastRebalance)
	if timeSinceRebalance < time.Duration(r.cfg.Bot.RebalanceIntervalSec)*time.Second {
		return false, "rebalance interval not reached"
	}

	deviation := r.calculateDeviation()
	if deviation > r.cfg.Bot.RebalanceThreshold {
		return true, fmt.Sprintf("price deviation %.4f%% exceeds threshold %.4f%%", deviation*100, r.cfg.Bot.RebalanceThreshold*100)
	}

	riskCheck := r.riskEngine.CheckRisk(r.currentPrice, r.twapPrice, r.cfg.Oracle.RefPrice)
	if !riskCheck.Allowed {
		return false, fmt.Sprintf("risk check failed: %s", riskCheck.Reason)
	}

	return true, "normal rebalance"
}

func (r *Rebalancer) calculateDeviation() float64 {
	diff := new(big.Float).Sub(r.currentPrice, r.twapPrice)
	absDiff := new(big.Float).Abs(diff)
	deviation := new(big.Float).Quo(absDiff, r.twapPrice)
	dev, _ := deviation.Float64()
	return dev
}

func (r *Rebalancer) CalculateRanges() (coreLower, coreUpper, midLower, midUpper, tailLower, tailUpper int32) {
	refPrice := r.cfg.Oracle.RefPrice
	coreBps := r.cfg.Bot.CoreRangeBps
	midBps := r.cfg.Bot.MidRangeBps
	tailBps := r.cfg.Bot.TailRangeBps

	coreLower = uniswap.PriceToTick(refPrice * (1 - float64(coreBps)/10000))
	coreUpper = uniswap.PriceToTick(refPrice * (1 + float64(coreBps)/10000))
	midLower = uniswap.PriceToTick(refPrice * (1 - float64(midBps)/10000))
	midUpper = uniswap.PriceToTick(refPrice * (1 + float64(midBps)/10000))
	tailLower = uniswap.PriceToTick(refPrice * (1 - float64(tailBps)/10000))
	tailUpper = uniswap.PriceToTick(refPrice * (1 + float64(tailBps)/10000))

	return
}

func (r *Rebalancer) ExecuteRebalance(ctx context.Context) error {
	should, reason := r.ShouldRebalance()
	if !should {
		return fmt.Errorf("rebalance not needed: %s", reason)
	}

	// Only execute stabilization (arbitrage)
	if r.cfg.Stabilization.Enabled {
		stabilizeErr := r.ExecuteStabilization(ctx)
		if stabilizeErr != nil {
			log.Printf("Stabilization failed: %v", stabilizeErr)
			return stabilizeErr
		}
	}

	r.lastRebalance = time.Now()

	return nil
}

func (r *Rebalancer) ShouldStabilize() (bool, string) {
	if !r.cfg.Stabilization.Enabled {
		return false, "stabilization disabled"
	}

	if r.executor == nil {
		return false, "executor not available"
	}

	timeSinceStabilize := time.Since(r.lastStabilize)
	if timeSinceStabilize < time.Duration(r.cfg.Stabilization.CooldownSeconds)*time.Second {
		return false, "stabilization cooldown not reached"
	}

	deviationBps := int(r.calculateDeviation() * 10000)
	if deviationBps < r.cfg.Stabilization.DeviationBps {
		return false, fmt.Sprintf("deviation %d bps below threshold %d bps", deviationBps, r.cfg.Stabilization.DeviationBps)
	}

	return true, fmt.Sprintf("deviation %d bps exceeds threshold %d bps", deviationBps, r.cfg.Stabilization.DeviationBps)
}

func (r *Rebalancer) ExecuteStabilization(ctx context.Context) error {
	should, reason := r.ShouldStabilize()
	if !should {
		return fmt.Errorf("stabilization not needed: %s", reason)
	}

	token0Addr := common.HexToAddress(r.cfg.Uniswap.Token0Address)
	token1Addr := common.HexToAddress(r.cfg.Uniswap.Token1Address)

	walletAddr := r.executor.GetWalletAddress()
	token0Balance, err := r.executor.GetTokenBalance(ctx, token0Addr, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to get token0 balance: %w", err)
	}
	token1Balance, err := r.executor.GetTokenBalance(ctx, token1Addr, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to get token1 balance: %w", err)
	}

	priceVal, _ := r.currentPrice.Float64()
	minSwap := r.cfg.Stabilization.MinSwapAmount
	maxSwap := r.cfg.Stabilization.MaxSwapAmount
	swapBps := r.cfg.Stabilization.SwapAmountBps

	var amountIn *big.Int
	var tokenIn, tokenOut common.Address

	if priceVal > 1.0 {
		tokenIn = token0Addr
		tokenOut = token1Addr
		available := float64(token0Balance.Int64()) / 1e6
		swapVal := available * float64(swapBps) / 10000
		if swapVal < minSwap {
			swapVal = minSwap
		}
		if swapVal > maxSwap {
			swapVal = maxSwap
		}
		if swapVal > available {
			swapVal = available
		}
		amountIn = big.NewInt(int64(swapVal * 1e6))
		log.Printf("=== Stabilization: USDx > 1, selling USDx for USDT ===")
		log.Printf("Price: %.6f, Selling USDx: %.2f", priceVal, swapVal)
	} else {
		tokenIn = token1Addr
		tokenOut = token0Addr
		available := float64(token1Balance.Int64()) / 1e6
		swapVal := available * float64(swapBps) / 10000
		if swapVal < minSwap {
			swapVal = minSwap
		}
		if swapVal > maxSwap {
			swapVal = maxSwap
		}
		if swapVal > available {
			swapVal = available
		}
		amountIn = big.NewInt(int64(swapVal * 1e6))
		log.Printf("=== Stabilization: USDx < 1, buying USDx with USDT ===")
		log.Printf("Price: %.6f, Buying USDx: %.2f", priceVal, swapVal)
	}

	if amountIn.Cmp(big.NewInt(0)) <= 0 {
		return fmt.Errorf("swap amount is zero or negative")
	}

	amountOutMin := big.NewInt(0)
	slippageBps := int64(r.cfg.Execution.MaxSlippageBps)
	amountOutMin = new(big.Int).Mul(amountIn, big.NewInt(10000-slippageBps))
	amountOutMin = new(big.Int).Div(amountOutMin, big.NewInt(10000))

	result, err := r.executor.ExecuteSwap(ctx, tokenIn, tokenOut, amountIn, amountOutMin, big.NewInt(0))
	if err != nil {
		return fmt.Errorf("stabilization swap failed: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("stabilization tx failed: %s", result.TxHash)
	}

	log.Printf("Stabilization successful! Tx: %s, AmountIn: %s, AmountOut: %s", 
		result.TxHash, amountIn.String(), result.Amount0.String())

	r.lastStabilize = time.Now()
	return nil
}

func (r *Rebalancer) Start() {
	r.isRunning = true
}

func (r *Rebalancer) Stop() {
	r.isRunning = false
}

func (r *Rebalancer) IsRunning() bool {
	return r.isRunning
}

func (r *Rebalancer) GetLastRebalanceTime() time.Time {
	return r.lastRebalance
}
