# Project Roadmap

The evolution of binGO-CLI organized by development phases.

## TODO

#### Phase 7.6: Custom Domain Setup
**Goal:** Point bingoserver.live to Fly.io for production shareable links

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
**Goal:** Persistent rooms that host a bingo game and a live prediction-bet exchange, all visible in a shared lobby. Anyone can take either side of any bet; bets are shareable by code; temporal branches form a navigable tree.

**Design Overview:**

A **Room** is a named space with a short code (e.g. `AB3K7`, no prefix). The single bingo game reuses the same base code with the existing `BINGO-` prefix (`BINGO-AB3K7`).

A **Bet** is a free-text prediction. The bet creator is automatically the first player on the `for` side; any room member can join `for` or `against`. A bet can be **branched** to create a child bet (same claim, narrower time window or scope) — the child carries a `parent_bet_code` FK and forms a navigable tree.

Each bet has a **share code** of the form `BET-<roomcode>-<5char-hash>` (e.g. `BET-AB3K7-X9Q2M`). The room code is embedded so the server can route to the correct room without a separate lookup, and the hash makes the code short and URL-safe. Positions (who is on which side) are a first-class table, not metadata on the bet row — this enables the exchange model cleanly.

**Lock time vs. resolve time:** `locked_at` is when new positions close (no more joining sides); `resolves_at` is when the outcome is evaluated. These are independent so bettors can't join at the last second with full information.

**Resolution:** All bets resolve by **majority vote** — whichever side has more positions at `resolves_at` wins, auto-applied by the server worker. A **dispute window** (10 min) after resolution allows any position holder to contest before the outcome locks permanently. `host_decides` manual resolution is deferred to a future phase.

**Side bet rooms:** A room can declare itself a side-bet room by providing a **linked room's code** at creation time. The server subscribes the new room's WebSocket session to the linked room's event feed (game events, bet resolutions), giving context to bets made in the side room. This replaces any notion of hidden/private bets — instead of hiding a bet inside a room you just create a separate room for it and share that room's code with whoever you want.

**Code relationships:**
```
Room code:          AB3K7              (5 alphanumeric chars, no prefix)
Game code:          BINGO-AB3K7        (same chars, existing BINGO- prefix)
Bet code:           BET-AB3K7-X9Q2M   (room code + 5-char hash suffix)
Branch bet:         BET-AB3K7-R7KP1   (same room prefix, own hash; parent_bet_code → BET-AB3K7-X9Q2M)
Side-bet room:      XK2P9              (separate room, linked_room_code = AB3K7)
```

**Tasks:**

- [ ] **Database schema additions** (`db/store.go` + `db/sqlite.go`)
  - `rooms` table: `id`, `code` (5 chars, unique), `host_id`, `linked_room_code` (nullable FK → rooms.code — set if this is a side-bet room), `created_at`
  - `bets` table: `id`, `code` (e.g. `BET-AB3K7-X9Q2M`, unique), `room_code` (FK → rooms.code), `parent_bet_code` (nullable FK → bets.code, for branch bets), `creator_username`, `description` (max 280 chars), `locked_at` (nullable UTC — new positions rejected after this), `resolves_at` (UTC — resolution evaluated at this time), `status` (`open`|`locked`|`pending_resolution`|`disputed`|`won`|`lost`|`cancelled`), `created_at`, `resolved_at`, `dispute_deadline` (nullable UTC — set to `resolved_at + 10 min` on resolution)
  - `bet_positions` table: `id`, `bet_code` (FK → bets.code), `username`, `side` (`for`|`against`), `joined_at`
  - Indexes: `idx_bets_room_code`, `idx_bets_resolves_at`, `idx_bets_status`, `idx_bets_parent`, `idx_positions_bet_code`, `idx_positions_username`, `idx_rooms_linked_room_code`
  - Extend `GameStore` interface with: `CreateRoom`, `GetRoom`, `GetLinkedRooms` (returns all rooms with a given `linked_room_code`), `CreateBet`, `GetBetByCode`, `GetBetsByRoom`, `GetExpiredBets`, `GetLockedBets`, `ResolveBet`, `DisputeBet`, `CreateBetPosition`, `GetBetPositions`, `GetBetTree` (returns bet + all descendants)

