# Changelog

All notable changes to binGO-CLI are documented in this file.

## [Unreleased]

### Phase 8.10 - Security Hardening: Rate Limiting & DDoS Mitigation (2026-04-14)

#### Added
- **`server/ratelimit.go`** (new file) — all per-IP rate limiting logic extracted from `server.go`:
  - `wsConnLimitMiddleware` — HTTP middleware that counts active WebSocket connections per client IP; rejects the `(maxConnsPerIP+1)`th concurrent attempt with HTTP 429, logs a structured `WARN`, and increments `bingo_rate_limited_total{endpoint="ws"}`; `defer decrementConnCount` frees the slot when the connection closes
  - `getCodeLimiter(ip)` — returns (or creates) a per-IP `rate.Limiter` token bucket for game-code guesses; configured at `codeGuessPerWindow=5` burst / `codeGuessWindow=60s` refill (one token per 12 s)
  - `decrementConnCount(ip)` — atomically decrements the WS connection counter for an IP; cleans up the map entry at zero
  - `cleanupRateLimiters()` — evicts fully-replenished `CodeLimiters` entries and zero `ConnCounts` entries to prevent unbounded map growth
- **`server/server.go`** — security hardening changes:
  - `Server.ConnCounts map[string]int` + `ConnCountsMu sync.Mutex` fields — track live WS connections per IP
  - `Server.CodeLimiters map[string]*rate.Limiter` + `CodeLimitersMu sync.Mutex` fields — per-IP code-guess token buckets
  - `/ws` handler wrapped: `s.Mux.Handle("/ws", s.wsConnLimitMiddleware(websocket.Handler(s.wsHandler)))`
  - `extractClientIP` rewritten to take the **leftmost** comma-separated value from `X-Forwarded-For` (previously took the whole string verbatim, allowing header spoofing); trims whitespace
  - `handlePlayerConnect` — on invalid game code, consumes a limiter token; if exhausted, sends rate-limit error message to client, logs `RateLimitExceeded`, increments `bingo_rate_limited_total{endpoint="code_guess"}`, and returns early
  - `startCleanupRoutine` — now calls `s.cleanupRateLimiters()` on each cleanup tick
- **`server/metrics.go`** — added `RateLimitedTotal *prometheus.CounterVec` (labels: `endpoint` = `ws` | `code_guess`); registered in `NewMetrics()`, unregistered in `ResetMetrics()`; `RecordRateLimit(endpoint string)` convenience helper
- **`server/logger.go`** — added `RateLimitExceeded(ip, endpoint string, attemptCount int)` — emits `WARN`-level structured JSON event with `event_type: "rate_limit_exceeded"`, `ip`, `endpoint`, and optional `attempt_count`

#### Tests
- **`server/ratelimit_test.go`** (new file) — 14 unit tests covering all rate-limit paths:
  - `TestExtractClientIP_*` — multi-hop XFF, single IP, empty header, direct remote addr
  - `TestWsConnLimitMiddleware_*` — allows up to `maxConnsPerIP`, blocks 6th, concurrent safety
  - `TestGetCodeLimiter_*` — returns same limiter per IP, blocks after exhaustion, per-IP isolation
  - `TestDecrementConnCount_*` — below zero guard, map cleanup at zero
  - `TestCleanupRateLimiters_*` — removes full limiters, keeps partially-consumed ones
  - `TestMetrics_RecordRateLimit` — correct label increments
- **`tests/container_regression_test.go`** — two new Testcontainers regression tests:
  - **14.1 `TestRegressionWSConnLimit`** — opens `maxConnsPerIP` real WebSocket connections, holds them open, asserts the 6th attempt gets HTTP 429, verifies `bingo_rate_limited_total{endpoint="ws"}` is present in `/metrics`, and confirms `rate_limit_exceeded` appears in container logs
  - **14.2 `TestRegressionCodeGuessRateLimit`** — sends `codeGuessPerWindow` bad-code login attempts (each receives a normal error), asserts the `(codeGuessPerWindow+1)`th attempt receives the rate-limit message, verifies `bingo_rate_limited_total{endpoint="code_guess"}` in `/metrics`, and confirms `rate_limit_exceeded` in container logs

#### Dependencies
- `golang.org/x/time` upgraded to v0.15.0 (direct — `rate.Limiter` token bucket)

---

### Phase 8.9 - Context Propagation, Error Wrapping & OpenTelemetry Tracing (2026-04-13)

