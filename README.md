```bash
 /$$       /$$            /$$$$$$   /$$$$$$ 
| $$      |__/           /$$__  $$ /$$__  $$
| $$$$$$$  /$$ /$$$$$$$ | $$  \__/| $$  \ $$
| $$__  $$| $$| $$__  $$| $$ /$$$$| $$  | $$
| $$  \ $$| $$| $$  \ $$| $$|_  $$| $$  | $$
| $$  | $$| $$| $$  | $$| $$  \ $$| $$  | $$
| $$$$$$$/| $$| $$  | $$|  $$$$$$/|  $$$$$$/
|_______/ |__/|__/  |__/ \______/  \______/ 

```
# binGO

Multiplayer bingo game server with WebSocket support, SQLite persistence, AI-powered buzzword generation, Prometheus metrics, and Grafana dashboards.

This is the **server** component — players connect via the companion **[binGO-CLI](https://github.com/jkMLnop/binGO-CLI)** terminal client or the built-in web client.

## Quick Start

```bash
# Run locally (no database, in-memory only)
go run . -port 8080

# With SQLite persistence
go run . -port 8080 -db ./bingo.db
```

Then connect clients:
- **Web client**: open http://localhost:8080 in a browser
- **CLI client**: `binGO-CLI -mode client -server localhost:8080`

## Usage

```
binGO -port 8080 -db ./bingo.db
```

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `8080` | HTTP/WebSocket listen port |
| `-db` | (none) | SQLite database path (omit for in-memory only) |
| `-version` | `false` | Print binary version and exit |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_API_KEY` | `dev-admin-key-local-only` | Admin API auth key |
| `DEEPSEEK_API_KEY` | (none) | Required for AI buzzword generation |
| `DEEPSEEK_BASE_URL` | `https://api.deepseek.com` | DeepSeek API base URL |
| `DEEPSEEK_MODEL` | `deepseek-v4-pro` | DeepSeek model name |
| `LOG_LEVEL` | `info` | Log verbosity |

## Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `/` | None | Web client (SPA) |
| `/ws` | JWT (after login) | WebSocket game connection |
| `/metrics` | None | Prometheus scrape target |
| `/api/status` | None | Health check |
| `/api/game/{code}` | None | Get game info by code |
| `/api/leaderboard` | None | Top players by wins |
| `/admin/api/games` | `X-Admin-Key` | POST create, GET list |
| `/admin/api/games/{id}` | `X-Admin-Key` | GET detail, DELETE force-close |

**Full Admin API docs: [docs/ADMIN_API.md](docs/ADMIN_API.md)**

## Deployment (Fly.io)

```bash
# Staging
cd dagger && go run . deploy --env staging --version $(git rev-parse --short HEAD)

# Production (tagged release)
git tag v2.0.0 && git push origin v2.0.0
```

CI/CD via Dagger + GitHub Actions — see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Monitoring

The stack includes Prometheus (`/metrics`) and Grafana with pre-built dashboards:

```bash
docker-compose up -d
# Grafana: http://localhost:3000 (admin / change_me_in_production)
# Prometheus: http://localhost:9090
```

See [docs/MONITORING_SETUP.md](docs/MONITORING_SETUP.md).

## Architecture

```
binGO/
├── main.go                     # Server entry point, flag parsing, shutdown
├── server/                     # WebSocket server, game logic, LLM, API, metrics
│   ├── server.go               # Core WebSocket handler & game coordination
│   ├── game.go                 # Game & Player structs (thread-safe)
│   ├── api.go                  # REST API endpoints
│   ├── auth.go                 # JWT generation & IP-bound validation
│   ├── admin.go                # Admin API CRUD
│   ├── deepseek.go             # DeepSeek LLM client (OpenAI-compatible)
│   ├── llm.go                  # LLM types, prompts, extraction
│   ├── ratelimit.go            # Per-IP WS + code-guess rate limiting
│   ├── db.go                   # Nil-safe DB helpers
│   ├── player_db.go            # Player tracking
│   ├── feedback.go             # LLM feedback collection
│   ├── room.go                 # Room/game-session management
│   ├── scraper.go              # Web scraping for buzzwords
│   ├── tracing.go              # OpenTelemetry tracing
│   ├── logger.go               # Structured JSON logging
│   ├── metrics.go              # Prometheus metric definitions
│   ├── utils.go                # Utility functions
│   └── types.go                # Message & data types
├── db/                         # Database layer
│   ├── store.go                # GameStore interface
│   └── sqlite.go               # SQLite implementation
├── tests/                      # Integration, E2E & regression tests
├── web-client/src/             # React/TypeScript web client (Vite)
├── dagger/                     # Dagger CI/CD pipeline
├── docker-compose.yml          # Server + Prometheus + Grafana stack
├── Dockerfile                  # Multi-stage Alpine build
├── fly.toml                    # Fly.io production config
├── fly.staging.toml            # Fly.io staging config
├── prometheus.yml              # Prometheus scrape config
├── buzzwords.csv               # Default sample dataset
└── .github/workflows/ci.yml    # CI trigger → Dagger
```

## Testing

```bash
# Unit tests (fast, no Docker)
go test ./...

# Integration tests (DB, API, multiplayer)
go test -tags=integration ./tests -v

# Container regression tests (Docker required)
go test -tags=container -timeout=10m ./tests -v

# Via Dagger (same as CI)
cd dagger && go run . test
```

See [tests/README.md](tests/README.md).

## CI/CD Pipeline

| Trigger | Pipeline |
|---------|----------|
| PR to `main` | `dagger test` (unit + integration) |
| Push to `main` | `dagger test` → `test-container` → build → publish → deploy to **staging** |
| Tag `v*` | Full pipeline → deploy to **production** |

All logic lives in `dagger/main.go`. GitHub Actions is a thin trigger.

## Companion: binGO-CLI

The terminal client is maintained in the **[binGO-CLI](https://github.com/jkMLnop/binGO-CLI)** repository. It connects to this server for multiplayer bingo games.

## Project Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md) for the development roadmap.
