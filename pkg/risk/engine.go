package risk

import (
	"fmt"
	"math"
	"math/big"
	"time"

	"uniswap-bot/config"
)

type RiskCheck struct {
	Allowed        bool
	Reason         string
	Severity       RiskSeverity
	ShouldStop     bool
	ShouldWithdraw bool
}

type RiskSeverity string

const (
	SeverityLow      RiskSeverity = "low"
	SeverityMedium   RiskSeverity = "medium"
	SeverityHigh     RiskSeverity = "high"
	SeverityCritical RiskSeverity = "critical"
)

type RiskEngine struct {
	cfg                 *config.Config
	dailyLoss           float64
	circuitBreakerStart time.Time
	isCircuitBreaker    bool
	totalVolume         float64
	failedTrades        int
	totalTrades         int
}

func NewRiskEngine(cfg *config.Config) *RiskEngine {
	return &RiskEngine{
		cfg:              cfg,
		dailyLoss:        0,
		isCircuitBreaker: false,
		totalVolume:      0,
		failedTrades:     0,
		totalTrades:      0,
	}
}

func (e *RiskEngine) CheckRisk(currentPrice, twapPrice *big.Float, refPrice float64) *RiskCheck {
	current, _ := currentPrice.Float64()
	twap, _ := twapPrice.Float64()

	deviation := math.Abs(current - refPrice)
	twapDeviation := math.Abs(twap - refPrice)

	circuitBreakerThreshold := float64(e.cfg.Risk.CircuitBreakerDeviationBps) / 10000

	if deviation > circuitBreakerThreshold {
		e.triggerCircuitBreaker()
		return &RiskCheck{
			Allowed:        false,
			Reason:         fmt.Sprintf("price deviation %.4f%% exceeds circuit breaker threshold", deviation*100),
			Severity:       SeverityCritical,
			ShouldStop:     true,
			ShouldWithdraw: true,
		}
	}

	if e.isCircuitBreaker {
		elapsed := time.Since(e.circuitBreakerStart)
		if elapsed < time.Duration(e.cfg.Risk.CircuitBreakerDurationMin)*time.Minute {
			return &RiskCheck{
				Allowed:        false,
				Reason:         "circuit breaker is active",
				Severity:       SeverityHigh,
				ShouldStop:     true,
				ShouldWithdraw: true,
			}
		}
		e.resetCircuitBreaker()
	}

	maxDailyLoss := float64(e.cfg.Risk.MaxDailyLossBps) / 10000
	if e.dailyLoss > maxDailyLoss {
		return &RiskCheck{
			Allowed:        false,
			Reason:         fmt.Sprintf("daily loss %.4f%% exceeds max daily loss", e.dailyLoss*100),
			Severity:       SeverityHigh,
			ShouldStop:     true,
			ShouldWithdraw: false,
		}
	}

	twapThreshold := circuitBreakerThreshold / 2
	if twapDeviation > twapThreshold {
		return &RiskCheck{
			Allowed:        false,
			Reason:         fmt.Sprintf("TWAP deviation %.4f%% is too high", twapDeviation*100),
			Severity:       SeverityMedium,
			ShouldStop:     false,
			ShouldWithdraw: false,
		}
	}

	return &RiskCheck{
		Allowed:  true,
		Reason:   "risk check passed",
		Severity: SeverityLow,
	}
}

func (e *RiskEngine) CheckTradeSize(amount0, amount1 *big.Float) *RiskCheck {
	tradeValue0, _ := amount0.Float64()
	tradeValue1, _ := amount1.Float64()
	totalValue := tradeValue0 + tradeValue1

	maxTradeBps := float64(e.cfg.Risk.MaxSingleTradeBps) / 10000

	if totalValue > maxTradeBps {
		return &RiskCheck{
			Allowed:        false,
			Reason:         fmt.Sprintf("trade size %.4f%% exceeds max single trade %d bps", totalValue*100, e.cfg.Risk.MaxSingleTradeBps),
			Severity:       SeverityMedium,
			ShouldStop:     false,
			ShouldWithdraw: false,
		}
	}

	return &RiskCheck{
		Allowed:  true,
		Reason:   "trade size check passed",
		Severity: SeverityLow,
	}
}

func (e *RiskEngine) RecordTrade(success bool) {
	e.totalTrades++
	if !success {
		e.failedTrades++
	}
}

func (e *RiskEngine) RecordLoss(amount float64) {
	e.dailyLoss += amount
}

func (e *RiskEngine) GetFailureRate() float64 {
	if e.totalTrades == 0 {
		return 0
	}
	return float64(e.failedTrades) / float64(e.totalTrades)
}

func (e *RiskEngine) triggerCircuitBreaker() {
	e.isCircuitBreaker = true
	e.circuitBreakerStart = time.Now()
}

func (e *RiskEngine) resetCircuitBreaker() {
	e.isCircuitBreaker = false
	e.circuitBreakerStart = time.Time{}
}

func (e *RiskEngine) ResetDailyStats() {
	e.dailyLoss = 0
	e.totalTrades = 0
	e.failedTrades = 0
}

func (e *RiskEngine) IsCircuitBreakerActive() bool {
	return e.isCircuitBreaker
}

func (e *RiskEngine) GetDailyLoss() float64 {
	return e.dailyLoss
}