#### Changed
- **`db/sqlite.go`** — `NewSQLiteStore` signature changed to `NewSQLiteStore(ctx context.Context, dbPath string)`; `db.Ping()` replaced with `db.PingContext(ctx)`; 5 bare `fmt.Errorf("... not found")` branches (GetGameByCode, GetGameByID, GetPlayerByID, GetHost, GetHostByUsername) now wrap `sql.ErrNoRows` with `%w` so callers can use `errors.Is`
- **`server/db.go`** — `DBConfig.Close()` signature changed to `Close(ctx context.Context)` and passes ctx to `store.Close`; `NewDBConfig` uses `context.WithTimeout(ctx, 30s)` for store init and ping
- **`server/server.go`** — major context threading refactor: `ctx` flows from WebSocket session root (`wsHandler`) through `handlePlayerConnect` → `createPlayerInGame` → `processPlayerMessage` → `handlePlayerWin` → `archiveGame` and `markGameOrphaned` → `handlePlayerDisconnect`; all 5 DB call sites wrapped in `context.WithTimeout` (3s for game operations); stoppable cleanup goroutine (closes on `cleanupStop` channel signal from `Stop()`); `startHTTPServer` wraps `s.Mux` with `otelhttp.NewHandler` for automatic HTTP span extraction
- **`server/admin.go`** — all 4 handlers now extract `r.Context()` and start OpenTelemetry child spans (`bingo.admin.createGame`, `bingo.admin.listGames`, `bingo.admin.getGame`, `bingo.admin.deleteGame`)
- **`bin.go`** — shutdown ctx passed to `dbConfig.Close(ctx)`; `InitTracer` called between DB init and signal wait; `shutdownTracer(ctx)` called after DB close during graceful shutdown
- **`docker-compose.yml`** — added `OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo:4318` to `bingo-server` env; added `tempo` service (`grafana/tempo:latest`, OTLP HTTP port 4318, query port 3200); added `tempo-data` persistent volume
- **`grafana-provisioning/datasources/datasources.yml`** — added `uid: bingo-prometheus` to existing Prometheus datasource; added Tempo datasource (uid: `bingo-tempo`, url: `http://tempo:3200`) with `tracesToMetrics` and `serviceMap` links back to Prometheus

#### Added
- **`server/tracing.go`** (new file) — `InitTracer(srv *Server) (func(context.Context), error)` bootstraps an OTLP/HTTP trace exporter; reads `OTEL_EXPORTER_OTLP_ENDPOINT` env var (default `http://localhost:4318`); strips scheme and applies `WithInsecure()` for `http://`; sets global TracerProvider with `AlwaysSample()`; calls `srv.SetTracer(tp.Tracer("bingo-server"))`; returns a shutdown flush func. Swapping to Jaeger/Grafana Cloud for Phase 10 is a one-env-var change.
- **`Server.Tracer trace.Tracer`** field — defaults to a no-op tracer; set via `SetTracer`; used by all lifecycle functions and admin handlers
- **`Server.cleanupStop chan struct{}`** field — closed by `Stop()` to signal the background cleanup goroutine to exit without waiting for the next ticker tick
- **`server/logger.go`** — `SpanDetails(ctx context.Context) map[string]interface{}` extracts `trace_id` + `span_id` from the active span for structured log correlation; `mergeMaps` helper for composing log detail maps
- **OpenTelemetry spans** on all major lifecycle events: `bingo.ws.session`, `bingo.game.create`, `bingo.player.create`, `bingo.game.win`, `bingo.game.archive`, `bingo.game.restart`, `bingo.admin.*`
- **`tempo.yml`** (new file) — Grafana Tempo single-binary config: OTLP receivers on gRPC 4317 + HTTP 4318, local block storage at `/var/tempo/blocks`, 48h retention

#### Dependencies
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` promoted to direct dependency (v1.41.0)
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` promoted to direct dependency
- `github.com/cenkalti/backoff/v5` and updated gRPC/genproto/golang.org/x transitive deps added by otlptracehttp

#### Tests
- All existing unit tests pass (`go test ./...`)
- All integration tests pass (`go test -tags=integration ./tests/...`)
- `db/sqlite_test.go` — 4 `NewSQLiteStore` call sites updated to pass `context.Background()`
- `server/server_test.go` — 11 call sites updated (`handlePlayerWin`, `handleGameRestart`, `markGameOrphaned`, `db.NewSQLiteStore`) to pass `context.Background()`
- `tests/db_integration_test.go` — 9 `db.NewSQLiteStore` call sites updated to pass `context.Background()`

---

### Post-v8.2.0 Updates (2026-04-12)

