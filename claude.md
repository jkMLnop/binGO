# binGO-CLI — AI Context

Multiplayer CLI bingo game with WebSocket server, SQLite persistence, Prometheus
metrics, and Docker/Fly.io deployment.

Module: `github.com/jkMLnop/binGO-CLI` · Go 1.25.3 · CGO required (SQLite)

## Quick Commands

```bash
# Run all unit tests (no Docker, no build tags)
go test ./...

# Run integration tests (need no external infra, just SQLite)
go test -tags=integration ./tests -v

# Run container E2E + regression tests (needs Docker Desktop running)
go test -tags=container -timeout=10m ./tests -v

# Run E2E tests (needs docker-compose stack already running)
docker-compose up -d && go test -tags=e2e ./tests -v

# Build binary (version defaults to "dev")
go build -o binGO-CLI .

# Build binary with version injection
go build -ldflags "-X main.version=v8.2.0" -o binGO-CLI .

# Print version
./binGO-CLI -version

# Run modes
./binGO-CLI -mode standalone                         # local single-player
./binGO-CLI -mode server -port 8080 -db ./bingo.db   # multiplayer server
./binGO-CLI -mode client -server localhost:8080 -code BINGO-XXXXX

# Docker stack (server + Prometheus + Grafana)
cp .env.example .env   # first time only
docker-compose up -d --build

# Verify container tests compile (fast check, no Docker needed)
go build -tags=container ./tests/...

# --- Dagger pipeline (runs same functions locally and in CI) ---
cd dagger && go run . test                                    # unit + integration tests
cd dagger && go run . test-container                          # container regression (~10min)
cd dagger && go run . build --version dev                     # build Docker image
cd dagger && go run . deploy --env staging --version dev      # deploy to staging
cd dagger && go run . all --env staging --version dev --registry-user jkmlnop  # full pipeline

# --- Lefthook (enforces tests before every git push) ---
go install github.com/evilmartians/lefthook@latest && lefthook install
git push              # auto-runs: dagger test + dagger test-container
git push --no-verify  # bypass (escape hatch)
```

## Architecture

### Package Layout

| Package | Purpose |
|---|---|
| `main` (bin.go) | CLI entry point: flag parsing, mode dispatch, signal handling |
| `server/` | WebSocket server, game logic, admin API, auth, metrics, DB helpers |
| `client/` | WebSocket client, auth manager, display, player actions |
| `shared/` | Board model, win detection, display formatting, buzzword loading |
| `standalone/` | Single-player offline mode (no networking) |
| `db/` | `GameStore` interface + SQLite implementation |
| `tests/` | Integration, E2E, container, load, and multiplayer tests |
| `dagger/` | Dagger CI/CD pipeline (separate Go module) |

### Key Types

- **`server.Server`** — owns `Games map`, `CodeToGame map`, `Mux`, `TokenManager`, `DB`, `Metrics`, `Logger`
- **`server.Game`** — game session: `ID`, `Code`, `HostID` (immutable), `Players map`, `IsActive`, `Orphaned`, `Winner`, `CreatedAt`, `EndedAt`
- **`server.Player`** — connected player: `ID`, message channel, WebSocket conn (mutex-protected)
- **`db.GameStore`** — interface for all persistence; only implementation is `SQLiteStore`
- **`shared.Board`** — bingo board with `MarkCell()`, `CheckWin()`, cell ID system (`A1`, `B2`, etc.)

### WebSocket Protocol

Client sends `ClientMessage` (`json:"action"` = `"login"`, `"win"`, `"restart"`; fields: `username`, `token`, `code`).
Server sends `ServerMessage` (`json:"type"` = `"welcome"`, `"game_ended"`, `"player_joined"`, `"server_shutdown"`, `"error"`; fields include `buzzwords`, `players`, `token`, `code`, `winner`, `message`).

Players connect to `/ws`, send a login message with username + game code, receive a welcome with JWT token + buzzword grid. Board is generated client-side from the buzzword list.