- [ ] **Bet share code generation** (`server/room.go`)
  - `GenerateBetCode(roomCode string) string` — produces `BET-<roomCode>-<5char>` where the suffix is random alphanumeric, checked for uniqueness against DB before returning
  - Room code embedded in bet code allows server to route `/api/bet/:bet_code` without a join

- [ ] **Server — Room struct and state** (`server/room.go`)
  - `Room` struct: `Code string`, `HostID string`, `LinkedRoomCode string` (empty if not a side-bet room), `Game *Game` (nil until first game created), `mu sync.RWMutex`
  - `Server.Rooms map[string]*Room` + `RoomsMu sync.RWMutex`
  - `NewRoom(hostID string, linkedRoomCode string) *Room` with `GenerateRoomCode()` — 5-char alphanumeric, collision-checked
  - `getOrCreateRoom(code string) *Room` helper
  - On any game event or bet event in room `AB3K7`, server calls `forwardEventToLinkedRooms(roomCode, msg)` which looks up all rooms with `linked_room_code = AB3K7` and broadcasts a `linked_room_event` message to their members

- [ ] **Server — Background workers** (`server/room.go`)
  - **Lock worker** (every 30 s): finds bets with `status=open` and `locked_at <= now` → transitions to `locked`, broadcasts `bet_locked` to room members
  - **Resolution worker** (every 30 s): finds bets with `status=locked` (or `open` if no lock time) and `resolves_at <= now` → auto-resolves immediately by majority vote (whichever side has more positions wins; tie goes to `won` for the `for` side as tiebreaker); sets `dispute_deadline = now + 10 min`; broadcasts `bet_resolved`
  - **Dispute expiry worker** (every 30 s): finds bets with `status=disputed` and `dispute_deadline <= now` → re-applies original resolution and locks permanently

- [ ] **WebSocket protocol extensions** (`server/types.go`)
  - New client actions:
    - `room_login` — `{room_code, username, token}` — join room lobby session; if room has a `linked_room_code` the server immediately sends a `linked_room_snapshot` with recent events from the linked room
    - `place_bet` — `{description, resolves_at, locked_at?}` — creates bet, auto-joins creator on `for` side; resolution is always majority vote
    - `join_bet` — `{bet_code, side}` (`for`|`against`) — add position; rejected if bet is `locked`/`pending_resolution`/resolved
    - `branch_bet` — `{parent_bet_code, description, resolves_at, locked_at?}` — child bet; `resolves_at` must be ≤ parent's if parent has one
    - `resolve_bet` — `{bet_code, outcome}` (`won`|`lost`) — only available to bet creator or room host; only valid in `pending_resolution` status
    - `dispute_bet` — `{bet_code}` — available to any position holder within dispute window; transitions to `disputed`
  - New server message types:
    - `room_welcome` — lobby snapshot: game status, open bets with position counts, players online, `linked_room_code` if set
    - `linked_room_snapshot` — recent events from the linked room (sent on `room_login` if linked)
    - `linked_room_event` — forwarded event from linked room (game state change, bet resolved, etc.); clients display these in a read-only "Referenced Room" panel
    - `bet_placed` — `{bet}` — full bet object broadcast to room
    - `bet_position_updated` — `{bet_code, for_count, against_count, new_position}` — broadcast whenever a player joins a side (live odds proxy)
    - `bet_locked` — `{bet_code}` — no more positions accepted
    - `bet_pending_resolution` — `{bet_code, description, creator, for_count, against_count}`
    - `bet_resolved` — `{bet_code, outcome, resolved_by, dispute_deadline}`
    - `bet_disputed` — `{bet_code, disputed_by}`
    - `bet_resolution_locked` — `{bet_code, outcome}` — dispute window expired, outcome is final
    - `bet_branched` — `{parent_bet_code, child_bet}` — broadcast to room when a branch is created

