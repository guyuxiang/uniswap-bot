package position

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const PositionManagerABI = `[{"type":"function","name":"mint","inputs":[{"name":"params","type":"tuple","components":[{"name":"token0","type":"address"},{"name":"token1","type":"address"},{"name":"fee","type":"uint24"},{"name":"tickLower","type":"int24"},{"name":"tickUpper","type":"int24"},{"name":"amount0Desired","type":"uint256"},{"name":"amount1Desired","type":"uint256"},{"name":"amount0Min","type":"uint256"},{"name":"amount1Min","type":"uint256"},{"name":"recipient","type":"address"},{"name":"deadline","type":"uint256"}]}],"outputs":[{"name":"tokenId","type":"uint256"},{"name":"amount0","type":"uint256"},{"name":"amount1","type":"uint256"}],"stateMutability":"nonpayable"}]`

type PositionManager struct {
	client     *ethclient.Client
	address    common.Address
	privateKey *crypto.ECDSA
	chainID    *big.Int
}

func NewPositionManager(rpcURL, positionManagerAddr, privateKey string, chainID int64) (*PositionManager, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethclient: %w", err)
	}

	key, err := crypto.HexToECDSA(privateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return &PositionManager{
		client:     client,
		address:    common.HexToAddress(positionManagerAddr),
		privateKey: key,
		chainID:    big.NewInt(chainID),
	}, nil
}

type MintParams struct {
	Token0         common.Address
	Token1         common.Address
	Fee            uint32
	TickLower      int32
	TickUpper      int32
	Amount0Desired *big.Int
	Amount1Desired *big.Int
	Amount0Min     *big.Int
	Amount1Min     *big.Int
	Recipient      common.Address
	Deadline       *big.Int
}

type MintResult struct {
	TokenId *big.Int
	Amount0 *big.Int
	Amount1 *big.Int
}

func (pm *PositionManager) Mint(ctx context.Context, params MintParams) (*MintResult, *types.Transaction, error) {
	parsedABI, err := abi.JSON([]byte(PositionManagerABI))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	recipient := crypto.PubkeyToAddress(pm.privateKey.Input)

	type MintParamsInternal struct {
		Token0         common.Address `json:"token0"`
		Token1         common.Address `json:"token1"`
		Fee            uint32         `json:"fee"`
		TickLower      int32          `json:"tickLower"`
		TickUpper      int32          `json:"tickUpper"`
		Amount0Desired *big.Int       `json:"amount0Desired"`
		Amount1Desired *big.Int       `json:"amount1Desired"`
		Amount0Min     *big.Int       `json:"amount0Min"`
		Amount1Min     *big.Int       `json:"amount1Min"`
		Recipient      common.Address `json:"recipient"`
		Deadline       *big.Int       `json:"deadline"`
	}

	internalParams := MintParamsInternal{
		Token0:         params.Token0,
		Token1:         params.Token1,
		Fee:            params.Fee,
		TickLower:      params.TickLower,
		TickUpper:      params.TickUpper,
		Amount0Desired: params.Amount0Desired,
		Amount1Desired: params.Amount1Desired,
		Amount0Min:     params.Amount0Min,
		Amount1Min:     params.Amount1Min,
		Recipient:      recipient,
		Deadline:       params.Deadline,
	}

	input, err := parsedABI.Pack("mint", internalParams)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack input: %w", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(pm.privateKey, pm.chainID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	gasPrice, err := pm.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to suggest gas price: %w", err)
	}

	auth.GasPrice = gasPrice
	auth.GasLimit = 800000

	tx := types.NewTransaction(auth.Nonce.Uint64(), pm.address, big.NewInt(0), 800000, gasPrice, input)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(pm.chainID), pm.privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	err = pm.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, pm.client, signedTx)
	if err != nil {
		return nil, signedTx, fmt.Errorf("failed to wait for receipt: %w", err)
	}

	if receipt.Status == 0 {
		return nil, signedTx, fmt.Errorf("transaction failed")
	}

	result := &MintResult{}
	for _, rlog := range receipt.Logs {
		if len(rlog.Data) >= 96 {
			result.TokenId = new(big.Int).SetBytes(rlog.Data[0:32])
			result.Amount0 = new(big.Int).SetBytes(rlog.Data[32:64])
			result.Amount1 = new(big.Int).SetBytes(rlog.Data[64:96])
		}
	}

	return result, signedTx, nil
}

func (pm *PositionManager) Close() error {
	return pm.client.Close()
}

func PriceToTick(price float64) int32 {
	return int32(math.Log(price) / math.Ln1_0001)
}
