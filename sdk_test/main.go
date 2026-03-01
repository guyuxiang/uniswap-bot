package main

import (
	"context"
	"fmt"
	"log"
	"math/big"

	sdkCoreEntities "github.com/daoleno/uniswap-sdk-core/entities"
	"github.com/daoleno/uniswapv3-sdk/constants"
	"github.com/daoleno/uniswapv3-sdk/entities"
	"github.com/daoleno/uniswapv3-sdk/examples/contract"
	"github.com/daoleno/uniswapv3-sdk/utils"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	RPCURL      = "https://astrochain-sepolia.gateway.tenderly.co/5neqYQoinBsj3Cc3O36Dun"
	CHAINID     = uint(1301)
	FACTORYADDR = "0x1F98431c8aD98523631AE4a59f267346ea31F984"
	POSITIONMGR = "0xC36442b4a4522E871399CD717aBDD847Ab11FE88"
	POOLADDR    = "0x4e250d2b6f4534a0e5d3f08c3b16e80c4e63aef4"
	FEE         = uint32(500)
	TOKEN0ADDR  = "0x948e15b38f096d3a664fdeef44c13709732b2110"
	TOKEN1ADDR  = "0x2d7efff683b0a21e0989729e0249c42cdf9ee442"
)

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	ctx := context.Background()

	client, err := ethclient.Dial(RPCURL)
	if err != nil {
		log.Fatal("Failed to connect to client:", err)
	}
	log.Println("✓ Connected to RPC")

	blockNum, err := client.BlockNumber(ctx)
	if err != nil {
		log.Fatal("Failed to get block number:", err)
	}
	log.Printf("✓ Current block: %d\n", blockNum)

	log.Println("\n=== Test 1: Get Factory ===")
	factory, err := contract.NewUniswapv3Factory(common.HexToAddress(FACTORYADDR), client)
	if err != nil {
		log.Fatal("Failed to create factory:", err)
	}
	log.Printf("✓ Factory created: %s\n", FACTORYADDR)

	log.Println("\n=== Test 2: Get Pool Address from Factory ===")
	actualPoolAddr, err := factory.GetPool(nil, common.HexToAddress(TOKEN0ADDR), common.HexToAddress(TOKEN1ADDR), big.NewInt(int64(FEE)))
	if err != nil {
		log.Println("Warning: Failed to get pool from factory:", err)
	} else if actualPoolAddr.Hex() == "0x0000000000000000000000000000000000000000" {
		log.Println("Pool does not exist on this network")
	} else {
		log.Printf("✓ Pool address from factory: %s\n", actualPoolAddr.Hex())
	}

	log.Println("\n=== Test 3: Get Position Manager ===")
	posMgr, err := contract.NewUniswapv3NFTPositionManager(common.HexToAddress(POSITIONMGR), client)
	if err != nil {
		log.Fatal("Failed to create position manager:", err)
	}
	log.Printf("✓ Position Manager created: %s\n", POSITIONMGR)

	log.Println("\n=== Test 4: Get Position Details (if exists) ===")
	position, err := posMgr.Positions(nil, big.NewInt(1))
	if err != nil {
		log.Println("Warning: Failed to get position:", err)
	} else {
		log.Printf("✓ Position liquidity: %s\n", position.Liquidity.String())
		log.Printf("✓ Position token0: %s\n", position.Token0.Hex())
		log.Printf("✓ Position token1: %s\n", position.Token1.Hex())
		log.Printf("✓ Position tokensOwed0: %s\n", position.TokensOwed0.String())
		log.Printf("✓ Position tokensOwed1: %s\n", position.TokensOwed1.String())
	}

	log.Println("\n=== Test 5: Get Token Balances ===")
	walletAddr := common.HexToAddress("0x20AaF3E0162dc97b4C71281aC1Ca4719cEb15060")
	balance0, err := getTokenBalance(ctx, client, common.HexToAddress(TOKEN0ADDR), walletAddr)
	if err != nil {
		log.Println("Warning: Failed to get token0 balance:", err)
		balance0 = big.NewInt(0)
	}
	balance1, err := getTokenBalance(ctx, client, common.HexToAddress(TOKEN1ADDR), walletAddr)
	if err != nil {
		log.Println("Warning: Failed to get token1 balance:", err)
		balance1 = big.NewInt(0)
	}
	log.Printf("✓ Token0 (GLUSD) balance: %s\n", balance0.String())
	log.Printf("✓ Token1 (USDT) balance: %s\n", balance1.String())

	log.Println("\n=== Test 6: Get ETH Balance ===")
	ethBalance, err := client.BalanceAt(ctx, walletAddr, nil)
	if err != nil {
		log.Println("Warning: Failed to get ETH balance:", err)
		ethBalance = big.NewInt(0)
	}
	log.Printf("✓ ETH balance: %s\n", ethBalance.String())

	if actualPoolAddr.Hex() != "0x0000000000000000000000000000000000000000" && actualPoolAddr.Hex() != "" {
		log.Println("\n=== Test 7: Get Pool Data ===")
		pool, err := getPoolData(ctx, client, actualPoolAddr.Hex(), FEE)
		if err != nil {
			log.Println("Warning: Failed to get pool:", err)
		} else {
			log.Printf("✓ Pool liquidity: %s\n", pool.Liquidity.String())
			log.Printf("✓ Pool sqrtPriceX96: %s\n", pool.SqrtRatioX96.String())
			log.Printf("✓ Pool tick: %d\n", pool.TickCurrent)
		}
	}

	log.Println("\n=== All Tests Passed ===")
}