### HTTP Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `/ws` | JWT (after login) | WebSocket game connection |
| `/metrics` | None | Prometheus scrape target |
| `/api/status` | None | Server health check |
| `/api/game/{code}` | None | Get game info by code |
| `/api/leaderboard` | None | Top players by wins |
| `/admin/api/games` | `X-Admin-Key` header | POST create, GET list |
| `/admin/api/games/{id}` | `X-Admin-Key` header | GET detail, DELETE force-close |

### Auth

- JWT tokens issued on login, bound to client IP, include username + expiration
- `TokenManager` in `server/auth.go` — `IssueToken()` / `VerifyToken()`
- Admin API uses `X-Admin-Key` header validated against `ADMIN_API_KEY` env var (fallback: `dev-admin-key-local-only`)

### Database (SQLite)

Tables: `games`, `players`, `hosts`, `wins_history`, `game_archives`  
Key indexes: `idx_players_game_id`, `idx_hosts_username`, `idx_wins_player_username`, `idx_game_archives_ended_at`

`GameStore` interface in `db/store.go` — designed for future PostgreSQL swap (Phase 10). All DB calls go through `server/db.go` helper functions (`SaveGameToDB`, `RecordPlayerInDB`, `RecordWinInDB`, `ArchiveGameInDB`, etc.) which are nil-safe (no-op when DB is nil).

Game archives auto-cleanup: background goroutine deletes records older than 4 days, runs hourly.

### Metrics (Prometheus)

All metrics prefixed `bingo_`. Defined in `server/metrics.go`:

| Metric | Type | Notes |
|---|---|---|
| `bingo_game_count` | Gauge | Active games |
| `bingo_player_count` | Gauge | Connected players |
| `bingo_games_created_total` | Counter | |
| `bingo_players_connected_total` | Counter | |
| `bingo_players_disconnected_total` | Counter | |
| `bingo_game_archived_total` | Counter | |
| `bingo_game_restarted_total` | Counter | |
| `bingo_admin_api_requests_total` | Counter | |
| `bingo_errors_total` | CounterVec | Labels: `error_type` = `auth\|game\|db\|ws\|input\|llm` |
| `bingo_rate_limited_total` | CounterVec | Labels: `endpoint` = `ws\|code_guess` |
| `bingo_game_creation_duration_ms` | Histogram | |
| `bingo_database_query_duration_ms` | Histogram | |
| `bingo_admin_api_latency_ms` | Histogram | |

**Quirk:** `bingo_errors_total` is a `CounterVec` — label combinations only appear in `/metrics` output after the first `.Inc()`. A fresh server with zero errors will **not** export this metric until an error occurs. This is normal Prometheus behavior, not a bug.

`ResetMetrics()` unregisters all metrics from `prometheus.DefaultRegisterer` — **must** be called between tests that create new `Server` instances to avoid duplicate registration panics.

### Game Lifecycle

1. **Create** → `NewGame()` + `GenerateGameCode()` → `BINGO-XXXXX` (5 alphanumeric chars)
2. **Join** → player connects via WebSocket with code → `getOrCreateGame(code)` → `createPlayerInGame()`
3. **Play** → client marks cells locally, sends `"win"` action when `CheckWin()` is true
4. **Win** → `handlePlayerWin()` sets `game.Winner` + `game.EndedAt`; archives to DB; broadcasts to all players. **`IsActive` stays `true`** after win (allows restart).
5. **Restart** → host sends `"restart"` → `handleGameRestart()` resets board, keeps code
6. **Orphan** → last player disconnects without winner → `markGameOrphaned()` sets `IsActive=false`, `Orphaned=true`, archives
7. **Admin delete** → `DELETE /admin/api/games/{id}` → sets `IsActive=false`, broadcasts warning

`IsActive=false` is reserved for admin-deleted or orphaned games only. A won game keeps `IsActive=true`.

### Graceful Shutdown

`bin.go` catches `SIGINT` + `SIGTERM` → calls `srv.NotifyShutdown()` (broadcasts `server_shutdown` to all players, closes WebSockets) → `srv.Stop(ctx)` with 5s timeout.

## Test Organization

