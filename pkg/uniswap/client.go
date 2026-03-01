package uniswap

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"uniswap-bot/config"
	"uniswap-bot/pkg/contracts"
)

type Token struct {
	Address  common.Address
	Decimals uint8
	Symbol   string
	Name     string
}

type Pool struct {
	Address   common.Address
	Token0    *Token
	Token1    *Token
	Fee       uint32
	Liquidity *big.Int
	Slot0     Slot0
}

type Slot0 struct {
	Price            *big.Int
	Tick             int32
	ObservationIndex uint16
}

type Client struct {
	ethClient  *ethclient.Client
	cfg        *config.Config
	poolAddr   common.Address
	feeTier    uint32
	chainID    int64
	token0Addr common.Address
	token1Addr common.Address

	factory     *contracts.Uniswapv3Factory
	positionMgr *contracts.Uniswapv3NFTPositionManager
	swapRouter  *contracts.Uniswapv3RouterV2
	poolContract *contracts.Uniswapv3Pool
}

func NewClient(cfg *config.Config) (*Client, error) {
	client, err := ethclient.Dial(cfg.Uniswap.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethclient: %w", err)
	}

	poolAddr := common.HexToAddress(cfg.Uniswap.PoolAddress)
	token0Addr := common.HexToAddress(cfg.Uniswap.Token0Address)
	token1Addr := common.HexToAddress(cfg.Uniswap.Token1Address)

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

	poolContract, err := contracts.NewUniswapv3Pool(poolAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool contract: %w", err)
	}

	return &Client{
		ethClient:   client,
		cfg:         cfg,
		poolAddr:    poolAddr,
		feeTier:     cfg.Uniswap.FeeTier,
		chainID:     cfg.Uniswap.ChainID,
		token0Addr:  token0Addr,
		token1Addr:  token1Addr,
		factory:     factory,
		positionMgr: positionMgr,
		swapRouter:  swapRouter,
		poolContract: poolContract,
	}, nil
}

func (c *Client) GetPool(ctx context.Context) (*Pool, error) {
	liquidity, err := c.poolContract.Liquidity(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get liquidity: %w", err)
	}

	slot0, err := c.poolContract.Slot0(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get slot0: %w", err)
	}

	return &Pool{
		Address:   c.poolAddr,
		Token0:    &Token{Address: c.token0Addr, Decimals: 18, Symbol: "GLUSD", Name: "GLUSD"},
		Token1:    &Token{Address: c.token1Addr, Decimals: 18, Symbol: "USDT", Name: "USDT"},
		Fee:       c.feeTier,
		Liquidity: liquidity,
		Slot0: Slot0{
			Price:            slot0.SqrtPriceX96,
			Tick:             int32(slot0.Tick.Int64()),
			ObservationIndex: slot0.ObservationIndex,
		},
	}, nil
}

func (c *Client) GetCurrentPrice(ctx context.Context) (*big.Float, error) {
	pool, err := c.GetPool(ctx)
	if err != nil {
		return nil, err
	}

	price := new(big.Float).SetInt(pool.Slot0.Price)
	price = price.Quo(price, big.NewFloat(1<<96))
	price = price.Mul(price, price)

	return price, nil
}

func (c *Client) GetTwapPrice(ctx context.Context, intervalSeconds int64) (*big.Float, error) {
	return big.NewFloat(1.0), nil
}

func (c *Client) GetEthClient() *ethclient.Client {
	return c.ethClient
}

func (c *Client) GetChainID() int64 {
	return c.chainID
}

func (c *Client) GetPositionManager() *contracts.Uniswapv3NFTPositionManager {
	return c.positionMgr
}

func (c *Client) GetSwapRouter() *contracts.Uniswapv3RouterV2 {
	return c.swapRouter
}

func (c *Client) GetFactory() *contracts.Uniswapv3Factory {
	return c.factory
}

func (c *Client) GetPoolContract() *contracts.Uniswapv3Pool {
	return c.poolContract
}

func (c *Client) GetToken0() common.Address {
	return c.token0Addr
}

func (c *Client) GetToken1() common.Address {
	return c.token1Addr
}

func (c *Client) Close() {
	c.ethClient.Close()
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
