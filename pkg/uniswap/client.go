package uniswap

import (
	"context"
	"fmt"
	"math"
	"math/big"

	sdkCoreEntities "github.com/daoleno/uniswap-sdk-core/entities"
	"github.com/daoleno/uniswapv3-sdk/constants"
	sdkEntities "github.com/daoleno/uniswapv3-sdk/entities"
	"github.com/daoleno/uniswapv3-sdk/examples/contract"
	"github.com/daoleno/uniswapv3-sdk/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
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
	poolAddr   common.Address
	feeTier    uint32
	chainID    int64
	token0Addr common.Address
	token1Addr common.Address

	sdkPool     *sdkEntities.Pool
	factory     *contract.Uniswapv3Factory
	positionMgr *contract.Uniswapv3NFTPositionManager
	swapRouter  *contract.Uniswapv3RouterV2
}

func NewClient(rpcURL, poolAddress string, feeTier uint32) (*Client, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethclient: %w", err)
	}

	poolAddr := common.HexToAddress(poolAddress)
	factoryAddr := common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984")
	positionMgrAddr := common.HexToAddress("0xC36442b4a4522E871399CD717aBDD847Ab11FE88")
	swapRouterAddr := common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564")

	token0Addr := common.HexToAddress("0x948e15b38f096d3a664fdeef44c13709732b2110")
	token1Addr := common.HexToAddress("0x2d7efff683b0a21e0989729e0249c42cdf9ee442")

	factory, err := contract.NewUniswapv3Factory(factoryAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory: %w", err)
	}

	positionMgr, err := contract.NewUniswapv3NFTPositionManager(positionMgrAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create position manager: %w", err)
	}

	swapRouter, err := contract.NewUniswapv3RouterV2(swapRouterAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create swap router: %w", err)
	}

	return &Client{
		ethClient:  client,
		poolAddr:   poolAddr,
		feeTier:    feeTier,
		chainID:    1301,
		token0Addr: token0Addr,
		token1Addr: token1Addr,
		factory:    factory,
		positionMgr: positionMgr,
		swapRouter: swapRouter,
	}, nil
}

func (c *Client) GetPool(ctx context.Context) (*Pool, error) {
	pool, err := c.fetchPoolData(ctx)
	if err != nil {
		return nil, err
	}

	return &Pool{
		Address:   c.poolAddr,
		Token0:    &Token{Address: c.token0Addr, Decimals: 18, Symbol: "GLUSD", Name: "GLUSD"},
		Token1:    &Token{Address: c.token1Addr, Decimals: 18, Symbol: "USDT", Name: "USDT"},
		Fee:       c.feeTier,
		Liquidity: pool.Liquidity,
		Slot0: Slot0{
			Price:            pool.SqrtRatioX96,
			Tick:             int32(pool.TickCurrent),
			ObservationIndex: 0,
		},
	}, nil
}

func (c *Client) fetchPoolData(ctx context.Context) (*sdkEntities.Pool, error) {
	contractPool, err := contract.NewUniswapv3Pool(c.poolAddr, c.ethClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool contract: %w", err)
	}

	liquidity, err := contractPool.Liquidity(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get liquidity: %w", err)
	}

	slot0, err := contractPool.Slot0(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get slot0: %w", err)
	}

	token0 := sdkCoreEntities.NewToken(uint(c.chainID), c.token0Addr, 18, "GLUSD", "GLUSD")
	token1 := sdkCoreEntities.NewToken(uint(c.chainID), c.token1Addr, 18, "USDT", "USDT")

	feeAmount := constants.FeeAmount(c.feeTier)
	tickSpacing := constants.TickSpacings[feeAmount]

	minTick := sdkEntities.NearestUsableTick(utils.MinTick, tickSpacing)
	maxTick := sdkEntities.NearestUsableTick(utils.MaxTick, tickSpacing)

	pooltick, err := contractPool.Ticks(nil, big.NewInt(int64(minTick)))
	if err != nil {
		return nil, fmt.Errorf("failed to get tick: %w", err)
	}

	ticks := []sdkEntities.Tick{
		{
			Index:          minTick,
			LiquidityNet:   pooltick.LiquidityNet,
			LiquidityGross: pooltick.LiquidityGross,
		},
		{
			Index:          maxTick,
			LiquidityNet:   new(big.Int).Neg(pooltick.LiquidityNet),
			LiquidityGross: pooltick.LiquidityGross,
		},
	}

	tickDataProvider, err := sdkEntities.NewTickListDataProvider(ticks, tickSpacing)
	if err != nil {
		return nil, fmt.Errorf("failed to create tick data provider: %w", err)
	}

	pool, err := sdkEntities.NewPool(token0, token1, feeAmount, slot0.SqrtPriceX96, liquidity, int(slot0.Tick.Int64()), tickDataProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	return pool, nil
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

func (c *Client) GetPositionManager() *contract.Uniswapv3NFTPositionManager {
	return c.positionMgr
}

func (c *Client) GetSwapRouter() *contract.Uniswapv3RouterV2 {
	return c.swapRouter
}

func (c *Client) GetFactory() *contract.Uniswapv3Factory {
	return c.factory
}

func (c *Client) GetSDKPool(ctx context.Context) (*sdkEntities.Pool, error) {
	if c.sdkPool != nil {
		return c.sdkPool, nil
	}
	return c.fetchPoolData(ctx)
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
