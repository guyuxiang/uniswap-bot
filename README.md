# GLUSD/USDT 做市机器人

Uniswap V3 自动做市机器人，稳定币交易对价格保持在 1.0 附近。

## 功能特性

- **三层流动性头寸**: Core(核心深度) / Mid(缓冲层) / Tail(尾部防御)
- **自动再平衡**: 根据价格偏离和定时触发
- **风控熔断**: 价格偏离超过阈值自动暂停
- **实时监控**: 价格、手续费、Gas成本、净收益
- **API控制**: 启动/停止/查询状态
- **Web管理界面**: 可视化控制面板

---

# 系统架构

## 整体架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                        Web 管理界面                              │
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
│   Rebalancer  │   │   Risk Engine   │   │    Monitor      │
│  (再平衡逻辑)  │   │    (风控引擎)   │   │    (监控告警)   │
│               │   │                 │   │                 │
│ - 价格监控    │   │ - 熔断判断      │   │ - 价格追踪      │
│ - 区间计算    │   │ - 损失计算      │   │ - 告警生成      │
│ - 调仓执行    │   │ - 风险评估      │   │ - 状态更新      │
└───────────────┘   └─────────────────┘   └─────────────────┘
        │                                               │
        └───────────────────────┬───────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Executor (交易执行层)                       │
│                                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │ CreatePool  │  │ AddLiquidity│  │    Swap     │            │
│  │ (创建池子)  │  │  (添加流动性)│  │   (交易)    │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
│                           │                                     │
│                           ▼                                     │
│              ┌────────────────────────┐                         │
│              │  Ethereum RPC (JSON-RPC)│                        │
│              │  chainId: 1301 (Unichain)│                       │
│              └────────────────────────┘                         │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Uniswap V3 Contracts                         │
│  ┌────────────────┐  ┌─────────────────┐  ┌────────────────┐  │
│  │    Factory    │  │ Position Manager│  │    Swap Router │  │
│  │ 0x1F984...   │  │  0xC36442...    │  │  0xE5924...   │  │
│  └────────────────┘  └─────────────────┘  └────────────────┘  │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              Pool (GLUSD/USDT)                           │ │
│  │  Token0: GLUSD (0x948e...)  Token1: USDT (0x2d7e...)    │ │
│  │  Fee: 500 (0.05%)                                        │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## 核心模块

### 1. API Server (`pkg/api/`)
- 提供HTTP RESTful接口
- 集成Web前端静态文件服务
- 处理交易请求（创建池子、添加流动性、Swap）

### 2. Rebalancer (`pkg/rebalancer/`)
- **核心做市逻辑**
- 监控价格偏离
- 计算三层流动性区间
- 触发再平衡操作

### 3. Risk Engine (`pkg/risk/`)
- 熔断机制：价格偏离 > 阈值自动暂停
- 损失计算：单日最大损失限制
- 风险评估：单笔最大交易限制

### 4. Monitor (`pkg/monitor/`)
- 实时价格监控
- 告警生成（脱锚、头寸异常）
- 状态追踪

### 5. Executor (`pkg/executor/`)
- 交易执行层
- 直接与以太坊交互
- 支持：创建池子、添加流动性、Swap交易

### 6. Oracle (`pkg/oracle/`)
- TWAP价格计算
- 参考价格管理

### 7. Position (`pkg/position/`)
- 头寸管理
- 三层流动性配置

---

# 设计理念

## 1. 三层流动性头寸

把资金拆成三档，形成"深度 + 缓冲 + 尾部防御"：

```
价格区间配置 (参考价 1.0):

Tail (尾部防御)  ──────────  [0.98 - 1.02]  占比 10%
Mid (缓冲层)     ───────────  [0.995 - 1.005]  占比 30%
Core (核心深度)  ──────────  [0.9995 - 1.0005] 占比 60%
                        ↑
                    价格 1.0
```

- **Core（核心深度层）**: ±5 bps，占比60%，让交易者在1附近几乎无滑点
- **Mid（缓冲层）**: ±50 bps，占比30%，减少频繁再平衡
- **Tail（尾部防御层）**: ±200 bps，占比10%，极端情况下提供报价

## 2. 再平衡策略

触发条件：
- **定时触发**: 每60秒检查一次（可配置）
- **价格偏离触发**: 价格偏离 > 0.2% 时立即触发

再平衡流程：
```
价格更新 → 计算偏离 → 判断是否触发 → 风控检查 → 执行调仓 → 更新头寸
```

## 3. 风控机制

- **熔断**: TWAP价格偏离 > 30bps 触发熔断，暂停所有操作
- **单日最大损失**: 50bps
- **单笔最大交易**: 10bps
- **熔断持续时间**: 15分钟