| Tag / Location | What | Docker needed? |
|---|---|---|
| `go test ./...` (no tags) | Unit tests across all packages | No |
| `-tags=integration` (`tests/`) | DB persistence, API endpoints, archive+cleanup | No |
| `-tags=container` (`tests/`) | Testcontainers: builds Dockerfile, manages containers | Yes (Desktop) |
| `-tags=e2e` (`tests/`) | Requires `docker-compose up` already running | Yes (stack up) |

**Test file → build tag mapping:**
- `server/*_test.go`, `db/*_test.go`, `shared/*_test.go`, `client/*_test.go` → no tag (unit)
- `tests/db_integration_test.go`, `tests/multiplayer_test.go` → `integration`
- `tests/container_e2e_test.go`, `tests/container_regression_test.go` → `container`
- `tests/full_system_load_test.go` → `e2e`

**Regression policy for future changes:**
- Every new feature or behavior change must extend regression coverage in the same PR.
- Prefer automation first: unit/integration/container tests and Playwright smoke tests over manual-only steps.
- Keep `tests/REGRESSION_TESTS.md` current as the human-readable source of intent and coverage status.
- If a test scenario remains manual, add a short note explaining why it cannot be automated yet.
- When adding deploy-critical user flows, update browser smoke checks under `web-client/e2e/` and relevant CI workflows.

## Conventions & Patterns

- **Error wrapping**: Use `fmt.Errorf("context: %w", err)` at all boundaries
- **Error metrics**: Call `s.Metrics.RecordError("type")` on every error path in server.go; valid types: `auth`, `game`, `db`, `ws`, `input`, `llm`
- **DB nil-safety**: All `server/db.go` helpers check `if store == nil { return nil }` — DB is optional
- **HostID immutability**: `game.HostID` is set once on first player connect, never mutated
- **Mutex discipline**: `Game.PlayersMu` protects `Players` map; `Player.wsMu` protects `ws` conn; `Server.GamesMu` protects `Games`/`CodeToGame` maps
- **Game codes**: Format `BINGO-XXXXX` (11 chars), generated by `server.GenerateGameCode()`
- **Structured logging**: `server.Logger` emits JSON events — use `l.GameCreated()`, `l.PlayerJoined()`, etc.
- **Test isolation**: Call `ResetMetrics()` at start of tests that instantiate `NewServer()` to avoid Prometheus registration conflicts

## Environment Variables

| Variable | Default | Used by |
|---|---|---|
| `ADMIN_API_KEY` | `dev-admin-key-local-only` | `server/admin.go` — admin API auth |
| `LOG_LEVEL` | `info` | Server log verbosity |
| `DEEPSEEK_API_KEY` | (none) | `server/deepseek.go` — DeepSeek API auth (required) |
| `DEEPSEEK_BASE_URL` | `https://api.deepseek.com` | `server/deepseek.go` — DeepSeek API base URL |
| `DEEPSEEK_MODEL` | `deepseek-v4-pro` | `server/deepseek.go` — model name |
| `GRAFANA_USER` | `admin` | `docker-compose.yml` — Grafana UI |
| `GRAFANA_PASSWORD` | `change_me_in_production` | `docker-compose.yml` — Grafana UI |

| `FLY_API_TOKEN` | (none) | Dagger pipeline — Fly.io deployment |
| `GHCR_TOKEN` | (none) | Dagger pipeline — push Docker images to ghcr.io |
| `GH_TOKEN` | (none) | Dagger pipeline — create GitHub Releases |

`.env` is gitignored. Copy from `.env.example` before `docker-compose up`.

## Docker / Deployment

- **Dockerfile**: Multi-stage Alpine build, CGO enabled, ~50MB final image. Accepts `ARG VERSION` and `ARG GO_VERSION` build args.
- **docker-compose.yml**: `bingo-server` + `prometheus` + `grafana` with persistent volumes
- **Fly.io production**: `bingo-server` at `bingo-server.fly.dev`, config in `fly.toml`
- **Fly.io staging**: `bingo-server-staging` at `bingo-server-staging.fly.dev`, config in `fly.staging.toml`
- **Docker images**: pushed to `ghcr.io/jkmlnop/bingo-cli` (GitHub Container Registry)
- **Health check**: `GET /api/status` (used by Docker HEALTHCHECK and Fly.io)

