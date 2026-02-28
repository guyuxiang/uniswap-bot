package oracle

import (
	"context"
	"fmt"
	"math/big"

	"uniswap-bot/config"
	"uniswap-bot/pkg/uniswap"
)

type PriceOracle struct {
	cfg      *config.Config
	client   *uniswap.Client
	refPrice float64
}

func NewPriceOracle(cfg *config.Config, client *uniswap.Client) *PriceOracle {
	return &PriceOracle{
		cfg:      cfg,
		client:   client,
		refPrice: cfg.Oracle.RefPrice,
	}
}

func (o *PriceOracle) GetCurrentPrice(ctx context.Context) (*big.Float, error) {
	return o.client.GetCurrentPrice(ctx)
}

func (o *PriceOracle) GetTwapPrice(ctx context.Context) (*big.Float, error) {
	return o.client.GetTwapPrice(ctx, int64(o.cfg.Oracle.TwapIntervalSec))
}

func (o *PriceOracle) GetRefPrice() float64 {
	return o.refPrice
}

func (o *PriceOracle) GetPriceInfo(ctx context.Context) (*PriceInfo, error) {
	currentPrice, err := o.GetCurrentPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current price: %w", err)
	}

	twapPrice, err := o.GetTwapPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get TWAP price: %w", err)
	}

	return &PriceInfo{
		CurrentPrice: currentPrice,
		TwapPrice:    twapPrice,
		RefPrice:     big.NewFloat(o.refPrice),
	}, nil
}

type PriceInfo struct {
	CurrentPrice *big.Float
	TwapPrice    *big.Float
	RefPrice     *big.Float
}
