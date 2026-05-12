# Project Roadmap

The evolution of binGO-CLI organized by development phases.

## TODO

#### Phase 7.6: Custom Domain Setup
**Goal:** Point bingoserver.live to Fly.io for production shareable links

> **Sequence note:** Do this first — infrastructure only, no code changes. Unblocks proper share URLs (`https://bingoserver.live/room/:code`, `/game/:code`, `/bet/:code`) for all Phase 12 features.

**Tasks:**
- [ ] Register or configure bingoserver.live domain
  - Register domain (Namecheap, GoDaddy, etc.) if not already owned
  - Or verify you have admin access to existing domain

- [ ] Add Fly.io DNS records
  - Point domain's nameservers to Fly.io or add CNAME record
  - Fly.io DNS instructions: `flyctl certs create bingoserver.live`
  - This auto-provisions SSL/TLS certificate

- [ ] Verify SSL certificate
  - `flyctl certs list` to see certificate status
  - Visit https://bingoserver.live to confirm working

- [ ] Update README with production URL
  - Change references from bingo-server.fly.dev to bingoserver.live
  - Update shareable link examples

- [ ] PWA offline fallback (web client)
  - Add `manifest.json` and service worker via Vite PWA plugin (`vite-plugin-pwa`)
  - Cache static assets and last-visited game/room pages for offline display
  - Show a "You're offline" banner when WebSocket connection is lost rather than a blank screen



#### Phase 9.6: In-Game Chat
**Goal:** Let players send free-form text messages to everyone in the game during play

**Tasks:**
- [ ] Client command: `say <message>` (or a `/` prefix shorthand like `/hello everyone`)
  - Sends a `chat` action WebSocket message to the server
  - Server broadcasts `chat_message` event to all players in the game
- [ ] Server handler: `handlePlayerChat(game, player, text)`
  - Validate message is non-empty and within a reasonable length limit (e.g. 200 chars)
  - Broadcast `ServerMessage{Type:"chat_message", PlayerID: player.ID, Message: text}`
- [ ] Client display: chat messages printed inline below the board between redraws
  - Format: `💬 <username>: <message>`
  - On next full redraw (cell mark, player_update, etc.) the line scrolls away naturally
  - No persistent chat history panel — keeps the UI simple
- [ ] Help text updated with `say` command
- [ ] Rate-limit chat (e.g. 5 messages / 10 s per player) using existing rate-limit infrastructure

#### Phase 10: Kubernetes & Scaling (Future)
**Goal:** Run multiple server instances with shared database

