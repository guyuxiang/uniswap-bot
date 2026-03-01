package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"uniswap-bot/config"
	"uniswap-bot/pkg/api"
	"uniswap-bot/pkg/monitor"
	"uniswap-bot/pkg/oracle"
	"uniswap-bot/pkg/position"
	"uniswap-bot/pkg/rebalancer"
	"uniswap-bot/pkg/risk"
	"uniswap-bot/pkg/uniswap"
)

const (
	CreatePoolAction   = "create-pool"
	AddLiquidityAction = "add-liquidity"
	StartBotAction     = "start"
)

var (
	cfg             *config.Config
	uniswapClient   *uniswap.Client
	positionService *position.PositionService
	riskEngine      *risk.RiskEngine
	priceOracle     *oracle.PriceOracle
	rebalancerSvc   *rebalancer.Rebalancer
	monitorSvc      *monitor.Monitor
	apiServer       *api.Server
	ctx             context.Context
	cancel          context.CancelFunc
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	action := os.Args[1]
	cfgPath := "config.yaml"
	if len(os.Args) > 2 {
		cfgPath = os.Args[2]
	}

	var err error
	cfg, err = config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	switch action {
	case CreatePoolAction:
		handleCreatePool()
	case AddLiquidityAction:
		handleAddLiquidity()
	case StartBotAction:
		handleStartBot()
	default:
		fmt.Printf("Unknown action: %s\n", action)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: uniswap-bot <command> [config_file]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  create-pool     Create a new Uniswap V3 pool")
	fmt.Println("  add-liquidity  Add liquidity to the pool")
	fmt.Println("  start          Start the market making bot")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  uniswap-bot create-pool config.yaml")
	fmt.Println("  uniswap-bot add-liquidity config.yaml")
	fmt.Println("  uniswap-bot start config.yaml")
}

func handleCreatePool() {
	client, err := ethclient.Dial(cfg.Uniswap.RPCURL)
	if err != nil {
		log.Fatalf("Failed to connect to RPC: %v", err)
	}
	defer client.Close()

	chainID := big.NewInt(cfg.Uniswap.ChainID)
	privateKey, err := crypto.HexToECDSA(cfg.Bot.PrivateKey)
	if err != nil {
		log.Fatalf("Invalid private key: %v", err)
	}

	token0 := common.HexToAddress(cfg.Uniswap.Token0Address)
	token1 := common.HexToAddress(cfg.Uniswap.Token1Address)
	fee := uint32(cfg.Uniswap.FeeTier)
	factoryAddr := common.HexToAddress(cfg.Uniswap.FactoryAddress)

	fmt.Printf("=============================================\n")
	fmt.Printf("  Create GLUSD/USDT Pool on Unichain Sepolia\n")
	fmt.Printf("=============================================\n\n")
	fmt.Printf("Token0 (GLUSD): %s\n", token0.Hex())
	fmt.Printf("Token1 (USDT):  %s\n", token1.Hex())
	fmt.Printf("Fee Tier:       %d (0.05%%)\n", fee)
	fmt.Printf("Factory:        %s\n", factoryAddr.Hex())
	fmt.Println()

	methodID := crypto.Keccak256([]byte("createPool(address,address,uint24)"))[:4]
	
	token0Bytes := common.LeftPadBytes(token0.Bytes(), 32)
	token1Bytes := common.LeftPadBytes(token1.Bytes(), 32)
	feeBytes := common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32)
	
	input := append(methodID, append(token0Bytes, append(token1Bytes, feeBytes...)...)...)

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("Failed to suggest gas price: %v", err)
	}

	nonce, err := client.PendingNonceAt(ctx, crypto.PubkeyToAddress(privateKey.PublicKey))
	if err != nil {
		log.Fatalf("Failed to get nonce: %v", err)
	}

	tx := types.NewTransaction(nonce, factoryAddr, big.NewInt(0), 500000, gasPrice, input)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatalf("Failed to sign transaction: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}

	fmt.Printf("Transaction sent: %s\n", signedTx.Hash().Hex())
	fmt.Println("Waiting for confirmation...")

	receipt, err := bind.WaitMined(ctx, client, signedTx)
	if err != nil {
		log.Fatalf("Failed to wait for receipt: %v", err)
	}

	if receipt.Status == 0 {
		log.Fatalf("Transaction failed! Gas used: %d", receipt.GasUsed)
	}

	fmt.Printf("\nPool created successfully!\n")
	fmt.Printf("Gas used: %d\n\n", receipt.GasUsed)

	for _, rlog := range receipt.Logs {
		if len(rlog.Data) >= 32 {
			poolAddr := common.BytesToAddress(rlog.Data[12:32])
			fmt.Printf("*** Pool Address: %s ***\n", poolAddr.Hex())
		}
	}

	fmt.Println("\nAdd this address to config.yaml as pool_address")
}

