package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Uniswap   UniswapConfig   `yaml:"uniswap"`
	Bot       BotConfig       `yaml:"bot"`
	Risk      RiskConfig      `yaml:"risk"`
	Oracle    OracleConfig    `yaml:"oracle"`
	Execution ExecutionConfig `yaml:"execution"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type UniswapConfig struct {
	RPCURL          string `yaml:"rpc_url"`
	ChainID         int64  `yaml:"chain_id"`
	PoolAddress     string `yaml:"pool_address"`
	FactoryAddress  string `yaml:"factory_address"`
	PositionManager string `yaml:"position_manager"`
	SwapRouter      string `yaml:"swap_router"`
	Quoter          string `yaml:"quoter"`
	FeeTier         uint32 `yaml:"fee_tier"`
	Token0Address   string `yaml:"token0_address"`
	Token1Address   string `yaml:"token1_address"`
}

type BotConfig struct {
	PrivateKey           string  `yaml:"private_key"`
	CoreRatio            float64 `yaml:"core_ratio"`
	MidRatio             float64 `yaml:"mid_ratio"`
	TailRatio            float64 `yaml:"tail_ratio"`
	CoreRangeBps         int     `yaml:"core_range_bps"`
	MidRangeBps          int     `yaml:"mid_range_bps"`
	TailRangeBps         int     `yaml:"tail_range_bps"`
	RebalanceThreshold   float64 `yaml:"rebalance_threshold"`
	RebalanceIntervalSec int     `yaml:"rebalance_interval_seconds"`
}

type RiskConfig struct {
	MaxDailyLossBps            int `yaml:"max_daily_loss_bps"`
	MaxSingleTradeBps          int `yaml:"max_single_trade_bps"`
	CircuitBreakerDeviationBps int `yaml:"circuit_breaker_deviation_bps"`
	CircuitBreakerDurationMin  int `yaml:"circuit_breaker_duration_minutes"`
	MaxLeverage                int `yaml:"max_leverage"`
}

type OracleConfig struct {
	TwapIntervalSec int     `yaml:"twap_interval_seconds"`
	RefPrice        float64 `yaml:"ref_price"`
}

type ExecutionConfig struct {
	GasLimit           uint64  `yaml:"gas_limit"`
	GasPriceMultiplier float64 `yaml:"gas_price_multiplier"`
	MaxSlippageBps     int     `yaml:"max_slippage_bps"`
	DeadlineSeconds    int     `yaml:"deadline_seconds"`
	RetryTimes         int     `yaml:"retry_times"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
