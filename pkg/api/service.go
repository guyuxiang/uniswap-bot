package api

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	"uniswap-bot/config"
	"uniswap-bot/pkg/executor"
	"uniswap-bot/pkg/monitor"
	"uniswap-bot/pkg/position"
	"uniswap-bot/pkg/rebalancer"
	"uniswap-bot/pkg/risk"
)

type Server struct {
	cfg         *config.Config
	router      *gin.Engine
	positionSvc *position.PositionService
	riskEngine  *risk.RiskEngine
	rebalancer  *rebalancer.Rebalancer
	monitor     *monitor.Monitor
	executor    *executor.Executor
	mu          sync.RWMutex
}

func NewServer(cfg *config.Config, positionSvc *position.PositionService, riskEngine *risk.RiskEngine, rebalancer *rebalancer.Rebalancer, monitor *monitor.Monitor) *Server {
	exec, err := executor.NewExecutor(cfg)
	if err != nil {
		fmt.Printf("Warning: Failed to create executor: %v\n", err)
	}

	server := &Server{
		cfg:         cfg,
		router:      gin.Default(),
		positionSvc: positionSvc,
		riskEngine:  riskEngine,
		rebalancer:  rebalancer,
		monitor:     monitor,
		executor:    exec,
	}

	server.setupRoutes()
	return server
}

func (s *Server) setupRoutes() {
	s.router.GET("/", func(c *gin.Context) {
		c.File("./web/index.html")
	})
	s.router.GET("/index.html", func(c *gin.Context) {
		c.File("./web/index.html")
	})
	s.router.GET("/static/*filepath", func(c *gin.Context) {
		c.File("./web/" + c.Param("filepath"))
	})
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

		api.POST("/create-pool", s.handleCreatePool)
		api.POST("/add-liquidity", s.handleAddLiquidity)
		api.POST("/swap", s.handleSwap)
		api.GET("/balance", s.handleBalance)
	}
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().Unix(),
	})
}

func (s *Server) handleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"running":         s.rebalancer.IsRunning(),
		"circuit_breaker": s.riskEngine.IsCircuitBreakerActive(),
		"total_ratio":     s.positionSvc.GetTotalRatio(),
		"pool_address":    s.cfg.Uniswap.PoolAddress,
		"token0":          s.cfg.Uniswap.Token0Address,
		"token1":          s.cfg.Uniswap.Token1Address,
		"fee_tier":        s.cfg.Uniswap.FeeTier,
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

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		s.rebalancer.ExecuteRebalance(ctx)
	}()

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

type CreatePoolRequest struct {
	Token0 string `json:"token0" binding:"required"`
	Token1 string `json:"token1" binding:"required"`
	Fee    uint32 `json:"fee"`
}

func (s *Server) handleCreatePool(c *gin.Context) {
	if s.executor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "executor not initialized"})
		return
	}

	var req CreatePoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token0 := common.HexToAddress(req.Token0)
	token1 := common.HexToAddress(req.Token1)
	fee := req.Fee
	if fee == 0 {
		fee = uint32(s.cfg.Uniswap.FeeTier)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := s.executor.CreatePool(ctx, token0, token1, fee)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !result.Success {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":  false,
			"tx_hash":  result.TxHash,
			"gas_used": result.GasUsed,
			"error":    result.Error.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"tx_hash":      result.TxHash,
		"pool_address": result.PoolAddress,
		"gas_used":     result.GasUsed,
	})
}

type AddLiquidityRequest struct {
	Token0    string  `json:"token0" binding:"required"`
	Token1    string  `json:"token1" binding:"required"`
	Fee       uint32  `json:"fee"`
	Amount0   string  `json:"amount0" binding:"required"`
	Amount1   string  `json:"amount1" binding:"required"`
	TickLower int32   `json:"tick_lower"`
	TickUpper int32   `json:"tick_upper"`
	RangeBps  int     `json:"range_bps"`
}