#### Fixed
- **WebSocket `wss://` for Fly.io connections** (`c15d106`) — `client/player.go` now auto-detects `wss://` for `fly.dev` hostnames (previously only handled `ngrok` URLs), fixing connection failures to staging/production servers

#### Added
- **Lefthook container test skip optimization** (`c8ce3a0`) — container tests (~120s) are now skipped during `git push` when only non-build files change (docs, YAML, markdown). Uses `git diff --name-only HEAD @{push}` to check for `.go`, `.mod`, `.sum`, or `Dockerfile` changes. Also skips on merge/rebase commits. Verified with positive (`.go` change → container ran 125s) and negative (docs-only → container skipped 0.06s) test cases

#### Changed
- **`docs/ROADMAP.md`** (`58d0613`) — added observability architecture decision (Grafana Cloud free tier for staging/prod, self-hosted Prometheus for dev only, full self-hosted stack for Phase 10 K8s); added detailed monitoring tasks to Phase 8 and Phase 10; added load test configurability and distributed load testing (k6) tasks

---

### v8.2.0 — CI/CD Pipeline & Deployment Fixes (2026-04-11)

#### Fixed
- **Dagger deploy pipeline** — 6 iterative fixes to get Fly.io deployment working from Dagger:
  - Replaced `flyio/flyctl` Docker image with self-installed `flyctl` on Alpine (`bdc287a`)
  - Fixed `flyctl` argument ordering — init wrapper execs it (`0d05457`)
  - Removed `--wait-timeout` flag, unsupported in flyctl version (`e08cb8a`)
  - Fixed duplicate `"flyctl"` prefix in `WithExec` args (`9c927eb`)
  - Added `--wait-timeout=300` and HTTP health check config to `fly.toml`/`fly.staging.toml` (`348e8c5`)
- **CI workflow YAML** (`6d2fd63`) — fixed duplicate env key and shell syntax errors in `.github/workflows/ci.yml`
- **CI test job** (`81ce923`) — fixed failing CI test job configuration
- **Container test pipeline** (`9beb8f3`) — `test-container` Dagger function now correctly resolves project root relative to script CWD
- **SIGTERM flush race** (`5184110`) — fixed race condition in `NotifyShutdown` where shutdown messages could fail to flush before WebSocket close
- **Orphan-after-win double-archive** (`5184110`) — prevented duplicate `ArchiveGameInDB` calls when all players disconnect from a game that already had a winner
- **Multiplayer test logins** (`09052b6`) — all multiplayer tests now include required game code in login messages

#### Changed
- **`docs/DEVOPS.md`** (`10dbb1b`) — updated CI/CD and testing documentation to reflect Dagger/Lefthook pipeline

#### Tests
- Lefthook pre-push hook verification (`723472c`) — confirmed hooks fire correctly on `git push`

---

### Phase 8.8 - Dagger CI/CD Pipeline + Lefthook Guardrails (2026-04-11)

#### Philosophy
Automate guardrails as far as possible — if an SOP can be enforced by tooling, it should be. The pipeline is the mechanism; Lefthook is the enforcement trigger. Every `git push` runs the full test suite (unit + integration + container regression) locally before code reaches the remote. CI then runs the same Dagger functions server-side, ensuring identical behavior locally and in CI. Container tests (SIGTERM handling, volume persistence, cleanup goroutines) are complementary to Dagger's build+deploy coverage — they catch behavioral regressions that a deploy pipeline cannot.

#### Added
- **`dagger/main.go`** — Dagger Go SDK pipeline replacing all GitHub Actions jobs. Functions: `test`, `test-container`, `build`, `publish`, `deploy`, `release`, `all`
  - `go run ./dagger test` — unit + integration tests in CGO+SQLite container (~30s)
  - `go run ./dagger test-container` — container regression suite via Docker socket (~10min)
  - `go run ./dagger build --version <tag>` — builds Docker image with version injection
  - `go run ./dagger publish --version <tag>` — pushes to `ghcr.io/jkmlnop/bingo-cli`
  - `go run ./dagger deploy --env staging|production` — deploys to Fly.io
  - `go run ./dagger release --version v8.x.x` — cross-compile macOS Intel + Linux, GitHub Release
  - `go run ./dagger all --env staging` — full pipeline: Test → Build → Publish → Deploy
