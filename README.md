# TradeNexus

Institutional-grade stock signal platform for high-conviction momentum, breakout, and continuation signals.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                      TradeNexus                              │
├──────────────────────────────────────────────────────────────┤
│  Frontend (React + TypeScript + Vite)                        │
│  ├── Dashboard (health, stats, recent signals)               │
│  ├── Signals (filtered by timeframe, expanded details)       │
│  ├── Watchlists (CRUD, symbol search, dynamic WS subscribe)  │
│  ├── Scanners (weekly institutional scanner results)         │
│  └── Settings (Telegram integration with AES-256-GCM)       │
├──────────────────────────────────────────────────────────────┤
│  REST API (Go + Chi router)                                  │
│  ├── Basic Auth middleware                                   │
│  ├── Watchlist CRUD → Dynamic WS subscription                │
│  └── Signal query, scanner trigger, health check             │
├──────────────────────────────────────────────────────────────┤
│  Signal Pipeline                                             │
│  ├── Redis dedup (SHA256 hash) → MongoDB fallback            │
│  ├── Telegram dispatch (Redis Streams + consumer group)      │
│  └── Dead letter queue with exponential backoff              │
├──────────────────────────────────────────────────────────────┤
│  Strategy Engine                                             │
│  ├── Chase Momentum Pro (Pine Script v6 → Go conversion)     │
│  │   ├── EMA 10/20, SMA 40 trend stack                      │
│  │   ├── 20-bar breakout with crossover detection            │
│  │   ├── Volume spike (1.8x SMA20)                          │
│  │   ├── Strong candle (body > 0.5x ATR)                    │
│  │   ├── RSI momentum filter (>60 bull, <40 bear)           │
│  │   └── Cooldown (12 bars between same-direction signals)   │
│  └── Weekly Institutional Scanner (4 sub-scanners)           │
│      ├── Weekly Breakout (52-wk high + EMA stack + volume)   │
│      ├── Weekly Continuation (higher low + inside breakout)  │
│      ├── 52-Week High Breakout (ATH + volume expansion)     │
│      └── Price Action Continuation (3-week momentum)        │
├──────────────────────────────────────────────────────────────┤
│  Candle Engine                                               │
│  ├── Tick → 1H builder (IST market hours 9:15–15:30)        │
│  ├── 1H → 4H aggregator (9:15–13:15 + 13:15–15:30)         │
│  ├── Daily → Weekly/Monthly aggregator                       │
│  └── Gap detection and historical backfill                   │
├──────────────────────────────────────────────────────────────┤
│  Indicator Engine (Rolling, Snapshot-Persistent)             │
│  ├── EMA (10, 20, 50, 200)   │  SMA (40)                   │
│  ├── RSI (14, Wilder's)      │  ATR (14, Wilder's)         │
│  ├── Volume SMA (20)         │  Breakout (20-bar H/L)      │
│  └── State saved to MongoDB every 5 bars + on shutdown      │
├──────────────────────────────────────────────────────────────┤
│  Broker (Angel One SmartAPI)                                 │
│  ├── JWT + TOTP auth with auto-refresh                       │
│  ├── WebSocket V2 binary parser (Little Endian)              │
│  ├── Historical candle API with rate limiting                │
│  └── Instrument master symbol resolver                       │
├──────────────────────────────────────────────────────────────┤
│  Infrastructure                                              │
│  ├── MongoDB (candles, signals, indicators, watchlists)      │
│  ├── Redis (dedup, active candle backup, Telegram streams)   │
│  ├── AES-256-GCM encryption for Telegram credentials         │
│  └── Event bus (pub/sub) + Worker pools                      │
└──────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites
- Go 1.23+
- Docker & Docker Compose (for MongoDB + Redis)
- Node.js 20+

### 1. Start Infrastructure
```bash
docker compose up -d
```

Redis is published on `127.0.0.1:6380` to avoid conflicts with any Redis instance already running on `6379`.

