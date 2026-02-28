package uniswap

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	ChainID = 1301
)

var (
	USDT = Token{
		Address:  common.HexToAddress("0x2d7efff683b0a21e0989729e0249c42cdf9ee442"),
		Decimals: 18,
		Symbol:   "USDT",
		Name:     "USDT",
	}
	GLUSD = Token{
		Address:  common.HexToAddress("0x948e15b38f096d3a664fdeef44c13709732b2110"),
		Decimals: 18,
		Symbol:   "GLUSD",
		Name:     "GLUSD",
	}
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
	ethClient *ethclient.Client
	poolAddr  common.Address
	feeTier   uint32
	chainID   int64
}

func NewClient(rpcURL, poolAddress string, feeTier uint32) (*Client, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethclient: %w", err)
	}

	return &Client{
		ethClient: client,
		poolAddr:  common.HexToAddress(poolAddress),
		feeTier:   feeTier,
		chainID:   ChainID,
	}, nil
}

func (c *Client) GetPool(ctx context.Context) (*Pool, error) {
	return &Pool{
		Address:   c.poolAddr,
		Token0:    &GLUSD,
		Token1:    &USDT,
		Fee:       c.feeTier,
		Liquidity: big.NewInt(0),
		Slot0: Slot0{
			Price:            big.NewInt(0),
			Tick:             0,
			ObservationIndex: 0,
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

func (c *Client) Close() error {
	c.ethClient.Close()
	return nil
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