- **`dagger/main_test.go`** — pipeline unit tests: env routing validation, constants, help text
- **`dagger/go.mod`** — separate Go module isolating Dagger SDK dependency from main module
- **`.lefthook.yml`** — Git pre-push hooks enforcing `test` + `test-container` before every push. Install: `go install github.com/evilmartians/lefthook@latest && lefthook install`. Bypass: `git push --no-verify`
- **`fly.staging.toml`** — Fly.io staging config for `bingo-server-staging` app
- **`docs/DEVOPS.md`** — documents DevOps philosophy, tool roles, test tiers, local workflow, secrets setup, and Fly.io one-time setup
- **`var version = "dev"` in `bin.go`** — build-time version injection via `-ldflags "-X main.version=<value>"`. `-version` flag prints version and exits

#### Changed
- **`Dockerfile`** — added `ARG VERSION=dev` and `ARG GO_VERSION=1.25.3`; version injected into binary via `-ldflags`; `FROM golang:${GO_VERSION}-alpine` now consumes the build arg (was hard-coded)
- **`.github/workflows/ci.yml`** — replaced all CI jobs with thin Dagger triggers:
  - PR to `main` → `go run ./dagger test` (fast feedback, no deploy)
  - Push to `main` → `go run ./dagger all --env staging` (test → build → publish → deploy to staging)
  - Tag `v*` → `go run ./dagger all --env production` + `go run ./dagger release` (prod deploy + GitHub Release)

#### Environments
- **Staging:** `bingo-server-staging.fly.dev` — deployed on every push to `main`
- **Production:** `bingo-server.fly.dev` — deployed on `v*` tags
- **Registry:** `ghcr.io/jkmlnop/bingo-cli` — reusable for Phase 10 K8s migration

#### Required Setup (one-time)
- GitHub secret: `FLY_API_TOKEN`
- `flyctl apps create bingo-server-staging --org personal`
- `flyctl volumes create bingo_data --region sjc --app bingo-server-staging`
- `flyctl secrets set ADMIN_API_KEY=<key> --app bingo-server-staging`

#### Tests
- `TestFlyConfigStaging` — staging env resolves to correct app name + config file
- `TestFlyConfigProduction` — production env resolves correctly
- `TestFlyConfigInvalid` — 6 invalid env names all return errors
- `TestRegistryBase` — registry constant matches expected format
- `TestDefaultGoVersion` — Go version constant is set and correct
- `TestPrintUsageDoesNotPanic` — help text renders without panic

---

### Phase 8.7 - Real Error Metrics for Prometheus (2026-03-04)

#### Fixed
- **`bingo_errors_total` was never created** — the metric was referenced in `alert-rules.yml`, both Grafana dashboard JSONs, and `docs/MONITORING_SETUP.md` but didn't exist in `metrics.go`. The Grafana panel fell back to a fake formula (`increase(bingo_admin_api_requests_total[5m]) * 0.11`).

#### Added
- **`Metrics.ErrorsTotal *prometheus.CounterVec`** — labeled by `error_type`; registered in `metrics.go` alongside all other metrics; unregistered in `ResetMetrics()`
  - `auth` — invalid/expired JWT token on WebSocket connect
  - `input` — missing game code or missing username/token in login message
  - `game` — invalid game code, orphaned/deleted game, already-ended game, non-host restart attempt, host disconnected
  - `db` — failed writes to SQLite (save game, record player, record win, archive game)
  - `ws` — failed WebSocket send in `forwardPlayerMessages`
- **`Metrics.RecordError(errorType string)`** helper method — single call site used throughout `server.go`
- **11 `RecordError` call sites** added across `server.go`: `authenticatePlayer`, `handlePlayerConnect`, `getOrCreateGame`, `handlePlayerWin`, `handleGameRestart`, `archiveGame`, `createNewGame`, `createPlayerInGame`, `forwardPlayerMessages`

#### Changed
- **Both Grafana dashboard JSONs** (`grafana-dashboards/bingo-dashboard.json` and `grafana-provisioning/dashboards/bingo-dashboard.json`): Error Rate panel now queries `rate(bingo_errors_total[5m])` with `legendFormat: "{{error_type}}"` (one line per error type); panel title changed from `"Error Rate (5-min average) - TODO: Fix real error metrics"` to `"Error Rate (5-min average)"`

#### Tests
- `TestErrorMetricInvalidGameCode` — `error_type="game"` increments by 1 on unknown code lookup
- `TestErrorMetricAlreadyEndedGame` — `error_type="game"` increments by 1 on second win attempt
- `TestErrorMetricNonHostRestart` — `error_type="game"` increments by 1 when non-host tries to restart
- `TestErrorMetricScrapeable` — triggers an error, then serves `/metrics` via `httptest` and asserts `bingo_errors_total{error_type="game"} 1` appears in the Prometheus scrape response body

---

### Phase 8.6 - Game Lifecycle Management (2026-03-04)