**Cloud observability (Grafana Cloud) — Phase 10 prerequisite:**
Before scaling to K8s, establish a persistent observability layer:
- **Local dev:** `docker-compose up` → Prometheus scrapes `bingo-server:8080`, Grafana at `localhost:3000` (local only)
- **Staging / Production (Phase 10):** Grafana Cloud free tier (hosted Prometheus + Grafana). Scrapes `https://bingo-server-staging.fly.dev/metrics` and `https://bingo-server.fly.dev/metrics` directly. Free tier: 10k series, 14-day retention. **Status:** Load test passes, but Grafana Cloud scrape job not yet wired (no data appearing in dashboards). OTel tracing exporter misconfigured (localhost refs don't work in cloud).

**Tasks:**
- [ ] Grafana Cloud setup for staging & production (deferred from Phase 8)
  - Create free account at https://grafana.com/products/cloud/
  - Configure scrape job for `bingo-server-staging.fly.dev` and `bingo-server.fly.dev` with labels `env=staging` / `env=production`
  - Import `grafana-dashboards/bingo-dashboard.json` and set up alerting rules
  - Validate by running load test and confirming metric spikes appear in dashboards

- [ ] OTel tracing exporter swap for cloud (deferred from Phase 8)
  - Current: exporter tries `http://localhost:4318` (Tempo) — doesn't work in cloud
  - Fix: make `OTEL_EXPORTER_OTLP_ENDPOINT` configurable; use Grafana Cloud Tempo or Jaeger endpoint for staging/prod
  - Verify trace IDs flow end-to-end (game creation → DB write)

- [ ] PostgreSQL migration
  - Replace SQLite with PostgreSQL (same schema)
  - Use prepared statements for connection pooling
  - No app code changes needed (thanks to GameStore interface)

- [ ] Kubernetes deployment
  - Helm chart for server deployment
  - Persistent volume claims for PostgreSQL
  - Service mesh / ingress configuration
  - Horizontal pod autoscaling

- [ ] Distributed tracing (multi-pod debugging)
  - **Context**: OTel SDK + spans already in place from Phase 8 (bingo.ws.session, bingo.game.*, bingo.admin.*). Grafana Tempo running locally. Phase 10 only needs exporter swap.
  - Swap `OTEL_EXPORTER_OTLP_ENDPOINT` from Tempo → Jaeger or Grafana Cloud for prod
  - Trace game creation from client request → auth service → game service → DB write → response
  - Identify cross-pod bottlenecks and service latency breakdown
  - Debug session correlation (which pod handled which request)
  - Correlate traces with Phase 8 structured logs using trace IDs

- [ ] Self-hosted Prometheus & Grafana on K8s (replaces Grafana Cloud)
  - **Why:** Grafana Cloud free tier has 10k series / 14-day retention limits. Multi-replica K8s with PostgreSQL, tracing, and service mesh will exceed these. Self-hosted gives unlimited retention, custom recording rules, and Thanos for long-term storage.
  - Deploy Prometheus via `kube-prometheus-stack` Helm chart (bundles Prometheus, Grafana, Alertmanager, node-exporter)
  - Configure `ServiceMonitor` CRDs to auto-discover bingo-server pods (replaces static scrape targets)
  - Add Thanos sidecar for S3/GCS long-term metric storage beyond local TSDB retention
  - Migrate Grafana Cloud dashboards to self-hosted Grafana (export JSON → import)
  - Configure Alertmanager with PagerDuty/Slack integrations for production alerts
  - Add federation endpoint if running multiple Prometheus instances (one per namespace)
  - Correlate Prometheus metrics with OpenTelemetry traces using exemplars (trace ID links in Grafana panels)
  - Mirrors the `GameStore` interface pattern — observability backend is swappable without changing application instrumentation code (`bingo_*` metrics stay the same)

- [ ] Testing under K8s
  - Multi-replica game coordination
  - Database failover scenarios
  - Performance benchmarking under load with tracing insights

- [ ] Distributed load testing (replaces single-machine Go test at scale)
  - **Why:** `full_system_load_test.go` runs from a single machine — sufficient for Fly.io single-instance, but can't saturate multi-replica K8s from one client
  - Adopt k6 (Grafana OSS) or Grafana Cloud k6 for distributed load generation
  - Write k6 scripts mirroring the existing Go load test scenarios (game creation, WebSocket player lifecycle, concurrent marks)
  - Run k6 from multiple nodes or use Grafana Cloud k6 to generate load from distributed regions
  - Integrate k6 metrics with self-hosted Prometheus/Grafana for unified dashboards (load test results alongside app metrics)
  - Keep existing `full_system_load_test.go` for quick smoke tests; k6 for capacity planning and stress testing

#### Phase 12: Rooms, Live Bets & Bet Exchange
**Goal:** Persistent rooms hosting bingo games and a live prediction-bet exchange. Implemented in 6 incremental sub-phases.

**Design decisions:**
- Room code (`AB3K7`, 5-char) → game code `BINGO-AB3K7`. Existing standalone games keep their random `BINGO-XXXXX` codes for backward compat.
- Bingo game inside a room is created lazily on first `room_login`, not at room creation time.
- Existing Phase 9.5 in-game bet types (`Bet`, `BetCondition`) are renamed to `GameBet`, `GameBetCondition` before Phase 12.1 to avoid name collision.

**Code relationships:**
```
Room code:      AB3K7              (5 alphanumeric chars, no prefix)
Game code:      BINGO-AB3K7        (same chars, existing BINGO- prefix)
Bet code:       BET-AB3K7-X9Q2M   (room code + 5-char random suffix)
Branch bet:     BET-AB3K7-R7KP1   (same room prefix; parent_bet_code → BET-AB3K7-X9Q2M)
Side-bet room:  XK2P9              (separate room, linked_room_code = AB3K7)
```

---

##### Phase 12.0: Prerequisite — Rename GameBet types

- [ ] Rename `Bet` → `GameBet` and `BetCondition` → `GameBetCondition` in `server/types.go`
- [ ] Update all references in `server/server.go`, `server/game.go` (`Game.Bets []Bet` → `[]GameBet`)
- [ ] Update `web-client/src/lib/types.ts` and `web-client/src/App.tsx` to use renamed types
- [ ] Run `go test ./...` and web client build to confirm no regressions

---

##### Phase 12.1: Room Foundation
**Goal:** Rooms table, `Room` struct, room API, `room_login` / `room_welcome` WebSocket messages. No bets yet.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `rooms` table (`id`, `code` 5-char unique, `host_id`, `linked_room_code` nullable FK → rooms.code, `created_at`). Add optional `room_code` FK column to `games` table. Add `GameStore` methods: `CreateRoom`, `GetRoom`, `GetLinkedRooms`.
- [ ] **Server** (`server/room.go` — new file): `Room` struct (`Code`, `HostID`, `LinkedRoomCode`, `Game *Game` nil until first login, `mu sync.RWMutex`). `NewRoom(hostID, linkedRoomCode string)`. `GenerateRoomCode()` — 5-char alphanumeric, collision-checked. `getOrCreateRoom(code string)`. Add `Rooms map[string]*Room` + `RoomsMu sync.RWMutex` to `Server` struct.
- [ ] **HTTP** (`server/api.go`): `POST /api/rooms` (create room; returns `{code, game_code, linked_room_code}`). `GET /api/room/:code` (lobby snapshot: room info, game status, player count).
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): Add `room_login` client action (`{room_code, username, token}`). Add `room_welcome` server message (game status, open bets, players online, linked_room_code). Dispatch in server message handler.
- [ ] **Metrics** (`server/metrics.go`): Add `bingo_rooms_active` Gauge.
- [ ] **Logging** (`server/logger.go`): Add `RoomCreated(code, hostID string)`.
- [ ] **Tests** (`server/room_test.go`): `NewRoom`, `GenerateRoomCode` uniqueness and format (5-char alphanum), room creation API, `room_login` WS flow, lazy game creation on first login.