### 2. Configure Backend
```bash
cd backend
cp .env.example .env
# Edit .env with your Angel One credentials
```

### 3. Run Backend
```bash
cd backend
go mod tidy
go run cmd/tradenexus/main.go
```

### 4. Run Frontend
```bash
cd frontend
npm install
npm run dev
```

### 5. Access
- Frontend: http://localhost:5173
- Backend API: http://localhost:8080
- Default login: `admin` / `tradenexus2026`

## Project Structure

```
pine anti/
├── docker-compose.yml              # MongoDB + Redis
├── backend/
│   ├── .env.example                # Configuration template
│   ├── go.mod                      # Go module
│   ├── cmd/tradenexus/main.go      # Entry point
│   └── internal/
│       ├── api/server.go           # REST API (Chi)
│       ├── broker/                 # Angel One integration
│       │   ├── auth.go             # JWT + TOTP login
│       │   ├── binary_parser.go    # WebSocket V2 binary tick parser
│       │   ├── websocket.go        # WS connection manager
│       │   ├── historical.go       # Historical candle API
│       │   └── symbols.go          # Instrument master resolver
│       ├── candle/                 # Candle engine
│       │   ├── builder.go          # Tick → 1H candle builder
│       │   ├── aggregator.go       # 1H→4H, 1D→1W/1M aggregation
│       │   ├── engine.go           # Orchestrator
│       │   ├── store.go            # MongoDB persistence
│       │   └── reconciler.go       # Gap detection
│       ├── config/config.go        # Environment configuration
│       ├── indicator/              # Rolling indicators
│       │   ├── ema.go / sma.go     # Moving averages
│       │   ├── rsi.go / atr.go     # Momentum & volatility
│       │   ├── breakout.go         # Highest/lowest detection
│       │   └── manager.go          # State management + snapshots
│       ├── lock/redis_lock.go      # Distributed locking
│       ├── logger/logger.go        # Structured logging (zerolog)
│       ├── models/                 # Data models
│       │   ├── candle.go           # OHLCV + ActiveCandle
│       │   ├── signal.go           # Signal + SHA256 dedup hash
│       │   ├── indicator.go        # IndicatorSnapshot
│       │   ├── watchlist.go        # Watchlist + Stock
│       │   ├── scanner.go          # ScannerMatch
│       │   ├── ledger.go           # ProcessingLedger + RecoveryCheckpoint
│       │   └── settings.go         # TelegramSettings + SystemHealth
│       ├── scanner/weekly_engine.go # 4 institutional weekly scanners
│       ├── signal/pipeline.go      # Dedup + persist + dispatch
│       ├── store/                  # Data stores
│       │   ├── mongo.go            # MongoDB client + indexes
│       │   ├── redis.go            # Redis client + streams
│       │   └── encryption.go       # AES-256-GCM
│       ├── strategy/pine_engine.go # Pine Script → Go conversion
│       ├── telegram/dispatcher.go  # Redis Streams consumer + delivery
│       └── worker/                 # Concurrency
│           ├── event_bus.go        # Pub/sub event system
│           └── pool.go             # Worker pool
└── frontend/
    ├── vite.config.ts              # Vite config + API proxy
    └── src/
        ├── api.ts                  # Typed API client
        ├── types.ts                # TypeScript interfaces
        ├── App.tsx                 # Full SPA (5 pages)
        ├── index.css               # Apple-inspired design system
        └── main.tsx                # Entry point
```

## Timeframes

| Timeframe | Role | Signal Type |
|-----------|------|-------------|
| 4H | Early Momentum | First signal for intraday/swing |
| 1D | Swing Confirmation | Primary swing trading signal |
| 1W | Institutional Trend | Position-level confirmation |
| 1M | Macro Trend | Portfolio-level conviction |

## 4H Candle Boundaries (IST)

| Candle | Start | End | Duration |
|--------|-------|-----|----------|
| 1 | 9:15 | 13:15 | 4 hours |
| 2 | 13:15 | 15:30 | 2h 15m |