---

# 交易逻辑

## 创建交易对流程

```
1. 调用 Factory.createPool(tokenA, tokenB, fee)
       ↓
2. 签名交易并发送到RPC
       ↓
3. 等待交易确认
       ↓
4. 解析日志获取Pool地址
```

## 添加流动性流程

```
1. 计算Tick区间 (基于参考价格)
       ↓
2. 调用 PositionManager.mint(params)
       ↓
3. 签名交易并发送到RPC
       ↓
4. 等待确认
       ↓
5. 返回 TokenID 和实际添加数量
```

## Swap流程

```
1. 设置输入/输出代币和数量
       ↓
2. 计算最小输出数量（考虑滑点）
       ↓
3. 调用 Router.exactInputSingle(params)
       ↓
4. 签名交易并发送到RPC
       ↓
5. 返回交易哈希和实际输出
```

---

# API 接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/` | GET | Web管理界面 |
| `/api/v1/status` | GET | 运行状态、熔断状态 |
| `/api/v1/metrics` | GET | 价格、手续费、净收益等 |
| `/api/v1/positions` | GET | 头寸信息 |
| `/api/v1/risk` | GET | 风控状态 |
| `/api/v1/balance` | GET | 账户余额 |
| `/api/v1/start` | POST | 启动机器人 |
| `/api/v1/stop` | POST | 停止机器人 |
| `/api/v1/alerts` | GET | 告警信息 |
| `/api/v1/rebalance` | POST | 触发再平衡 |
| `/api/v1/create-pool` | POST | 创建交易池 |
| `/api/v1/add-liquidity` | POST | 添加流动性 |
| `/api/v1/swap` | POST | 执行Swap交易 |

### 创建交易对 API

```bash
curl -X POST http://localhost:8080/api/v1/create-pool \
  -H "Content-Type: application/json" \
  -d '{
    "token0": "0x948e15b38f096d3a664fdeef44c13709732b2110",
    "token1": "0x2d7efff683b0a21e0989729e0249c42cdf9ee442",
    "fee": 500
  }'
```

### 添加流动性 API

```bash
curl -X POST http://localhost:8080/api/v1/add-liquidity \
  -H "Content-Type: application/json" \
  -d '{
    "token0": "0x948e15b38f096d3a664fdeef44c13709732b2110",
    "token1": "0x2d7efff683b0a21e0989729e0249c42cdf9ee442",
    "amount0": "1000000000000000000",
    "amount1": "1000000000000000000"
  }'
```

### Swap API

```bash
curl -X POST http://localhost:8080/api/v1/swap \
  -H "Content-Type: application/json" \
  -d '{
    "token_in": "0x948e15b38f096d3a664fdeef44c13709732b2110",
    "token_out": "0x2d7efff683b0a21e0989729e0249c42cdf9ee442",
    "amount_in": "1000000000000000000"
  }'
```

---

# 配置

修改 `config.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

uniswap:
  rpc_url: "https://unichain-sepolia-rpc.publicnode.com"
  chain_id: 1301
  pool_address: "0x..."          # 创建池子后填写
  factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
  position_manager: "0xC36442b4a4522E871399CD717aBDD847Ab11FE88"
  swap_router: "0xE592427A0AEce92De3Edee1F18E0157C05861564"
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

oracle:
  ref_price: 1.0
```

---

# 快速开始

### 1. 编译

```bash
go build -o uniswap-bot .
```

### 2. 创建池子

```bash
./uniswap-bot create-pool config.yaml
```

输出:
```
Pool Address: 0x...
```

将池地址填入 `config.yaml` 的 `pool_address`。

### 3. 添加流动性

```bash
./uniswap-bot add-liquidity config.yaml
```

### 4. 启动机器人

```bash
./uniswap-bot start config.yaml
```

### 5. Web管理界面

启动后访问: http://localhost:8080

---

# 技术栈

- Go 1.21+
- Gin Web Framework
- go-ethereum
- Uniswap V3 SDK

---

# 目录结构

```
uniswap-bot/
├── main.go              # 主程序入口
├── config.yaml           # 配置文件
├── README.md             # 项目文档
├── 做市.md              # 设计文档(参考)
├── web/
│   └── index.html       # Web管理界面
├── config/              # 配置加载
├── pkg/
│   ├── api/             # HTTP API服务
│   ├── executor/        # 交易执行层
│   ├── monitor/         # 监控告警
│   ├── oracle/          # 价格预言机
│   ├── position/        # 头寸管理
│   ├── rebalancer/      # 再平衡逻辑
│   ├── risk/            # 风控引擎
│   └── uniswap/         # Uniswap交互
```

---

# 许可证

MIT
