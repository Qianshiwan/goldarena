# 🥇 金归子 GoldArena - 黄金模拟交易游戏平台

> COMEX黄金期货实时行情 | 多人模拟交易 | 游戏币体系 | 赛事竞技

## 项目简介

金归子（GoldArena）是一个基于 COMEX 黄金期货实时行情的模拟交易游戏平台。用户使用虚拟"游戏币"进行黄金期货交易，可参与多人实时模拟交易大赛。

- **交易品种**：COMEX黄金期货 (GC)，100金衡盎司/手
- **行情源**：Twelve Data API (开发) / CME Datafeed (生产)
- **货币体系**：¥10 = 10,000游戏币（1游戏币 = $1模拟资金）
- **首届大赛奖金池**：¥100,000

## 技术栈

| 层级 | 技术 |
|------|------|
| **后端** | Go 1.22 + Gin + PostgreSQL 16 + Redis 7 |
| **前端** | React 18 + Vite 5 + TailwindCSS 3 + TradingView Lightweight Charts |
| **基础设施** | Docker Compose + Nginx |
| **未来演进** | K8s + gRPC微服务化 + TimescaleDB 时序数据 |

## 项目结构

```
goldarena/
├── cmd/gateway/          # API 网关（含全部业务逻辑）
│   ├── main.go           # 入口：路由注册、服务编排
│   ├── user_handler.go   # 用户/认证/钱包
│   ├── trade_handler.go  # 交易引擎/持仓/盈亏
│   └── market_handler.go # 行情/K线/WebSocket
├── internal/common/      # 共享模型、中间件、响应格式
├── pkg/                  # 可复用公共包
│   ├── jwt/              # JWT 令牌管理
│   ├── db/               # PostgreSQL 连接池
│   ├── redis/            # Redis 客户端
│   ├── websocket/        # WebSocket Hub
│   └── errs/             # 统一错误码
├── web/                  # React 前端 SPA
│   └── src/
│       ├── components/   # 图表/交易/布局组件
│       ├── pages/        # 仪表盘/交易/登录/钱包
│       ├── stores/       # Zustand 状态管理
│       └── services/     # API 封装
├── data/init/            # 数据库初始化 SQL
├── docker/               # Dockerfile + nginx 配置
├── configs/              # 应用配置 YAML
└── docker-compose.yml    # 一键启动全部服务
```

## 快速开始

### 前置条件

- Docker & Docker Compose
- (本地开发) Go 1.22+, Node.js 22+

### 方式一：Docker 一键启动

```bash
cd goldarena
make docker-up
```

访问：
- 前端：http://localhost:3000
- API：http://localhost:8080/api/v1

### 方式二：本地开发

```bash
# 1. 启动数据库
make db-up

# 2. 初始化数据库
make init-db

# 3. 启动后端（终端1）
make dev-backend

# 4. 启动前端（终端2）
make dev-frontend
```

## 核心 API

| 接口 | 方法 | 路径 | 说明 |
|------|------|------|------|
| 用户注册 | POST | `/api/v1/auth/register` | 注册即送1万游戏币 |
| 用户登录 | POST | `/api/v1/auth/login` | 返回JWT令牌 |
| 实时报价 | GET | `/api/v1/market/quote?symbol=GC` | 公开接口 |
| K线数据 | GET | `/api/v1/market/klines?symbol=GC&period=1m` | 公开接口 |
| 下单交易 | POST | `/api/v1/trade/order` | 需要认证 |
| 持仓查询 | GET | `/api/v1/trade/positions` | 需要认证 |
| 平仓 | POST | `/api/v1/trade/close` | 需要认证 |
| 钱包查询 | GET | `/api/v1/user/wallet` | 需要认证 |
| 充值 | POST | `/api/v1/user/wallet/recharge` | 需要认证 |
| WebSocket | GET | `/api/v1/ws` | 实时行情推送 |

## 开发指南

详细的技术文档见 `黄金模拟交易平台-开发文档.docx`（上级目录）。

## License

Proprietary - 金归子 GoldArena