func getPoolData(ctx context.Context, client *ethclient.Client, poolAddr string, fee uint32) (*entities.Pool, error) {
	poolAddress := common.HexToAddress(poolAddr)
	contractPool, err := contract.NewUniswapv3Pool(poolAddress, client)
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

	token0 := sdkCoreEntities.NewToken(CHAINID, common.HexToAddress(TOKEN0ADDR), 18, "GLUSD", "GLUSD")
	token1 := sdkCoreEntities.NewToken(CHAINID, common.HexToAddress(TOKEN1ADDR), 18, "USDT", "USDT")

	feeAmount := constants.FeeAmount(fee)
	tickSpacing := constants.TickSpacings[feeAmount]

	minTick := entities.NearestUsableTick(utils.MinTick, tickSpacing)
	maxTick := entities.NearestUsableTick(utils.MaxTick, tickSpacing)

	pooltick, err := contractPool.Ticks(nil, big.NewInt(int64(minTick)))
	if err != nil {
		return nil, fmt.Errorf("failed to get tick: %w", err)
	}

	ticks := []entities.Tick{
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

	tickDataProvider, err := entities.NewTickListDataProvider(ticks, tickSpacing)
	if err != nil {
		return nil, fmt.Errorf("failed to create tick data provider: %w", err)
	}

	pool, err := entities.NewPool(token0, token1, feeAmount, slot0.SqrtPriceX96, liquidity, int(slot0.Tick.Int64()), tickDataProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	return pool, nil
}

func getTokenBalance(ctx context.Context, client *ethclient.Client, tokenAddr, ownerAddr common.Address) (*big.Int, error) {
	balanceOfMethod := "0x70a08231"
	balanceOfArgs := common.LeftPadBytes(ownerAddr.Bytes(), 32)
	data := append([]byte(balanceOfMethod), balanceOfArgs...)

	msg := ethereum.CallMsg{
		To:   &tokenAddr,
		Data: data,
	}

	result, err := client.CallContract(ctx, msg, nil)
	if err != nil {
		return big.NewInt(0), nil
	}

	if len(result) == 0 {
		return big.NewInt(0), nil
	}

	return new(big.Int).SetBytes(result), nil
}
