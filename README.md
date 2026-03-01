# Uniswap V3 Market Making Bot

Automated market making bot for Uniswap V3, optimized for stablecoin trading pairs with price maintained near 1.0.

## Features

- **Three-tier Liquidity Positions**: Core (core depth) / Mid (buffer layer) / Tail (tail defense)
- **Auto Rebalancing**: Triggered by price deviation and timer
- **Risk Management**: Circuit breaker auto-pause when price deviates
- **Real-time Monitoring**: Price, fees, gas costs, net PnL
- **API Control**: Start/stop/query status
- **Web Admin Interface**: Visual control panel

---

# System Architecture

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        Web Admin UI                             │
│                      (http://localhost:8080)                    │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                         API Server                              │
│                    (Gin HTTP Server :8080)                      │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐     │
│  │ Status   │ │ Metrics  │ │ Control  │ │ Trade Ops    │     │
│  │ /status  │ │ /metrics│ │ /start   │ │ /create-pool │     │
│  │          │ │          │ │ /stop    │ │ /add-liquid  │     │
│  │          │ │          │ │ /rebalance│ │ /swap       │     │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘     │
└─────────────────────────────────────────────────────────────────┘
                                │
        ┌───────────────────────┼───────────────────────┐
        ▼                       ▼                       ▼
┌───────────────┐   ┌─────────────────┐   ┌─────────────────┐
│   Rebalancer  │   │   Risk Engine   │   │    Monitor     │
│ (Rebalancing) │   │  (Risk Control) │   │ (Monitoring)   │
│               │   │                 │   │                 │
│ - Price watch │   │ - Circuit break │   │ - Price track  │
│ - Range calc  │   │ - Loss calc     │   │ - Alerts       │
│ - Exec rebal  │   │ - Risk eval     │   │ - Status       │
└───────────────┘   └─────────────────┘   └─────────────────┘
        │                                               │
        └───────────────────────┬───────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Executor (Trade Execution)                  │
│                                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │ CreatePool  │  │ AddLiquidity│  │    Swap     │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
│                           │                                     │
│                           ▼                                     │
│              ┌────────────────────────┐                         │
│              │  Contract Bindings     │                        │
│              │  (go-ethereum abigen)│                        │
│              └────────────────────────┘                         │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Uniswap V3 Contracts                         │
│  ┌────────────────┐  ┌─────────────────┐  ┌────────────────┐  │
│  │    Factory    │  │ Position Manager │  │  Swap Router   │  │
│  │ 0x1F984...   │  │  0xB7F724...    │  │  0xd1AAE...   │  │
│  └────────────────┘  └─────────────────┘  └────────────────┘  │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              Pool (GLUSD/USDT)                           │ │
│  │  Token0: GLUSD (0x948e...)  Token1: USDT (0x2d7e...)    │ │
│  │  Fee: 500 (0.05%)                                        │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Core Modules

### 1. API Server (`pkg/api/`)
- HTTP RESTful API
- Web UI static file serving
- Trade requests (create pool, add liquidity, swap)

### 2. Rebalancer (`pkg/rebalancer/`)
- **Core market making logic**
- Monitor price deviation
- Calculate three-tier liquidity ranges
- Trigger rebalancing

### 3. Risk Engine (`pkg/risk/`)
- Circuit breaker: auto-pause when price > threshold
- Loss calculation: max daily loss limit
- Risk assessment: max single trade limit

### 4. Monitor (`pkg/monitor/`)
- Real-time price monitoring
- Alerts generation (de-anchoring, position anomalies)
- Status tracking

### 5. Executor (`pkg/executor/`)
- Trade execution layer
- Direct Ethereum interaction using contract bindings
- Support: create pool, add liquidity, swap

### 6. Oracle (`pkg/oracle/`)
- TWAP price calculation
- Reference price management

### 7. Position (`pkg/position/`)
- Position management
- Three-tier liquidity configuration

---

# Design Philosophy

## 1. Three-tier Liquidity Positions

Split funds into three tiers, forming "depth + buffer + tail defense":

```
Price Range Config (Reference Price 1.0):

Tail (Tail Defense)  ──────────  [0.98 - 1.02]  10%
Mid (Buffer)        ───────────  [0.995 - 1.005] 30%
Core (Core Depth)   ──────────  [0.9995 - 1.0005] 60%
                        ↑
                    Price 1.0
```

- **Core**: ±5 bps, 60% - near-zero slippage for traders at 1.0
- **Mid**: ±50 bps, 30% - reduce frequent rebalancing
- **Tail**: ±200 bps, 10% - provide quotes in extreme cases

## 2. Rebalancing Strategy

Trigger conditions:
- **Timer**: Check every 60 seconds (configurable)
- **Price deviation**: Trigger immediately when price > 0.2%

Rebalancing flow:
```
Price Update → Calculate Deviation → Check Trigger → Risk Check → Execute → Update Position
```

## 3. Risk Control

- **Circuit Breaker**: Triggered when TWAP price deviation > 30bps, pause all operations
- **Max Daily Loss**: 50bps
- **Max Single Trade**: 10bps
- **Circuit Breaker Duration**: 15 minutes

---

# API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/` | GET | Web admin interface |
| `/api/v1/status` | GET | Running status, circuit breaker status |
| `/api/v1/metrics` | GET | Price, fees, net PnL |
| `/api/v1/positions` | GET | Position info |
| `/api/v1/risk` | GET | Risk status |
| `/api/v1/balance` | GET | Account balance |
| `/api/v1/start` | POST | Start bot |
| `/api/v1/stop` | POST | Stop bot |
| `/api/v1/alerts` | GET | Alerts |
| `/api/v1/rebalance` | POST | Trigger rebalancing |
| `/api/v1/create-pool` | POST | Create trading pool |
| `/api/v1/add-liquidity` | POST | Add liquidity |
| `/api/v1/swap` | POST | Execute swap |

---

# Configuration

Edit `config.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

uniswap:
  rpc_url: "https://astrochain-sepolia.gateway.tenderly.co/your-api-key"
  chain_id: 1301
  pool_address: "0x..."
  factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
  position_manager: "0xB7F724d6dDDFd008eFf5cc2834edDE5F9eF0d075"
  swap_router: "0xd1AAE39293221B77B0C71fBD6dCb7Ea29Bb5B166"
  quoter: "0x6Dd37329A1A225a6Fca658265D460423DCafBF89"
  fee_tier: 500
  token0_address: "0x948e15b38f096d3a664fdeef44c13709732b2110"  # GLUSD
  token1_address: "0x2d7efff683b0a21e0989729e0249c42cdf9ee442"  # USDT

bot:
  private_key: "your_private_key"
  core_ratio: 0.6
  mid_ratio: 0.3
  tail_ratio: 0.1
  core_range_bps: 5
  mid_range_bps: 50
  tail_range_bps: 200
  rebalance_threshold: 0.002
  rebalance_interval_seconds: 60

risk:
  circuit_breaker_deviation_bps: 30
  max_daily_loss_bps: 50
  max_single_trade_bps: 10

oracle:
  ref_price: 1.0

execution:
  gas_limit: 500000
  gas_price_multiplier: 1.1
  max_slippage_bps: 30
  deadline_seconds: 300
  retry_times: 3
```

## Uniswap V3 Contract Addresses

See `address.md` for detailed contract addresses on# Quick Start

 different networks.

---

## 1. Build

```bash
go build -o uniswap-bot .
```

## 2. Create Pool

```bash
./uniswap-bot create-pool config.yaml
```

Output:
```
Pool Address: 0x...
```

Fill the pool address into `config.yaml` `pool_address`.

## 3. Add Liquidity

```bash
./uniswap-bot add-liquidity config.yaml
```

## 4. Start Bot

```bash
./uniswap-bot start config.yaml
```

## 5. Web Admin Interface

Visit: http://localhost:8080

---

# Technical Stack

- Go 1.21+
- Gin Web Framework
- go-ethereum (contract bindings via abigen)
- Uniswap V3 Contracts

---

# Project Structure

```
uniswap-bot/
├── main.go                    # Main entry point
├── config.yaml                # Configuration file
├── address.md                # Contract addresses
├── README.md                 # Project documentation
├── web/
│   └── index.html            # Web admin interface
├── config/
│   ├── config.go             # Config loading
│   └── contracts.go          # Contract addresses
└── pkg/
    ├── api/                  # HTTP API server
    ├── executor/              # Trade execution & contract bindings
    │   ├── service.go        # Main executor
    │   └── erc20.go          # ERC20 token queries
    ├── contracts/            # Uniswap V3 contract bindings
    │   ├── uniswapv3_factory.go
    │   ├── uniswapv3_pool.go
    │   ├── uniswapv3_nft_position_manager.go
    │   ├── uniswapv3_router_v2.go
    │   └── uniswapv3_quoter.go
    ├── monitor/              # Monitoring & alerts
    ├── oracle/               # Price oracle
    ├── position/             # Position management
    ├── rebalancer/           # Rebalancing logic
    └── risk/                 # Risk engine
```

---

# License

MIT
