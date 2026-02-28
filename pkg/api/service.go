package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"uniswap-bot/config"
	"uniswap-bot/pkg/monitor"
	"uniswap-bot/pkg/position"
	"uniswap-bot/pkg/rebalancer"
	"uniswap-bot/pkg/risk"
)

type Server struct {
	cfg         *config.Config
	router      *gin.Engine
	positionSvc position.PositionService
	riskEngine  *risk.RiskEngine
	rebalancer  *rebalancer.Rebalancer
	monitor     *monitor.Monitor
}

func NewServer(cfg *config.Config, positionSvc position.PositionService, riskEngine *risk.RiskEngine, rebalancer *rebalancer.Rebalancer, monitor *monitor.Monitor) *Server {
	server := &Server{
		cfg:         cfg,
		router:      gin.Default(),
		positionSvc: positionSvc,
		riskEngine:  riskEngine,
		rebalancer:  rebalancer,
		monitor:     monitor,
	}

	server.setupRoutes()
	return server
}

func (s *Server) setupRoutes() {
	s.router.GET("/health", s.handleHealth)

	api := s.router.Group("/api/v1")
	{
		api.GET("/status", s.handleStatus)
		api.GET("/metrics", s.handleMetrics)
		api.GET("/positions", s.handlePositions)
		api.GET("/risk", s.handleRisk)
		api.POST("/rebalance", s.handleRebalance)
		api.POST("/start", s.handleStart)
		api.POST("/stop", s.handleStop)
		api.GET("/alerts", s.handleAlerts)
	}
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func (s *Server) handleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"running":         s.rebalancer.IsRunning(),
		"circuit_breaker": s.riskEngine.IsCircuitBreakerActive(),
		"total_ratio":     s.positionSvc.GetTotalRatio(),
	})
}

func (s *Server) handleMetrics(c *gin.Context) {
	metrics := s.monitor.GetMetrics()
	c.JSON(http.StatusOK, gin.H{
		"current_price":   metrics.CurrentPrice,
		"twap_price":      metrics.TwapPrice,
		"ref_price":       metrics.RefPrice,
		"deviation":       metrics.Deviation,
		"total_liquidity": metrics.TotalLiquidity,
		"fees_collected":  metrics.FeesCollected,
		"gas_cost":        metrics.GasCost,
		"net_profit":      metrics.NetProfit,
		"position_count":  metrics.PositionCount,
		"failure_rate":    metrics.FailureRate,
		"last_rebalance":  metrics.LastRebalance,
		"status":          metrics.Status,
	})
}

func (s *Server) handlePositions(c *gin.Context) {
	layers := s.positionSvc.GetLayers()
	c.JSON(http.StatusOK, gin.H{
		"layers": layers,
		"total":  len(layers),
	})
}

func (s *Server) handleRisk(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"circuit_breaker_active": s.riskEngine.IsCircuitBreakerActive(),
		"daily_loss":             s.riskEngine.GetDailyLoss(),
		"failure_rate":           s.riskEngine.GetFailureRate(),
	})
}

func (s *Server) handleRebalance(c *gin.Context) {
	if !s.rebalancer.IsRunning() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rebalancer is not running"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "rebalance triggered",
	})
}

func (s *Server) handleStart(c *gin.Context) {
	s.rebalancer.Start()
	s.monitor.UpdateStatus("running")
	c.JSON(http.StatusOK, gin.H{
		"message": "rebalancer started",
	})
}

func (s *Server) handleStop(c *gin.Context) {
	s.rebalancer.Stop()
	s.monitor.UpdateStatus("stopped")
	c.JSON(http.StatusOK, gin.H{
		"message": "rebalancer stopped",
	})
}

func (s *Server) handleAlerts(c *gin.Context) {
	alerts := s.monitor.CheckAlerts()
	c.JSON(http.StatusOK, gin.H{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) GetRouter() *gin.Engine {
	return s.router
}