- [ ] **HTTP REST endpoints** (`server/api.go`)
  - `POST /api/rooms` — create room; body: `{linked_room_code?}` (omit for a standalone room); returns `{code, game_code, linked_room_code}`
  - `GET /api/room/:code` — lobby snapshot: room info, game status, open/recent bets with position counts
  - `GET /api/room/:code/bets` — list bets; optional `?status=`, `?tree=true` (include child bets inline)
  - `POST /api/room/:code/bets` — place bet; body: `{description, resolves_at, locked_at?}`
  - `GET /api/bet/:bet_code` — single bet detail with full position list and branch tree (routable via room code embedded in bet code)
  - `POST /api/bet/:bet_code/join` — join a side; body: `{side}`; auth: JWT; rejected if `locked` or resolved
  - `POST /api/bet/:bet_code/branch` — create branch; body: `{description, resolves_at, locked_at?}`; auth: JWT
  - `PATCH /api/bet/:bet_code/resolve` — resolve; body: `{outcome}`; auth: JWT; restricted to creator or room host
  - `POST /api/bet/:bet_code/dispute` — dispute resolution; auth: JWT; restricted to position holders; only during dispute window

- [ ] **Authorization rules**
  - `resolve_bet` / `PATCH .../resolve` — only bet creator or room host; HTTP 403 otherwise
  - `dispute_bet` — only position holders on either side; only while `dispute_deadline > now`; HTTP 403 otherwise
  - `join_bet` — any room member; rejected with HTTP 409 if player already has a position on this bet
  - `place_bet` rate-limited: 3 bets per 60 s per player (extend `ratelimit.go`)
  - `join_bet` rate-limited: 10 joins per 60 s per player
  - Bet `description` sanitized server-side: strip control chars, max 280 chars, reject empty
  - `linked_room_code` validated on room creation: must be an existing room code; circular links rejected (HTTP 422)

- [ ] **Admin API extensions** (`server/admin.go`)
  - `GET /admin/api/rooms` — list all rooms with game + bet counts
  - `DELETE /admin/api/rooms/:code` — force-close room: cancels all open bets, broadcasts shutdown
  - `PATCH /admin/api/bets/:bet_code/force-resolve` — admin override resolution for disputed bets

- [ ] **Metrics** (`server/metrics.go`)
  - `bingo_rooms_active` Gauge
  - `bingo_bets_placed_total` Counter
  - `bingo_bets_branched_total` Counter
  - `bingo_bet_positions_total` CounterVec (labels: `side` = `for`|`against`)
  - `bingo_bets_resolved_total` CounterVec (labels: `outcome` = `won`|`lost`|`cancelled`)
  - `bingo_bets_disputed_total` Counter
  - `bingo_bets_pending_resolution` Gauge
  - `bingo_bets_disputed_active` Gauge

- [ ] **CLI — bingo game view** (`client/player.go`)
  - Add `sidebet` command available from within an active bingo game session
  - When typed, server creates a new room with `linked_room_code` set to the current game's room code, then prints:
    ```
    Side-bet room created! Code: XK2P9
    Share: https://bingoserver.live/room/XK2P9
    Others can join with: ./binGO-CLI -mode room -server ... -code XK2P9
    ```
  - Game play continues uninterrupted; the side-bet room runs independently
  - `help` text updated to mention `sidebet`