#### Changed
- **`handlePlayerWin` behavior change**: `IsActive` is no longer set to `false` on win — only `game.Winner` is set and `game.EndedAt` is recorded; `IsActive=false` is now reserved exclusively for admin-deleted or orphaned games, which allows the host to restart a won game without admin intervention
- **Duplicate-win guard improved**: condition changed from `!game.IsActive` to `!game.IsActive || game.Winner != ""` so a second win announcement is rejected even though the game stays active

#### Added
- **Orphaned game detection**: When the last player disconnects from a game that has no winner, `handlePlayerDisconnect` now calls `markGameOrphaned()` instead of silently leaving the game in `s.Games` as active
  - `Game.Orphaned bool` field added to distinguish orphaned games from admin-deleted ones
  - `markGameOrphaned()`: sets `IsActive = false`, `Orphaned = true`, `EndedAt = time.Now()`, logs the event, and archives the game (with empty `winner_id`)
- **Clearer join error for orphaned games**: `getOrCreateGame()` now returns `"game <CODE> has ended: all players disconnected"` instead of the admin-deleted message when a player tries to join a code whose game was orphaned
- **`Player.SetWS(*websocket.Conn)`**: New method to store the active WebSocket on a player; called at the end of `handlePlayerConnect` so `NotifyShutdown` can close connections cleanly
- **`Server.NotifyShutdown()`**: Broadcasts a `server_shutdown` message to every connected player, then closes their WebSocket connections so the HTTP server can drain within its shutdown timeout; logs the number of players notified
- **SIGTERM handling in `bin.go`**: `signal.Notify` now catches both `os.Interrupt` (Ctrl-C) and `syscall.SIGTERM` (Docker/k8s `docker stop`); `srv.NotifyShutdown()` is called before `srv.Stop(ctx)`

#### Tests
- `TestOrphanedGameMarkedOnLastDisconnect`: Verifies `markGameOrphaned` sets `IsActive=false`, `Orphaned=true`, and non-zero `EndedAt`
- `TestOrphanedGameNotJoinable`: Verifies `getOrCreateGame` returns the correct error message for an orphaned code (not the admin-deleted one)
- `TestNotifyShutdownDoesNotPanicWithNilWS`: Verifies `NotifyShutdown` delivers `server_shutdown` messages to both players and does not panic when `player.ws` is nil (unit-test environment)
- `TestPartialDisconnectDoesNotOrphan`: Verifies the `playerCount == 0` guard — a game with remaining players is never orphaned when a single player leaves
- `TestWinnerGameNotOrphaned`: Verifies a game ended by a win has `Orphaned=false` and `IsActive=true` (only the orphan/admin-delete path sets `IsActive=false`)
- `TestOrphanedGamePreservesHostID`: Verifies `HostID` remains unchanged after `markGameOrphaned` (HostID is always immutable)
- `TestOrphanedGameArchivesToDB` (integration): Creates an orphaned game state (`Winner=""`), calls `ArchiveGameInDB`, and verifies the DB row has an empty `winner_id`; also confirms a normal win archive stores the correct `winner_id` in the same test (contrast check)
- `TestHandlePlayerWinArchivesGameToDB`: End-to-end unit test verifying the full `handlePlayerWin` → `archiveGame` → `ArchiveGameInDB` path writes a row to `game_archives` with correct `code`, `host_id`, `winner_id`, and `player_count`
- `TestAdminKeyMiddlewareEnvVar`: Validates that `ADMIN_API_KEY` env var overrides the hardcoded default; covers custom key accepted, default key rejected when custom is set, default key works when env var is empty, and wrong key always rejected
- **Container E2E tests** (`tests/container_e2e_test.go`, build tag `container`): 6 Testcontainers-based tests automating former manual Docker-stack regression checks
  - `TestContainerAdminKeyCustom` — custom `ADMIN_API_KEY` env var displaces default key
  - `TestContainerAdminKeyFallback` — absent env var falls back to hardcoded dev key
  - `TestContainerSIGTERMNotifiesClients` — `docker stop` sends `server_shutdown` to connected players
  - `TestContainerOrphanedGame` — all players disconnect → orphan log line + join error
  - `TestContainerVolumeArchivePersistence` — win archived to SQLite; DB row survives container restart
  - `TestContainerCleanupGoroutine` — stale archive rows (>4 days) deleted on startup
- **Container regression tests** (`tests/container_regression_test.go`, build tag `container`): 8 Testcontainers-based regression tests
  - `TestRegressionCleanupRecentSurvives`, `TestRegressionMultiWinArchive`, `TestRegressionAdminAuthMatrix`, `TestRegressionAdminCreateGame`, `TestRegressionAdminListGames`, `TestRegressionAdminGetDeleteGame`, `TestRegressionAdminStatusCodes`, `TestRegressionAdminConcurrency`, `TestRegressionZeroPlayerShutdown`