---

##### Phase 12.2: Simple Bets
**Goal:** DB-persisted bets in rooms — place, join sides, manual resolve by creator/host. No workers, no branching yet.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `bets` table (`id`, `code` unique e.g. `BET-AB3K7-X9Q2M`, `room_code` FK, `parent_bet_code` nullable, `creator_username`, `description` max 280 chars, `locked_at` nullable, `resolves_at`, `status` enum `open|locked|pending_resolution|disputed|won|lost|cancelled`, `created_at`, `resolved_at`, `dispute_deadline` nullable). Add `bet_positions` table (`id`, `bet_code` FK, `username`, `side` `for|against`, `joined_at`). Indexes: `idx_bets_room_code`, `idx_bets_resolves_at`, `idx_bets_status`, `idx_bets_parent`, `idx_positions_bet_code`, `idx_positions_username`. Add `GameStore` methods: `CreateBet`, `GetBetByCode`, `GetBetsByRoom`, `CreateBetPosition`, `GetBetPositions`, `ResolveBet`.
- [ ] **Bet code gen** (`server/room.go`): `GenerateBetCode(roomCode string) string` → `BET-<roomCode>-<5char>`, uniqueness-checked against DB.
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): Client actions: `place_bet` (`{description, resolves_at, locked_at?}`), `join_bet` (`{bet_code, side}`), `resolve_bet` (`{bet_code, outcome}` — creator/host only). Server messages: `bet_placed`, `bet_position_updated` (`{bet_code, for_count, against_count, new_position}`), `bet_resolved` (`{bet_code, outcome, resolved_by, dispute_deadline}`).
- [ ] **HTTP** (`server/api.go`): `POST /api/room/:code/bets`, `GET /api/room/:code/bets` (`?status=`), `GET /api/bet/:bet_code`, `POST /api/bet/:bet_code/join` (`{side}`), `PATCH /api/bet/:bet_code/resolve` (`{outcome}`).
- [ ] **Auth & validation**: `resolve_bet` restricted to bet creator or room host (HTTP 403). `join_bet` rejects duplicate position (HTTP 409). `description` sanitized: strip control chars, max 280 chars, reject empty.
- [ ] **Rate limiting** (`server/ratelimit.go`): 3 `place_bet` / 60 s per player. 10 `join_bet` / 60 s per player.
- [ ] **Metrics** (`server/metrics.go`): `bingo_bets_placed_total` Counter. `bingo_bet_positions_total` CounterVec (label: `side`). `bingo_bets_resolved_total` CounterVec (label: `outcome`).
- [ ] **Logging** (`server/logger.go`): `BetPlaced`, `BetPositionJoined`, `BetResolved`.
- [ ] **Web client** (`web-client/src/`): Add `/room/:code` route. Left panel: game status + "Join Game" button → `/game/BINGO-:code`. Right panel: bet feed with for/against counts, status badge, countdown. "Place a Bet" modal (description, resolves_at, optional locked_at). Join FOR/AGAINST buttons. Share button copies `https://bingoserver.live/bet/:bet_code`. Share button on room copies `https://bingoserver.live/room/:code`.
- [ ] **Tests**: full bet lifecycle; 403 on non-creator resolve; 409 on duplicate position; rate limit enforcement; DB integration tests.