- [ ] **CLI — room mode** (`client/` + `bin.go`)
  - New `-mode room` flag: `./binGO-CLI -mode room -server localhost:8080 -code AB3K7`
  - **Room lobby view**: two-panel layout — left: bingo game status; right: live bet feed with `for`/`against` counts and countdown timers
  - **Bet detail view** (`view <bet_code>`): full-screen mode showing bet description, `FOR` and `AGAINST` player lists with timestamps, branch tree, share code, status + countdown; navigable: `back` returns to lobby
  - Lobby commands: `bet "<prediction>" <duration> [lock <duration>]` (e.g. `bet "Alice says synergy" 2h lock 90m`), `sidebet` (creates a new room linked to this one and prints its share code), `view <bet_code>`, `join` (switches to bingo game), `help`
  - If room has a linked room: a third panel or interleaved feed shows `linked_room_event` messages prefixed with `[↩ AB3K7]` so it's clear they're from the referenced room
  - Bet detail commands: `join for`, `join against`, `branch "<narrower claim>" <duration>`, `resolve won`, `resolve lost`, `dispute`, `share` (prints share URL), `back`
  - Countdown timers and position counts refresh every second without full redraw
  - Pending-resolution and disputed bets highlighted in a different colour

- [ ] **Web client — room lobby page** (`web-client/src/`)
  - Route `/room/:code` — room lobby
  - Left panel: bingo game status (current players, winner if ended) with "Join Game" button → `/game/BINGO-:code`
  - Right panel: live bet feed — cards showing description, `FOR N / AGAINST M` balance bar, countdown to lock/resolve, status badge
  - "Place a Bet" modal: description textarea, `resolves_at` datetime picker, optional `locked_at` datetime picker
  - If room has a `linked_room_code`: a collapsible "Referenced Room" panel at the top shows the linked room's live game status and recent bet resolutions (read-only, sourced from `linked_room_event` messages)
  - "Create Side-Bet Room" button on any room: creates a new room with `linked_room_code` set to the current room, then navigates to `/room/:new_code` — no extra modal needed
  - Share button on each bet card copies `https://bingoserver.live/bet/BET-AB3K7-X9Q2M`
  - Share button on room copies `https://bingoserver.live/room/:code`

- [ ] **Web client — bet detail page** (`web-client/src/`)
  - Route `/bet/:bet_code`
  - Header: bet description, share code, status badge, countdown (lock time if future, else resolve time)
  - Two-column position board: **FOR** (green) | **AGAINST** (red) — each column lists player names + joined timestamp, updates live via WebSocket
  - "Join FOR" / "Join AGAINST" buttons; disabled once player has a position or bet is locked
  - Branch bet tree below: each child bet is a collapsed card showing narrower claim + own position counts; click to navigate to `/bet/:child_code`
  - "Create Branch" button (opens modal pre-filled with parent description, `resolves_at` capped to parent's)
  - "Resolve" and "Dispute" buttons visible only when applicable (right role + right status)
  - Real-time updates via WebSocket `bet_position_updated`, `bet_resolved`, `bet_disputed`, `bet_branched`

- [ ] **Structured logging** (`server/logger.go`)
  - `RoomCreated(code, hostID string)`
  - `BetPlaced(betCode, roomCode, creatorUsername string, resolvesAt time.Time, method string)`
  - `BetPositionJoined(betCode, username, side string)`
  - `BetBranched(parentCode, childCode, creatorUsername string)`
  - `BetLocked(betCode string)`
  - `BetPendingResolution(betCode string)`
  - `BetResolved(betCode, outcome string, forCount, againstCount int)`
  - `LinkedRoomEventForwarded(sourceRoomCode, targetRoomCode, eventType string)`
  - `BetDisputed(betCode, disputedBy string)`
  - `BetResolutionLocked(betCode, outcome string)`

- [ ] **Tests**
  - Unit (`server/room_test.go`): `NewRoom`, `GenerateRoomCode`/`GenerateBetCode` uniqueness and format, bet lifecycle state machine (all transitions), lock worker, resolution worker (majority vote + tie-break), dispute expiry worker (mock clock), branch `resolves_at` validation, `forwardEventToLinkedRooms` fan-out, circular link rejection
  - Integration (`tests/`): room create → join → place bet → join both sides → lock fires → resolution worker fires → majority-vote auto-resolve; dispute → expiry re-locks; branch tree navigation; side-bet room creation from game view (linked_room_code set correctly); linked_room_event forwarded to side room; admin force-resolve
  - Race detector: `go test -race ./server/` covering concurrent `join_bet` on same bet from multiple goroutines