---

### Phase 8.5 - Game Archiving (2026-03-04)

#### Added
- **`game_archives` table**: New SQLite table persisting completed game sessions
  - Schema: `id, game_id, code, host_id, winner_id, player_count, created_at, ended_at`
  - Indexed on `ended_at` for efficient age-based cleanup queries
- **`GameStore.ArchiveGame()`**: New interface method (and SQLiteStore implementation) for writing a game archive record
- **`GameStore.CleanupOldArchives()`**: New interface method (and SQLiteStore implementation) that deletes records older than 4 days
- **`ArchiveGameInDB()` helper** in `server/db.go`: nil-safe wrapper used by the server
- **Background cleanup goroutine**: Started in `Server.Start()`, runs `CleanupOldArchives` immediately on startup then every hour; logs count of deleted records when > 0
- **`GameArchive` struct** in `db/store.go`: New type representing a persisted completed game session

#### Changed
- **`archiveGame()`** in `server/server.go`: Now writes to `game_archives` table via `ArchiveGameInDB()` and increments the `bingo_game_archived_total` Prometheus counter, replacing the previous in-memory append
- **`Server` struct**: Removed in-memory `ArchivedGames []ArchivedGame` slice (was temporary scaffolding)
- **`game.go`**: Removed `ArchivedGame` struct (no longer needed)

#### Tests
- `TestArchiveGame`: Verifies two archive entries can be persisted without error
- `TestCleanupOldArchives`: Verifies records older than 4 days are deleted while recent records are kept; second cleanup run returns 0
- `TestArchiveGameIntegration` (integration): Verifies the full `ArchiveGameInDB` + `CleanupOldArchives` path together — archives a game, inserts an old record, runs cleanup, confirms only the old record is deleted

---

### Phase 8.4 - Production Credentials Setup (2026-03-04)

#### Added
- **`.env.example` template**: Documents all supported environment variables with safe placeholder defaults
  - `GRAFANA_USER` / `GRAFANA_PASSWORD` — Grafana UI credentials
  - `ADMIN_API_KEY` — secret for authenticating admin API requests
  - `LOG_LEVEL` — server log verbosity
  - Each variable annotated with purpose and production guidance
- **`.gitignore` update**: `.env` added to prevent accidental credential commits
- **`docker-compose.yml` update**: `ADMIN_API_KEY` and `LOG_LEVEL` now use `${VAR:-fallback}` syntax so values flow from `.env` without breaking existing local setups
- **`docs/MONITORING_SETUP.md` credentials section**: Replaced vague local-dev note with full variable reference table, `cp .env.example .env` workflow, `openssl rand -hex 32` guidance for production keys, and explanation of why the fallback key is intentionally weak
- **`Dockerfile`**: Added `sqlite` CLI package (`apk add sqlite`) to the final Alpine image for debugging and testing inside the container
- **`docs/ROADMAP.md`**: Updated Phase 8 — checked off error metrics fix; removed completed game archiving and production credentials tasks; added context propagation & error wrapping audit; added Dagger pipeline testing notes
- **`tests/README.md`**: Added container E2E test section with run instructions (`go test -tags=container -timeout=10m ./tests -v`), prerequisites, and test-to-regression-test mapping table
- **`tests/REGRESSION_TESTS.md`**: Restructured — added automated test note at top; new Section 12 (Production Credentials via `.env`/docker-compose); simplified Section 11 (Admin API) to integration-with-gameplay tests only; rewrote Section 7 as "Database Persistence (Phase 8.5)"

#### Fixed
- **`tests/multiplayer_test.go`**: Changed `localGame.Board.MarkCell(move)` to `localGame.MarkCell(move)` to match updated API

#### Dependencies
- **`go.mod` / `go.sum`**: Added `testcontainers-go v0.40.0`, `docker/docker v28.5.1`, and transitive dependencies (Docker SDK, containerd, OCI, Moby, etc.) for container-based tests

---

### Phase 8.3 - Multi-Game Stability Testing (2026-02-10)

#### Added
- **E2E test framework**: New `// +build e2e` test class for container-dependent tests
  - Requires Docker stack running (`docker-compose up`)
  - Run with: `go test -tags=e2e ./tests`
  - Distinguishes from integration tests which don't require external infrastructure
