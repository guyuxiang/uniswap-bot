package rebalancer

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"uniswap-bot/config"
	"uniswap-bot/pkg/position"
	"uniswap-bot/pkg/risk"
	"uniswap-bot/pkg/uniswap"
)

type Rebalancer struct {
	cfg             *config.Config
	positionService position.PositionService
	riskEngine      *risk.RiskEngine
	currentPrice    *big.Float
	twapPrice       *big.Float
	lastRebalance   time.Time
	isRunning       bool
}

func NewRebalancer(cfg *config.Config, positionService position.PositionService, riskEngine *risk.RiskEngine) *Rebalancer {
	return &Rebalancer{
		cfg:             cfg,
		positionService: positionService,
		riskEngine:      riskEngine,
		currentPrice:    big.NewFloat(1.0),
		twapPrice:       big.NewFloat(1.0),
		lastRebalance:   time.Now(),
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

	coreLower, coreUpper, midLower, midUpper, tailLower, tailUpper := r.CalculateRanges()

	r.positionService.AddLayer("core", r.cfg.Bot.CoreRatio, coreLower, coreUpper, big.NewInt(0))
	r.positionService.AddLayer("mid", r.cfg.Bot.MidRatio, midLower, midUpper, big.NewInt(0))
	r.positionService.AddLayer("tail", r.cfg.Bot.TailRatio, tailLower, tailUpper, big.NewInt(0))

	r.lastRebalance = time.Now()

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