func (s *Server) handleAddLiquidity(c *gin.Context) {
	if s.executor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "executor not initialized"})
		return
	}

	var req AddLiquidityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token0 := common.HexToAddress(req.Token0)
	token1 := common.HexToAddress(req.Token1)
	fee := req.Fee
	if fee == 0 {
		fee = uint32(s.cfg.Uniswap.FeeTier)
	}

	amount0, ok := new(big.Int).SetString(req.Amount0, 10)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount0"})
		return
	}
	amount1, ok := new(big.Int).SetString(req.Amount1, 10)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount1"})
		return
	}

	tickLower := req.TickLower
	tickUpper := req.TickUpper
	if tickLower == 0 && tickUpper == 0 {
		tickLower, tickUpper = executor.CalculateTickRange(s.cfg.Oracle.RefPrice, s.cfg.Bot.CoreRangeBps)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := s.executor.AddLiquidity(ctx, token0, token1, fee, amount0, amount1, tickLower, tickUpper)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !result.Success {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":  false,
			"tx_hash":  result.TxHash,
			"gas_used": result.GasUsed,
			"error":    result.Error.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"tx_hash":    result.TxHash,
		"token_id":   result.TokenID.String(),
		"amount0":    result.Amount0.String(),
		"amount1":    result.Amount1.String(),
		"gas_used":   result.GasUsed,
		"tick_lower": tickLower,
		"tick_upper": tickUpper,
	})
}

type SwapRequest struct {
	TokenIn      string `json:"token_in" binding:"required"`
	TokenOut     string `json:"token_out" binding:"required"`
	AmountIn     string `json:"amount_in" binding:"required"`
	AmountOutMin string `json:"amount_out_min"`
	Fee          uint32 `json:"fee"`
}

func (s *Server) handleSwap(c *gin.Context) {
	if s.executor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "executor not initialized"})
		return
	}

	var req SwapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokenIn := common.HexToAddress(req.TokenIn)
	tokenOut := common.HexToAddress(req.TokenOut)

	amountIn, ok := new(big.Int).SetString(req.AmountIn, 10)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount_in"})
		return
	}

	amountOutMin := big.NewInt(0)
	if req.AmountOutMin != "" {
		amountOutMin, ok = new(big.Int).SetString(req.AmountOutMin, 10)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount_out_min"})
			return
		}
	} else {
		amountOutMin = new(big.Int).Mul(amountIn, big.NewInt(int64(10000-s.cfg.Execution.MaxSlippageBps)))
		amountOutMin = new(big.Int).Div(amountOutMin, big.NewInt(10000))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := s.executor.ExecuteSwap(ctx, tokenIn, tokenOut, amountIn, amountOutMin, big.NewInt(0))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !result.Success {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":  false,
			"tx_hash":  result.TxHash,
			"gas_used": result.GasUsed,
			"error":    result.Error.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"tx_hash":   result.TxHash,
		"amount_in":  amountIn.String(),
		"amount_out": result.Amount0.String(),
		"gas_used":  result.GasUsed,
	})
}

func (s *Server) handleBalance(c *gin.Context) {
	if s.executor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "executor not initialized"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token0Addr := common.HexToAddress(s.cfg.Uniswap.Token0Address)
	token1Addr := common.HexToAddress(s.cfg.Uniswap.Token1Address)

	token0Balance, err := s.executor.GetTokenBalance(ctx, token0Addr, common.HexToAddress(s.cfg.Uniswap.PositionManager))
	if err != nil {
		token0Balance = big.NewInt(0)
	}

	token1Balance, err := s.executor.GetTokenBalance(ctx, token1Addr, common.HexToAddress(s.cfg.Uniswap.PositionManager))
	if err != nil {
		token1Balance = big.NewInt(0)
	}

	c.JSON(http.StatusOK, gin.H{
		"token0_balance": token0Balance.String(),
		"token1_balance": token1Balance.String(),
		"token0_address": token0Addr.Hex(),
		"token1_address": token1Addr.Hex(),
	})
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) GetRouter() *gin.Engine {
	return s.router
}