---

##### Phase 12.3: Auto-Resolution Workers + Dispute
**Goal:** Lock worker, majority-vote resolution worker, 10-min dispute window, dispute expiry worker.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `GameStore` methods: `GetExpiredBets`, `GetLockedBets`, `DisputeBet`.
- [ ] **Background workers** (`server/room.go`): Three goroutines on 30 s tickers:
  - **Lock worker**: `status=open` + `locked_at ≤ now` → `locked`, broadcast `bet_locked`
  - **Resolution worker**: `status=locked` + `resolves_at ≤ now` → majority vote (tie → `for` side wins), set `dispute_deadline = now + 10 min`, broadcast `bet_resolved`
  - **Dispute expiry worker**: `status=disputed` + `dispute_deadline ≤ now` → re-apply majority outcome permanently, broadcast `bet_resolution_locked`
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): Client action: `dispute_bet` (`{bet_code}` — position holders only, within dispute window). Server messages: `bet_locked`, `bet_pending_resolution`, `bet_disputed`, `bet_resolution_locked`.
- [ ] **HTTP** (`server/api.go`): `POST /api/bet/:bet_code/dispute` — position holders only; HTTP 403 outside dispute window.
- [ ] **Metrics** (`server/metrics.go`): `bingo_bets_disputed_total` Counter. `bingo_bets_pending_resolution` Gauge. `bingo_bets_disputed_active` Gauge.
- [ ] **Logging** (`server/logger.go`): `BetLocked`, `BetPendingResolution`, `BetDisputed`, `BetResolutionLocked`.
- [ ] **Tests**: worker unit tests with mock clock for each worker; tie-break (for-side wins); full dispute flow; `go test -race ./server/` for concurrent `join_bet`.

---

