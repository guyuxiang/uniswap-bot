package position

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Position struct {
	TokenId     *big.Int
	Liquidity   *big.Int
	TickLower   int32
	TickUpper   int32
	TokensOwed0 *big.Int
	TokensOwed1 *big.Int
}

type Layer struct {
	Name       string
	Ratio      float64
	TickLower  int32
	TickUpper  int32
	PositionId *big.Int
	Liquidity  *big.Int
}

type PositionManagerClient interface {
	Mint(ctx context.Context, token0, token1 common.Address, fee uint32, tickLower, tickUpper int32, amount0Desired, amount1Desired *big.Int) (*types.Transaction, *big.Int, error)
	AddLiquidity(ctx context.Context, tokenId *big.Int, amount0Desired, amount1Desired *big.Int) (*types.Transaction, error)
	RemoveLiquidity(ctx context.Context, tokenId, liquidity *big.Int) (*types.Transaction, error)
	Collect(ctx context.Context, tokenId *big.Int, recipient common.Address) (*types.Transaction, error)
	GetPosition(ctx context.Context, tokenId *big.Int) (*Position, error)
}

type PositionService struct {
	positions map[string]*Layer
}

func NewPositionService() *PositionService {
	return &PositionService{
		positions: make(map[string]*Layer),
	}
}

func (s *PositionService) AddLayer(name string, ratio float64, tickLower, tickUpper int32, posId *big.Int) {
	s.positions[name] = &Layer{
		Name:       name,
		Ratio:      ratio,
		TickLower:  tickLower,
		TickUpper:  tickUpper,
		PositionId: posId,
	}
}

func (s *PositionService) GetLayers() []*Layer {
	layers := make([]*Layer, 0, len(s.positions))
	for _, layer := range s.positions {
		layers = append(layers, layer)
	}
	return layers
}

func (s *PositionService) GetLayerByName(name string) (*Layer, error) {
	layer, ok := s.positions[name]
	if !ok {
		return nil, fmt.Errorf("layer %s not found", name)
	}
	return layer, nil
}

func (s *PositionService) GetTotalRatio() float64 {
	var total float64
	for _, layer := range s.positions {
		total += layer.Ratio
	}
	return total
}