- **Comprehensive load test**: `tests/full_system_load_test.go` with 4 phases:
  - Phase 1: Creates 10 concurrent games via Admin API
  - Phase 1.5: Generates realistic error scenarios (invalid auth, invalid game codes)
  - Phase 2: Connects 50 players across games, records 250 marks
  - Phase 3: Verifies game state consistency across instances
  - Phase 4: Validates metrics collection (games created, admin requests)
- **Test results**: Achieves 172.28 players/sec throughput with zero data corruption
- **Grafana monitoring integration**: Load test runs with Prometheus scraping for real-time dashboard observation
- **Error scenario simulation**: Test Phase 1.5 hammers admin API with invalid keys and game codes to measure error handling

#### Fixed (Partial)
- **Error rate gauge display**: Now shows simulated data based on admin API request volume
  - Query: `increase(bingo_admin_api_requests_total[5m]) * 0.11`
  - Note: Currently displays calculated estimate, not real error metrics (see TODO below)
  - TODO: Implement real Prometheus error counter metrics that properly export from server

### Phase 8.1 - Admin API for Testing & Game Management (2026-02-08)

#### Added
- **Admin API middleware**: X-Admin-Key header validation against ADMIN_API_KEY env var with secure dev key default
- **Game CRUD endpoints**:
  - `POST /admin/api/games` - Create new game with optional player list
  - `GET /admin/api/games` - List all active games with state
  - `GET /admin/api/games/{id}` - Retrieve detailed game state and players
  - `DELETE /admin/api/games/{id}` - Force close a game
- **Admin API documentation**: Comprehensive [docs/ADMIN_API.md](docs/ADMIN_API.md) with curl examples and workflows
- **Test suite**: 6 integration tests covering authentication, CRUD operations, and routing
- **Integration with monitoring**: Admin operations logged with structured JSON and integrated with metrics endpoints
- **Hybrid README documentation**: Quick start Admin API section in main README with link to full docs

#### Fixed
- **Game deletion enforcement**: Deleted games now properly prevent new players from joining, hosts from restarting, and players from winning
  - `getOrCreateGame()` checks `is_active` and rejects deleted games at connection time
  - `handleGameRestart()` checks `is_active` and prevents restart attempts on deleted games  
  - `handlePlayerWin()` already validated `is_active` preventing wins on deleted/ended games
- **Player notification on deletion**: Connected players now receive broadcast message when game is deleted by admin
  - Message: "⚠️ Game has been closed by admin. Play can continue but the game cannot be won or restarted."
  - Uses same notification pattern as host disconnect to maintain consistent UX
  - Broadcast happens atomically with deletion (before API response)

### Bug Fixes (2026-02-07)

#### Fixed
- **Post-Game Prompt UX**: Fixed issue where error messages after game end still showed cell marking prompts - now displays "Game has ended. Type 'q' to quit." when appropriate
- **Duplicate Game Archiving**: Removed duplicate archive call when game restarts - games are now archived only once when the game ends, not again on restart
- **Dead 'board' Command**: Removed non-functional 'board' input case from client that only redrew an already-visible board with no additional value
- **Username Impersonation Vulnerability**: Reject any attempt to join as an existing player in the game, preventing account hijacking
- **Host Disconnect Messaging**: Fixed incorrect message type causing old board marks to display when host disconnects - now uses `error` type to avoid board redraw
- **Duplicate Win Announcements**: Added check to prevent win announcements when game is already ended, sending error feedback to client instead of silently logging
- **Dead Code Removal**: Removed manual `win` command from client interface (wins already announce automatically on mark detection)

### Phase 8.2 - Host Tracking Simplification (2026-02-04)

#### Fixed
- **HostID Immutability**: Removed `game.HostID = ""` mutation on host disconnect - host now retains immutable ID for reconnection
- **Reconnection Detection**: Added check to detect returning players and reuse existing player object instead of triggering collision errors  
- **Host Connection Status**: Check if host is connected before showing restart prompt - non-hosts now see accurate "Host disconnected" message when applicable

#### Added
- 5 new unit tests for host disconnect/reconnection scenarios
- 4 new integration tests for E2E host disconnect validation
- Comprehensive test coverage for immutable host ID principle

### Added

#### Game Restart & Host Management (Phase 7.3)
- **Game restart functionality**: Host can restart the game after it ends, resetting the board with new buzzwords while maintaining the game code
- **Host-only privileges**: Only the original host can trigger a restart; non-host players see a waiting message
- **Host reconnection**: Hosts can reconnect after disconnection and retain their original host status
- **Orphaned game detection**: Non-hosts are notified when the host disconnects and cannot restart the game