##### Phase 12.4: Bet Branching
**Goal:** `parent_bet_code` FK, branch creation, `resolves_at` ≤ parent validation, bet tree display.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `GameStore` method: `GetBetTree(betCode string)` — returns bet + all descendants recursively.
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): Client action: `branch_bet` (`{parent_bet_code, description, resolves_at, locked_at?}`). Validate `resolves_at` ≤ parent's `resolves_at`. Server message: `bet_branched` (`{parent_bet_code, child_bet}`).
- [ ] **HTTP** (`server/api.go`): `POST /api/bet/:bet_code/branch`. Update `GET /api/bet/:bet_code` to include full branch tree.
- [ ] **Metrics** (`server/metrics.go`): `bingo_bets_branched_total` Counter.
- [ ] **Logging** (`server/logger.go`): `BetBranched(parentCode, childCode, creatorUsername string)`.
- [ ] **Web client** (`web-client/src/`): Add `/bet/:bet_code` route. Header: description, share code, status badge, countdown. Two-column position board (FOR green | AGAINST red), player names + joined timestamps. Branch tree below (child cards, click to navigate to `/bet/:child_code`). "Create Branch" modal (pre-filled, resolves_at capped to parent's). "Join FOR" / "Join AGAINST" (disabled when locked or already positioned). "Resolve" and "Dispute" buttons when applicable. Real-time updates via `bet_position_updated`, `bet_resolved`, `bet_disputed`, `bet_branched`.
- [ ] **Tests**: branch creation, `resolves_at` validation edge cases, `GetBetTree` recursion, web client navigation.

---

##### Phase 12.5: Side-Bet Rooms
**Goal:** `linked_room_code`, event forwarding from linked room to side rooms, `sidebet` CLI command.

- [ ] **Validation** (`server/api.go`): On `POST /api/rooms`, if `linked_room_code` provided: verify room exists (HTTP 404); reject circular links (HTTP 422).
- [ ] **Event forwarding** (`server/room.go`): `forwardEventToLinkedRooms(roomCode string, msg ServerMessage)` — queries `GetLinkedRooms(roomCode)`, broadcasts `linked_room_event` to each side room's connected players. Called on game events and bet events in the source room.
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): On `room_login`, if room has `linked_room_code`, send `linked_room_snapshot` (last 20 events from linked room). Server message: `linked_room_event` (`{source_room_code, event_type, payload}`).
- [ ] **CLI** (`client/player.go`): Add `sidebet` command in active game session — creates a new room with `linked_room_code` = current game's room code, prints share URL, game continues uninterrupted. Update `help` text.
- [ ] **Web client** (`web-client/src/`): "Create Side-Bet Room" button → `POST /api/rooms` with `linked_room_code`, navigate to `/room/:new_code`. If room has `linked_room_code`: collapsible "Referenced Room" panel (read-only, sourced from `linked_room_event` messages).
- [ ] **Logging** (`server/logger.go`): `LinkedRoomEventForwarded(sourceRoomCode, targetRoomCode, eventType string)`.
- [ ] **Tests**: event fan-out correctness, circular link rejection (HTTP 422), linked room snapshot delivery on login.

---

##### Phase 12.6: CLI Room Mode + Admin API
**Goal:** `-mode room` flag, full room lobby CLI, bet detail view, admin room endpoints.

- [ ] **bin.go**: Add `-mode room` flag. Dispatch to `runRoom(serverAddr, roomCode string)`.
- [ ] **`client/room.go`** (new file): Room lobby view — two-panel layout (game status left, live bet feed right with for/against counts + countdown timers refreshing every 1 s without full redraw). Lobby commands: `bet "<prediction>" <duration> [lock <duration>]` (e.g. `bet "Alice says synergy" 2h lock 90m`), `view <bet_code>`, `join` (switches to bingo game), `sidebet`, `help`. If room has linked room: interleaved `linked_room_event` messages prefixed with `[↩ <room_code>]`. Bet detail view (`view <bet_code>`): full-screen FOR/AGAINST player lists with timestamps, branch tree. Commands: `join for`, `join against`, `branch "<claim>" <duration>`, `resolve won`, `resolve lost`, `dispute`, `share`, `back`. Pending-resolution and disputed bets highlighted differently.
- [ ] **Admin API** (`server/admin.go`): `GET /admin/api/rooms` (list all rooms with game + bet counts). `DELETE /admin/api/rooms/:code` (force-close: cancel open bets, broadcast shutdown). `PATCH /admin/api/bets/:bet_code/force-resolve` (admin override for disputed bets).
- [ ] **Tests**: CLI room mode integration tests. Admin room endpoints (auth, list, delete, force-resolve).