func handleAddLiquidity() {
	client, err := ethclient.Dial(cfg.Uniswap.RPCURL)
	if err != nil {
		log.Fatalf("Failed to connect to RPC: %v", err)
	}
	defer client.Close()

	chainID := big.NewInt(cfg.Uniswap.ChainID)
	privateKey, err := crypto.HexToECDSA(cfg.Bot.PrivateKey)
	if err != nil {
		log.Fatalf("Invalid private key: %v", err)
	}

	token0 := common.HexToAddress(cfg.Uniswap.Token0Address)
	token1 := common.HexToAddress(cfg.Uniswap.Token1Address)
	posMgrAddr := common.HexToAddress(cfg.Uniswap.PositionManager)
	fee := uint32(cfg.Uniswap.FeeTier)

	amount0 := big.NewInt(0).Mul(big.NewInt(100), big.NewInt(1e18))
	amount1 := big.NewInt(0).Mul(big.NewInt(100), big.NewInt(1e18))

	refPrice := cfg.Oracle.RefPrice
	coreBps := cfg.Bot.CoreRangeBps
	tickLower := int32(math.Log(refPrice*(1-float64(coreBps)/10000)) / math.Log(1.0001))
	tickUpper := int32(math.Log(refPrice*(1+float64(coreBps)/10000)) / math.Log(1.0001))

	fmt.Printf("=============================================\n")
	fmt.Printf("       Add Liquidity to GLUSD/USDT\n")
	fmt.Printf("=============================================\n\n")
	fmt.Printf("Token0 (GLUSD): %s\n", token0.Hex())
	fmt.Printf("Token1 (USDT):  %s\n", token1.Hex())
	fmt.Printf("Position Manager: %s\n", posMgrAddr.Hex())
	fmt.Printf("Amount0: %s\n", amount0.String())
	fmt.Printf("Amount1: %s\n", amount1.String())
	fmt.Printf("Tick Range: [%d, %d]\n", tickLower, tickUpper)
	fmt.Printf("Fee: %d\n\n", fee)

	methodID := crypto.Keccak256([]byte("mint((address,address,uint24,int24,int24,uint256,uint256,uint256,uint256,address,uint256))"))[:4]

	recipient := crypto.PubkeyToAddress(privateKey.PublicKey)
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

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("Failed to suggest gas price: %v", err)
	}

	nonce, err := client.PendingNonceAt(ctx, crypto.PubkeyToAddress(privateKey.PublicKey))
	if err != nil {
		log.Fatalf("Failed to get nonce: %v", err)
	}

	tx := types.NewTransaction(nonce, posMgrAddr, big.NewInt(0), 800000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatalf("Failed to sign transaction: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}

	fmt.Printf("Transaction sent: %s\n", signedTx.Hash().Hex())
	fmt.Println("Waiting for confirmation...")

	receipt, err := bind.WaitMined(ctx, client, signedTx)
	if err != nil {
		log.Fatalf("Failed to wait for receipt: %v", err)
	}

	if receipt.Status == 0 {
		log.Fatalf("Transaction failed! Gas used: %d", receipt.GasUsed)
	}

	fmt.Printf("\nLiquidity added successfully!\n")
	fmt.Printf("Gas used: %d\n\n", receipt.GasUsed)

	for _, rlog := range receipt.Logs {
		if len(rlog.Data) >= 96 {
			tokenId := new(big.Int).SetBytes(rlog.Data[0:32])
			amount0 := new(big.Int).SetBytes(rlog.Data[32:64])
			amount1 := new(big.Int).SetBytes(rlog.Data[64:96])
			fmt.Printf("Position Token ID: %s\n", tokenId.String())
			fmt.Printf("Amount0 used: %s\n", amount0.String())
			fmt.Printf("Amount1 used: %s\n", amount1.String())
		}
	}
}

type Bot struct {
	uniswapClient   *uniswap.Client
	positionService *position.PositionService
	riskEngine      *risk.RiskEngine
	priceOracle     *oracle.PriceOracle
	rebalancer      *rebalancer.Rebalancer
	monitor         *monitor.Monitor
	apiServer       *api.Server
}

func NewBot() (*Bot, error) {
	uniswapClient, err := uniswap.NewClient(cfg.Uniswap.RPCURL, cfg.Uniswap.PoolAddress, cfg.Uniswap.FeeTier)
	if err != nil {
		return nil, fmt.Errorf("failed to create uniswap client: %w", err)
	}

	positionService := position.NewPositionService()
	riskEngine := risk.NewRiskEngine(cfg)
	priceOracle := oracle.NewPriceOracle(cfg, uniswapClient)
	rebalancerSvc := rebalancer.NewRebalancer(cfg, positionService, riskEngine)
	monitorSvc := monitor.NewMonitor(positionService, riskEngine, rebalancerSvc)
	apiServer := api.NewServer(cfg, positionService, riskEngine, rebalancerSvc, monitorSvc)

	return &Bot{
		uniswapClient:   uniswapClient,
		positionService: positionService,
		riskEngine:      riskEngine,
		priceOracle:     priceOracle,
		rebalancer:      rebalancerSvc,
		monitor:         monitorSvc,
		apiServer:       apiServer,
	}, nil
}

func handleStartBot() {
	bot, err := NewBot()
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	log.Println("Starting GLUSD/USDT Market Making Bot...")

	bot.rebalancer.Start()

	go bot.priceUpdateLoop()
	go bot.rebalanceLoop()
	go bot.monitorLoop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("Stopping bot...")
		bot.Stop()
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting API server on %s", addr)
	if err := bot.apiServer.Run(addr); err != nil {
		log.Fatalf("Bot failed: %v", err)
	}
}

func (b *Bot) priceUpdateLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.updatePrices()
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bot) updatePrices() {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	priceInfo, err := b.priceOracle.GetPriceInfo(ctx)
	if err != nil {
		log.Printf("Failed to get price info: %v", err)
		return
	}

	current, _ := priceInfo.CurrentPrice.Float64()
	twap, _ := priceInfo.TwapPrice.Float64()

	b.monitor.UpdatePrices(current, twap, cfg.Oracle.RefPrice)
	b.rebalancer.UpdatePrices(rebalancer.PriceInfo{
		CurrentPrice: priceInfo.CurrentPrice,
		TwapPrice:    priceInfo.TwapPrice,
		RefPrice:     priceInfo.RefPrice,
	})

	log.Printf("Price update: current=%.6f, twap=%.6f, ref=%.6f", current, twap, cfg.Oracle.RefPrice)
}

func (b *Bot) rebalanceLoop() {
	ticker := time.NewTicker(time.Duration(cfg.Bot.RebalanceIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.executeRebalance()
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bot) executeRebalance() {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	err := b.rebalancer.ExecuteRebalance(ctx)
	if err != nil {
		log.Printf("Rebalance failed: %v", err)
		return
	}

	b.monitor.UpdateLastRebalance(b.rebalancer.GetLastRebalanceTime())
	log.Println("Rebalance executed successfully")
}

func (b *Bot) monitorLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.checkAlerts()
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bot) checkAlerts() {
	alerts := b.monitor.CheckAlerts()
	for _, alert := range alerts {
		log.Printf("[%s] %s", alert.Level, alert.Message)
	}

	if b.riskEngine.IsCircuitBreakerActive() {
		log.Println("Circuit breaker triggered! Stopping rebalancer...")
		b.rebalancer.Stop()
		b.monitor.UpdateStatus("circuit_breaker")
	}
}

func (b *Bot) Stop() {
	cancel()
	b.rebalancer.Stop()
	b.uniswapClient.Close()
}