#### Game Archiving (Phase 7.4)
- **Server-side archiving**: Completed games are archived automatically for record-keeping
- **Code persistence across sessions**: Game codes remain valid indefinitely within a server session and across game restarts
- **Archived game logging**: Server logs all completed games with game ID and code for auditing

#### Production Database & Cloud Deployment (Phase 7.5)
- **SQLite database**: Persistent game storage with schema for hosts, games, players, and win history
- **Abstract database layer**: Interface-based design enabling future PostgreSQL/K8s migration
- **REST API**: `/api/game/:code`, `/api/leaderboard?limit=10`, `/api/status` endpoints
- **Integration tests**: 7/7 database persistence & API tests passing
- **Command-line database flag**: `-db <path>` enables optional persistence
- **Docker containerization**: Multi-stage build with ~50MB runtime image
- **Fly.io deployment**: Production server at https://bingo-server.fly.dev/ with persistent volume

#### Observability & Monitoring (Phase 8.1)
- **Prometheus metrics endpoint**: `/metrics` exposes server metrics including:
  - `game_count` (total active games)
  - `player_count` (total connected players)
  - `game_creation_duration_ms` (latency histogram)
  - `database_query_duration_ms` (DB performance histogram)
- **Structured JSON logging**: Comprehensive event logging with timestamps and metadata for:
  - Game lifecycle events (created, ended, restarted, archived)
  - Player connection events (joined, disconnected, errors)
  - Database performance issues
- **Grafana dashboards**: Pre-configured dashboards visualizing:
  - Games created per minute
  - Average players per game
  - Error rate & error types
  - Database query latency percentiles (p50, p95, p99)
- **Alert rules**: Prometheus AlertManager rules for:
  - Error rate > 5%
  - Game creation latency p95 > 500ms
  - Database latency p95 > 250ms
- **Local validation**: Docker Compose setup for testing observability stack locally before production deployment

#### Simplified Host Tracking Architecture (Phase 8.2)
- **Single immutable HostID**: Replaced dual `HostID` + `OriginalHostID` with single immutable host identifier set on first player connect
- **Removed IP classification system**: Eliminated `ClassifyIP`, `IsLocalConnection`, and `IPType` - no more IP-based connection logic
- **Unified game creation**: All players (local and remote) now require a game code - no localhost/LAN auto-join logic
- **Simplified protocol**: Removed `OriginalHostID` field from `ServerMessage` struct for cleaner protocol
- **Foundation for host privileges**: Architecture now supports future host-specific features (buzzword approval, host-only settings) without complex reconnection logic
- **Cleaner codebase**: Reduced complexity by removing ~200 lines of IP-based routing code

#### ngrok Support
- **Internet multiplayer**: Remote players can now connect via ngrok tunnels using a game code
- **Secure WebSocket (WSS)**: Automatic protocol detection for ngrok domains (wss://) vs. local (ws://)
- **Game code authentication**: Remote connections require a valid game code for security

#### JWT Token Authentication
- **IP-bound tokens**: Authentication tokens are bound to the client's IP address, preventing token hijacking
- **Session-specific tokens**: Tokens include session information and expiration timestamps
- **Automatic re-authentication**: Players can reconnect using saved tokens without re-entering credentials

#### Display & UX Improvements
- **Updated help text**: `help` command now includes the `restart` command with description
- **Player list updates**: Real-time player list synchronization across all connected clients
- **Clear game state messages**: Status messages for join, mark, win, and restart events

### Testing
- **Comprehensive regression tests**: 49 manual test cases covering 11 functional areas
- **Test categories**: Server initialization, ngrok connectivity, multiplayer gameplay, win detection, game restart, host disconnect behavior, game archiving, edge cases, code validity, display/UX, and backwards compatibility
- **All tests passing**: ✅ 49/49 tests completed and verified

See [tests/REGRESSION_TESTS.md](tests/REGRESSION_TESTS.md) for complete test documentation.

### Fixed
- Multiple display rendering issues during game transitions
- Player list synchronization delays
- Board state management during reconnections

### Known Limitations
- **Game codes not persisted to disk**: Codes are valid for the current server session only. Restarting the server generates a new code. (Phase 7.5+ feature for persistent code storage)
- **Archives in-memory only**: Game archives are lost when the server restarts. (Phase 8+ feature for database persistence)
- **No archived game UI**: No interface for viewing or replaying archived games. (Future enhancement)

---

## Versioning

This project uses semantic versioning. Releases are tagged and built automatically via GitHub Actions.

To create a release:
```bash
git tag v1.0.0
git push origin v1.0.0
```
