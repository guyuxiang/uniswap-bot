package monitor

import (
	"fmt"
	"sync"
	"time"

	"uniswap-bot/pkg/position"
	"uniswap-bot/pkg/rebalancer"
	"uniswap-bot/pkg/risk"
)

type Metrics struct {
	mu             sync.RWMutex
	CurrentPrice   float64
	TwapPrice      float64
	RefPrice       float64
	Deviation      float64
	TotalLiquidity float64
	FeesCollected  float64
	GasCost        float64
	NetProfit      float64
	PositionCount  int
	FailureRate    float64
	LastRebalance  time.Time
	Status         string
}

type Monitor struct {
	metrics     *Metrics
	positionSvc position.PositionService
	riskEngine  *risk.RiskEngine
	rebalancer  *rebalancer.Rebalancer
}

func NewMonitor(positionSvc position.PositionService, riskEngine *risk.RiskEngine, rebalancer *rebalancer.Rebalancer) *Monitor {
	return &Monitor{
		metrics:     &Metrics{},
		positionSvc: positionSvc,
		riskEngine:  riskEngine,
		rebalancer:  rebalancer,
	}
}

func (m *Monitor) UpdatePrices(current, twap, ref float64) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.CurrentPrice = current
	m.metrics.TwapPrice = twap
	m.metrics.RefPrice = ref
	m.metrics.Deviation = calculateDeviation(current, ref)
}

func (m *Monitor) UpdateLiquidity(liquidity float64) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.TotalLiquidity = liquidity
}

func (m *Monitor) UpdateFees(fees float64) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.FeesCollected += fees
}

func (m *Monitor) UpdateGasCost(cost float64) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.GasCost += cost
	m.metrics.NetProfit = m.metrics.FeesCollected - m.metrics.GasCost
}

func (m *Monitor) UpdatePositionCount(count int) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.PositionCount = count
}

func (m *Monitor) UpdateFailureRate(rate float64) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.FailureRate = rate
}

func (m *Monitor) UpdateStatus(status string) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.Status = status
}

func (m *Monitor) UpdateLastRebalance(t time.Time) {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	m.metrics.LastRebalance = t
}

func (m *Monitor) GetMetrics() *Metrics {
	m.metrics.mu.RLock()
	defer m.metrics.mu.RUnlock()

	metricsCopy := *m.metrics
	return &metricsCopy
}

func (m *Monitor) CheckAlerts() []Alert {
	m.metrics.mu.RLock()
	defer m.metrics.mu.Unlock()

	var alerts []Alert

	if m.metrics.Deviation > 0.01 {
		alerts = append(alerts, Alert{
			Level:   "warning",
			Message: fmt.Sprintf("Price deviation %.4f%% is high", m.metrics.Deviation*100),
			Time:    time.Now(),
		})
	}

	if m.metrics.Deviation > 0.03 {
		alerts = append(alerts, Alert{
			Level:   "critical",
			Message: fmt.Sprintf("Price deviation %.4f%% exceeds circuit breaker", m.metrics.Deviation*100),
			Time:    time.Now(),
		})
	}

	if m.metrics.FailureRate > 0.1 {
		alerts = append(alerts, Alert{
			Level:   "warning",
			Message: fmt.Sprintf("Failure rate %.2f%% is high", m.metrics.FailureRate*100),
			Time:    time.Now(),
		})
	}

	if m.riskEngine != nil && m.riskEngine.IsCircuitBreakerActive() {
		alerts = append(alerts, Alert{
			Level:   "critical",
			Message: "Circuit breaker is active!",
			Time:    time.Now(),
		})
	}

	return alerts
}

type Alert struct {
	Level   string
	Message string
	Time    time.Time
}

func calculateDeviation(price, ref float64) float64 {
	diff := price - ref
	if diff < 0 {
		diff = -diff
	}
	return diff / ref
}

func (m *Monitor) StartPeriodicTasks(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			m.snapshot()
		}
	}()
}

func (m *Monitor) snapshot() {
	if m.rebalancer != nil {
		m.metrics.mu.Lock()
		m.metrics.LastRebalance = m.rebalancer.GetLastRebalanceTime()
		m.metrics.Status = "running"
		if !m.rebalancer.IsRunning() {
			m.metrics.Status = "stopped"
		}
		m.metrics.mu.Unlock()
	}
}