## CI/CD Pipeline (Dagger)

All CI/CD logic lives in `dagger/main.go` (separate Go module: `dagger/go.mod`). GitHub Actions (`.github/workflows/ci.yml`) is a thin trigger layer that calls Dagger functions. Lefthook (`.lefthook.yml`) enforces the same pipeline locally on every `git push`.

**Pipeline functions:** `test`, `test-container`, `build`, `publish`, `deploy`, `release`, `all`

**CI triggers:**
- PR to `main` → `test` only (fast feedback)
- Push to `main` → `all --env staging` (test → build → publish → deploy to staging)
- Tag `v*` → `all --env production` + `release` (deploy to prod + GitHub Release)

**Local enforcement (Lefthook):** every `git push` runs `test` + `test-container` before code leaves the machine. Bypass with `git push --no-verify`.

**Required secrets:** `FLY_API_TOKEN` (GitHub + local env), `GHCR_TOKEN` (local env), `GH_TOKEN` (local env for releases)

**Version injection:** `var version = "dev"` in `bin.go`, set via `-ldflags "-X main.version=<tag>"` in Dockerfile + Dagger. Deployed containers are traceable to exact commit/tag.

## Current State (v8.2.0)

Completed AI provider migration: Ollama → DeepSeek API.
- New `server/deepseek.go` — `DeepSeekClient` implementing `LLMClient` interface, OpenAI-compatible `/chat/completions`, SSE streaming, think on/off, temperature/top_p, JSON mode, auto-refill for fixed word count
- `server/llm.go` — trimmed of all Ollama code; kept `LLMClient` interface, prompts, extraction helpers, `ChatMessage`/`WordSet`/`GeneratedSets`/`SSEEvent` types
- `server/api.go` — handlers use `DeepSeekClient`; removed `NumPredict`, `Think`, `InputOrder` fields
- `server/metrics.go` — `bingo_errors_total` includes `llm` label
- `tools/llm_experiments/` — retargeted to DeepSeek API; removed Ollama-specific axes (numPredict, inputOrder, scrapeMode); added thinking, temperature, top_p, max_tokens

See `docs/ROADMAP.md` for upcoming plans.

## Files Worth Knowing

| File | Why |
|---|---|
| `server/server.go` | Core server — all WebSocket handling, game flow, shutdown |
| `server/deepseek.go` | DeepSeek API client — OpenAI-compatible `/chat/completions`, SSE streaming, think/JSON mode |
| `server/llm.go` | Shared LLM types (`LLMClient` interface), prompts, extraction logic |
| `server/ratelimit.go` | Per-IP WS conn limiting, code-guess rate limiting, cleanup |
| `server/metrics.go` | All Prometheus metric definitions + `ResetMetrics()` |
| `server/admin.go` | Admin API CRUD + auth middleware |
| `server/game.go` | `Game` and `Player` structs, `NewGame()`, `AddPlayer()`, `RemovePlayer()` |
| `server/db.go` | Nil-safe DB helper functions bridging `server.Game` ↔ `db.GameStore` |
| `db/store.go` | `GameStore` interface — the contract for all persistence |
| `db/sqlite.go` | SQLite implementation of `GameStore` |
| `shared/board.go` | Board model, cell marking, win detection |
| `bin.go` | Entry point, flag parsing, signal handling, shutdown orchestration, version var |
| `dagger/main.go` | Dagger CI/CD pipeline — test, build, publish, deploy, release |
| `dagger/main_test.go` | Pipeline unit tests (env routing, constants) |
| `.lefthook.yml` | Git pre-push hooks — enforces test suite before every push |
| `.github/workflows/ci.yml` | Thin CI trigger calling Dagger functions |
| `fly.staging.toml` | Fly.io staging config (`bingo-server-staging`) |
| `docs/DEVOPS.md` | DevOps philosophy, tool roles, test tiers, local workflow |
| `CHANGELOG.md` | Detailed release history with test listings |
| `docs/ROADMAP.md` | TODO items by phase |
| `tests/REGRESSION_TESTS.md` | Manual regression test checklist |
