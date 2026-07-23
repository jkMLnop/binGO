# Project Roadmap

The evolution of binGO organized by development phases.

> **Note:** The CLI client has been split into its own repository —
> [binGO-CLI](https://github.com/jkMLnop/binGO-CLI). This roadmap covers the
> server component only.

## TODO

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

#### Phase 13: Rooms, Live Bets & Bet Exchange
**Goal:** Persistent rooms hosting bingo games and a live prediction-bet exchange. Implemented in 6 incremental sub-phases.

**Design decisions:**
- Room infrastructure (`rooms` table, `Room` struct, `room_login`/`room_welcome`, `POST /api/rooms`, `GET /api/room/:code`, metrics, logging) was established in **Phase 11.0**. Phase 13 adds bets and side-bet rooms on top of existing rooms.
- Room code (`AB3K7`, 5-char) → game code `BINGO-AB3K7`. Standalone games with no associated room retain their `BINGO-XXXXX` codes unchanged.
- Bingo game inside a room is created lazily on first `room_login` — behaviour established in Phase 11.0.
- Existing Phase 9.5 in-game bet types (`Bet`, `BetCondition`) are renamed to `GameBet`, `GameBetCondition` before Phase 13.2 to avoid name collision.

**Code relationships:**
```
Room code:      AB3K7              (5 alphanumeric chars, no prefix)
Game code:      BINGO-AB3K7        (same chars, existing BINGO- prefix)
Bet code:       BET-AB3K7-X9Q2M   (room code + 5-char random suffix)
Branch bet:     BET-AB3K7-R7KP1   (same room prefix; parent_bet_code → BET-AB3K7-X9Q2M)
Side-bet room:  XK2P9              (separate room, linked_room_code = AB3K7)
```

---

##### Phase 12.5: Multi-Board Room System ✅
**Goal:** One room, many boards. Room pages show all boards; hosts create new boards from a room page. Completed June 2025.

**Design decisions:**
- Each board is a `Game` with an optional `title` field. Boards link to their parent room via `room_code` column.
- Room page (`/room/:code`) shows all boards in the room with join/delete actions.
- Board creation happens from the room page using the existing GenerateModal in "create" mode.
- Identity layer: `bingo-identity` localStorage key persists username across sessions. Pre-fills join form.

**Tasks:**
- [x] **DB** (`db/store.go`, `db/sqlite.go`): Add `title` column to `games` table. Add `GetGamesByRoom(roomCode)` and `SetGameRoomCode(gameID, roomCode)` to `GameStore`.
- [x] **Server** (`server/api.go`): Add `GET/POST/DELETE /api/room/:code/games` endpoints. Set `HostUsername` on room info responses. Auth via Bearer token (room admin).
- [x] **Frontend** (`App.tsx`): Add `/room/:code` route with `RoomPage` component. Add `GamesBetsPanel` component showing board cards. Update `GenerateModal` with `mode` prop ("edit"|"create") and board title field for create mode. Update `HomePage` with "Create Room" button and room join form.
- [x] **API** (`api.ts`): Add `RoomGameInfo` type, `fetchRoomGames`, `createRoomGame`, `deleteRoomGame` functions.
- [x] **Identity layer**: localStorage `bingo-identity` for persistent username. Pre-fills join form. "Change" link clears identity. Token cleared on Leave but localStorage preserved.
- [x] **Tests**: All existing unit + integration tests pass. Room tests pass.
- [x] **Playwright tests** (Phase G): `room-flow-smoke.spec.js`, `dev-smoke.spec.js`, update `staging-smoke.spec.js`.

---

##### Phase 13.1: Room Linking (prerequisite for Phase 13.5)
**Goal:** Add `linked_room_code` to the `rooms` table so Phase 13.5 can create side-bet rooms that mirror events from a source room. No user-facing changes.

> Room foundation (`rooms` table, `Room` struct, `room_login`/`room_welcome`, `POST /api/rooms`, `GET /api/room/:code`, metrics, logging) was moved to **Phase 11.0** to give Phases 11.3, 11.4, and 12 a consistent `rooms.code` FK from day one. Phase 13.2 can start directly once Phase 11.0 is complete.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): `ALTER TABLE rooms ADD COLUMN linked_room_code TEXT NULLABLE REFERENCES rooms(code)`. Add `GameStore` method `GetLinkedRooms(roomCode string) ([]Room, error)`. Update `room_welcome` server message to include `linked_room_code` when present.
- [ ] **Tests**: `GetLinkedRooms` returns correct rooms for a given code. Circular link validation enforced at API level in Phase 13.5.

---

##### Phase 13.2: Simple Bets
**Goal:** DB-persisted bets in rooms — place, join sides, manual resolve by creator/host. No workers, no branching yet.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `bets` table (`id`, `code` unique e.g. `BET-AB3K7-X9Q2M`, `room_code` FK, `parent_bet_code` nullable, `creator_username`, `description` max 280 chars, `locked_at` nullable, `resolves_at`, `status` enum `open|locked|pending_resolution|disputed|won|lost|cancelled`, `created_at`, `resolved_at`, `dispute_deadline` nullable). Add `bet_positions` table (`id`, `bet_code` FK, `username`, `side` `for|against`, `joined_at`). Indexes: `idx_bets_room_code`, `idx_bets_resolves_at`, `idx_bets_status`, `idx_bets_parent`, `idx_positions_bet_code`, `idx_positions_username`. Add `GameStore` methods: `CreateBet`, `GetBetByCode`, `GetBetsByRoom`, `CreateBetPosition`, `GetBetPositions`, `ResolveBet`.
- [ ] **Bet code gen** (`server/room.go`): `GenerateBetCode(roomCode string) string` → `BET-<roomCode>-<5char>`, uniqueness-checked against DB.
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): Client actions: `place_bet` (`{description, resolves_at, locked_at?}`), `join_bet` (`{bet_code, side}`), `resolve_bet` (`{bet_code, outcome}` — creator/host only). Server messages: `bet_placed`, `bet_position_updated` (`{bet_code, for_count, against_count, new_position}`), `bet_resolved` (`{bet_code, outcome, resolved_by, dispute_deadline}`).
- [ ] **HTTP** (`server/api.go`): `POST /api/room/:code/bets`, `GET /api/room/:code/bets` (`?status=`), `GET /api/bet/:bet_code`, `POST /api/bet/:bet_code/join` (`{side}`), `PATCH /api/bet/:bet_code/resolve` (`{outcome}`).
- [ ] **Auth & validation**: `resolve_bet` restricted to bet creator or room host (HTTP 403). `join_bet` rejects duplicate position (HTTP 409). `description` sanitized: strip control chars, max 280 chars, reject empty.
- [ ] **Rate limiting** (`server/ratelimit.go`): 3 `place_bet` / 60 s per player. 10 `join_bet` / 60 s per player.
- [ ] **Metrics** (`server/metrics.go`): `bingo_bets_placed_total` Counter. `bingo_bet_positions_total` CounterVec (label: `side`). `bingo_bets_resolved_total` CounterVec (label: `outcome`).
- [ ] **Logging** (`server/logger.go`): `BetPlaced`, `BetPositionJoined`, `BetResolved`.
- [ ] **Web client** (`web-client/src/`): Add `/room/:code` route. Left panel: game status + "Join Game" button → `/game/BINGO-:code`. Right panel: bet feed with for/against counts, status badge, countdown. "Place a Bet" modal (description, resolves_at, optional locked_at). Join FOR/AGAINST buttons. Share button copies `https://yubetcha.com/bet/:bet_code`. Share button on room copies `https://yubetcha.com/room/:code`.
- [ ] **Tests**: full bet lifecycle; 403 on non-creator resolve; 409 on duplicate position; rate limit enforcement; DB integration tests.

---

##### Phase 13.3: Auto-Resolution Workers + Dispute
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

##### Phase 13.4: Bet Branching
**Goal:** `parent_bet_code` FK, branch creation, `resolves_at` ≤ parent validation, bet tree display.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `GameStore` method: `GetBetTree(betCode string)` — returns bet + all descendants recursively.
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): Client action: `branch_bet` (`{parent_bet_code, description, resolves_at, locked_at?}`). Validate `resolves_at` ≤ parent's `resolves_at`. Server message: `bet_branched` (`{parent_bet_code, child_bet}`).
- [ ] **HTTP** (`server/api.go`): `POST /api/bet/:bet_code/branch`. Update `GET /api/bet/:bet_code` to include full branch tree.
- [ ] **Metrics** (`server/metrics.go`): `bingo_bets_branched_total` Counter.
- [ ] **Logging** (`server/logger.go`): `BetBranched(parentCode, childCode, creatorUsername string)`.
- [ ] **Web client** (`web-client/src/`): Add `/bet/:bet_code` route. Header: description, share code, status badge, countdown. Two-column position board (FOR green | AGAINST red), player names + joined timestamps. Branch tree below (child cards, click to navigate to `/bet/:child_code`). "Create Branch" modal (pre-filled, resolves_at capped to parent's). "Join FOR" / "Join AGAINST" (disabled when locked or already positioned). "Resolve" and "Dispute" buttons when applicable. Real-time updates via `bet_position_updated`, `bet_resolved`, `bet_disputed`, `bet_branched`.
- [ ] **Tests**: branch creation, `resolves_at` validation edge cases, `GetBetTree` recursion, web client navigation.

---

##### Phase 13.5: Side-Bet Rooms
**Goal:** `linked_room_code`, event forwarding from linked room to side rooms, `sidebet` CLI command.

- [ ] **Validation** (`server/api.go`): On `POST /api/rooms`, if `linked_room_code` provided: verify room exists (HTTP 404); reject circular links (HTTP 422).
- [ ] **Event forwarding** (`server/room.go`): `forwardEventToLinkedRooms(roomCode string, msg ServerMessage)` — queries `GetLinkedRooms(roomCode)`, broadcasts `linked_room_event` to each side room's connected players. Called on game events and bet events in the source room.
- [ ] **WebSocket** (`server/types.go`, `server/server.go`): On `room_login`, if room has `linked_room_code`, send `linked_room_snapshot` (last 20 events from linked room). Server message: `linked_room_event` (`{source_room_code, event_type, payload}`).
- [ ] **CLI** (`client/player.go`): Add `sidebet` command in active game session — creates a new room with `linked_room_code` = current game's room code, prints share URL, game continues uninterrupted. Update `help` text.
- [ ] **Web client** (`web-client/src/`): "Create Side-Bet Room" button → `POST /api/rooms` with `linked_room_code`, navigate to `/room/:new_code`. If room has `linked_room_code`: collapsible "Referenced Room" panel (read-only, sourced from `linked_room_event` messages).
- [ ] **Logging** (`server/logger.go`): `LinkedRoomEventForwarded(sourceRoomCode, targetRoomCode, eventType string)`.
- [ ] **Tests**: event fan-out correctness, circular link rejection (HTTP 422), linked room snapshot delivery on login.

---

##### Phase 13.6: CLI Room Mode + Admin API
**Goal:** `-mode room` flag, full room lobby CLI, bet detail view, admin room endpoints.

- [ ] **bin.go**: Add `-mode room` flag. Dispatch to `runRoom(serverAddr, roomCode string)`.
- [ ] **`client/room.go`** (new file): Room lobby view — two-panel layout (game status left, live bet feed right with for/against counts + countdown timers refreshing every 1 s without full redraw). Lobby commands: `bet "<prediction>" <duration> [lock <duration>]` (e.g. `bet "Alice says synergy" 2h lock 90m`), `view <bet_code>`, `join` (switches to bingo game), `sidebet`, `help`. If room has linked room: interleaved `linked_room_event` messages prefixed with `[↩ <room_code>]`. Bet detail view (`view <bet_code>`): full-screen FOR/AGAINST player lists with timestamps, branch tree. Commands: `join for`, `join against`, `branch "<claim>" <duration>`, `resolve won`, `resolve lost`, `dispute`, `share`, `back`. Pending-resolution and disputed bets highlighted differently.
- [ ] **Admin API** (`server/admin.go`): `GET /admin/api/rooms` (list all rooms with game + bet counts). `DELETE /admin/api/rooms/:code` (force-close: cancel open bets, broadcast shutdown). `PATCH /admin/api/bets/:bet_code/force-resolve` (admin override for disputed bets).
- [ ] **Tests**: CLI room mode integration tests. Admin room endpoints (auth, list, delete, force-resolve).

---

#### Phase 14: Public Bet Search Engine + Agentic Auto-Settlement
**Goal:** Transform yubetcha.com into a Polymarket-style public bet discovery platform where anyone can find, join, and watch bets on real-world media events settle automatically — powered by a local Python agent stack (YouTube/Twitch/Zoom/local audio) running on your machine, posting results back to the Fly.io server.

**Design boundary — two completely separate bet worlds:**

| | Private Room Bets (Phase 13) | Public Bets (Phase 14) |
|---|---|---|
| Access | Room code invite only | Anyone via search |
| Discoverability | Never indexed, never searchable | Landing page search engine |
| Settlement | Manual (creator or host) | Agentic (YouTube/Twitch/Zoom bot) |
| Source | What happens in the room/game | External media (video, stream, tweet) |
| DB table | `bets` (room-scoped, `room_code` FK) | `public_bets` (no room FK, `source_url`) |
| API namespace | `/api/room/:code/bets` | `/api/bets/search`, `/api/public-bets/` |
| UI surface | Room lobby right panel | Landing page search bar |

These share zero DB tables and zero API routes. A user browsing the landing page never sees private room bets. A user inside a private room never sees public bets.

**Infrastructure layout:**
```
Mac mini (local agent stack)            Fly.io (cloud server)
────────────────────────────            ─────────────────────
Settlement agent (cron/poll) ────────→  POST /api/public-bets/:code/resolve
Recommendation agent (batch) ────────→  POST /api/public-bets/ (create suggestion)
Ollama deepseek-r1:8b (LLM)
faster-whisper base (STT)
Local listener (Zoom/any audio)
```

Agent auth: dedicated `AGENT_API_KEY` env var on server (separate from `ADMIN_API_KEY`). Agents send it as `X-Agent-Key` header.

Webhook strategy: use polling (5-min interval) to avoid inbound connection requirements. Optional upgrade path: ngrok paid plan (~$10/mo) for static domain + push webhooks from YouTube PubSubHubbub and Twitch EventSub.

---

##### Phase 14.0: Public Bet Foundation
**Goal:** `public_bets` DB table, search API, agent auth, and landing page search UI. Prerequisite for all other Phase 13 sub-phases.

- [ ] **DB** (`db/store.go`, `db/sqlite.go`): Add `public_bets` table (`id`, `code` unique e.g. `PUB-X9Q2M`, `creator_username` nullable, `description` max 280 chars, `source_url` nullable, `source_type` enum `youtube|twitch|twitter|zoom|local|manual`, `tags` JSON array, `status` enum `open|locked|pending_resolution|disputed|won|lost|cancelled|expired`, `resolves_at`, `locked_at` nullable, `created_at`, `resolved_at` nullable, `settlement_evidence` text nullable, `dispute_deadline` nullable). Add `public_bet_positions` table (mirrors `bet_positions` but FK → `public_bets.code`). Indexes: `idx_public_bets_status`, `idx_public_bets_resolves_at`, `idx_public_bets_source_type`, `idx_public_bet_positions_bet_code`. Add `GameStore` methods: `CreatePublicBet`, `GetPublicBetByCode`, `SearchPublicBets(query string, sourceType string, status string, limit int)`, `CreatePublicBetPosition`, `GetPublicBetPositions`, `ResolvePublicBet`.
- [ ] **Server auth** (`server/auth.go`): Add `AGENT_API_KEY` env var (fallback: `dev-agent-key-local-only`). Add `agentAuthMiddleware` checking `X-Agent-Key` header — used on resolve/create endpoints.
- [ ] **HTTP** (`server/api.go`): `GET /api/bets/search?q=&source=&status=` (partial match search, returns paginated bet cards). `POST /api/public-bets/` (agent or admin creates a public bet). `GET /api/public-bets/:code` (full detail + positions). `POST /api/public-bets/:code/join` (`{side}` — any authenticated user). `PATCH /api/public-bets/:code/resolve` (agent auth only — `{outcome, evidence}`).
- [ ] **Metrics** (`server/metrics.go`): `bingo_public_bets_active` Gauge. `bingo_public_bets_created_total` Counter. `bingo_public_bets_resolved_total` CounterVec (label: `source_type`).
- [ ] **Web client** (`web-client/src/`): Replace placeholder landing page (`/`) with search engine UI. Empty state: large centered search bar + "Trending bets" section (top 10 open bets by position count). As user types: debounced `GET /api/bets/search?q=<partial>` (150ms debounce). Results: bet cards showing description, source type icon, for/against counts, status badge, expiry countdown. Click card → `/bet/:code` public bet detail page (FOR/AGAINST join buttons, source link, settlement evidence when resolved). Share button copies `https://yubetcha.com/bet/:code`.
- [ ] **Tests**: `SearchPublicBets` full-text matching, agent auth (403 without key, 200 with), join deduplication (409), public bet lifecycle integration test.

---

##### Phase 14.1: YouTube Settlement Agent
**Goal:** Python agent that monitors YouTube channels, fetches transcripts, evaluates bet conditions via LLM, and settles bets automatically.

> **Agent stack lives in `agent/` directory** (new Python project, separate from Go server). Uses `pyproject.toml` / `uv` for dependency management.

- [ ] **Agent scaffold** (`agent/`): Create Python project with `uv`. Core interface: `SourceAdapter` abstract class with `poll(bet: PublicBet) -> TranscriptChunk | None`. `ConditionEvaluator` class wraps Ollama (`deepseek-r1:8b`) with structured JSON output. `SettlementClient` posts results to Go server via `PATCH /api/public-bets/:code/resolve`. Scheduler: APScheduler cron job, 5-min interval per tracked source. Config via `.env` file: `BINGO_SERVER_URL`, `AGENT_API_KEY`, `OLLAMA_BASE_URL` (default `http://localhost:11434`).
- [ ] **YouTube adapter** (`agent/adapters/youtube.py`): `youtube-transcript-api` library fetches captions for video ID. YouTube Data API v3 (`google-api-python-client`) polls channel for new uploads since last check. Fallback: `yt-dlp` audio download → `faster-whisper base` transcription when no captions available. Stores `last_checked_at` per channel in `agent/state.db` (SQLite, separate from server DB).
- [ ] **LLM condition evaluator** (`agent/evaluator.py`): Prompt template: given transcript and bet description, return `{met: bool, confidence: float, evidence: str}`. Structured output enforced via Ollama JSON mode. Confidence threshold: only settle if `confidence >= 0.85`. Below threshold: log as `uncertain`, retry on next poll cycle with more transcript context.
- [ ] **Settlement loop** (`agent/scheduler.py`): For each `open` public bet with `source_type=youtube`: fetch new transcript chunks → evaluate → if met: `PATCH /api/public-bets/:code/resolve {outcome: "won", evidence: "..."}`. If `resolves_at` passed with no match: resolve as `lost`. Idempotent: skip bets already in terminal state.
- [ ] **Tests** (`agent/tests/`): Mock YouTube API responses. Evaluator unit tests with known transcript fixtures. Settlement client integration test against local Go server. Confidence threshold boundary tests.

---

##### Phase 14.2: Twitch Settlement Agent
**Goal:** Monitor Twitch streams via local audio capture — same BlackHole → faster-whisper pipeline as 13.3, reusing the same `SourceAdapter` interface. No Twitch API keys, no chat proxy heuristics.

> **Prerequisite:** Phase 14.3 (local listener) must be complete — this phase is a thin config layer on top of it.

- [ ] **Twitch adapter** (`agent/adapters/twitch.py`): Open the Twitch stream URL in the system browser or via `streamlink` → audio routes through BlackHole → existing `LocalAudioAdapter` from 13.3 handles transcription. `streamlink` preferred (headless, no browser needed): `streamlink twitch.tv/<channel> best --player-fifo` piped to a virtual audio sink.
- [ ] **Stream discovery**: Poll Twitch Helix API (`GET /streams?user_login=`) every 60 s to detect when a tracked channel goes live. No EventSub, no webhooks needed.
- [ ] **Config**: `TWITCH_CHANNELS` env var (comma-separated list of channel names to track). Agent starts capture automatically when a tracked channel goes live; stops on stream end.
- [ ] **LLM evaluator**: same `ConditionEvaluator` as 14.1/14.3 — transcript replaces chat. No prompt changes needed.
- [ ] **Tests**: mock `streamlink` subprocess with WAV fixture piped as audio. Stream discovery polling mock. Full settlement lifecycle with local Go server.

---

##### Phase 14.3: Local Listener Agent (Universal Audio Monitor)
**Goal:** Capture any audio playing on the Mac (Zoom calls, browser streams, local video) via system audio routing, transcribe with faster-whisper, and feed to the same condition evaluator. Enables bingo auto-marking and bet settlement for any meeting or stream without platform-specific integration.

**Setup (one-time, macOS):**
- Install BlackHole 2ch virtual audio driver (free, open-source)
- Create macOS Multi-Output Device: speakers + BlackHole as co-outputs (audio still plays normally)
- Set BlackHole as default input device for the agent

- [ ] **Local listener** (`agent/adapters/local_audio.py`): `sounddevice` library captures audio from BlackHole input device in 3-second chunks. `faster-whisper` (`base` model, CPU) transcribes each chunk. Transcript chunks appended to sliding window (last 60s). Emits `TranscriptChunk` events consumed by scheduler.
- [ ] **Bingo auto-mark integration**: For each transcript chunk, check against active game's buzzword list (exact + fuzzy match via `rapidfuzz`). On match: call `POST /api/games/:code/mark` on Go server (new endpoint) to mark the cell for all players. No LLM needed — pure string matching is fast and deterministic.
- **Bet condition evaluation**: Same `ConditionEvaluator` as 14.1/14.2 — local transcript replaces YouTube/Twitch source.
- [ ] **CLI mode** (`bin.go`): Add `-mode listen -server <addr> -code <game_code>` flag. Starts the local listener agent and connects to a running game session. Prints "Listening... (BlackHole detected)" or setup instructions if BlackHole not found.
  > **Note:** `-mode listen` is a Python subprocess launched by the Go binary, or alternatively a standalone `agent/listen.py` CLI script. TBD based on packaging preference.
- [ ] **Performance**: On 2018 Intel i7 Mac mini, `faster-whisper base` transcribes a 3s chunk in ~1-2s. Acceptable lag for bingo (cell marks appear 2-4s after word is spoken). Document minimum hardware requirements.
- [ ] **Tests**: mock audio input fixture (WAV file played through adapter). Buzzword matching unit tests (exact, fuzzy threshold). End-to-end: WAV with known content → bingo cell marked on local server.

---

##### Phase 14.4: Zoom SDK Bot Integration
**Goal:** Dedicated Zoom bot that joins meetings as a silent participant and receives the official Zoom live transcript stream — higher quality than local audio capture, works even when not personally in the meeting.

> **Prerequisite:** Zoom Marketplace app registration (free for development). Requires `ZOOM_ACCOUNT_ID`, `ZOOM_CLIENT_ID`, `ZOOM_CLIENT_SECRET` env vars.

- [ ] **Zoom bot** (`agent/adapters/zoom_sdk.py`): Zoom Meeting SDK (server-to-server OAuth). Bot joins meeting by meeting ID, receives `meeting.transcription_message` webhook events (requires live transcription enabled on host's Zoom account). Falls back to Phase 14.3 local audio listener if SDK join fails or host hasn't enabled transcription.
- [ ] **Webhook receiver** (`agent/webhook_server.py`): Small FastAPI server (port 8090) receiving Zoom webhook push events. `ngrok http 8090` tunnels it to a public URL registered in Zoom Marketplace dashboard. Alternatively: Fly.io webhook relay endpoint (`POST /internal/zoom-webhook`) forwarded to local agent via long-poll queue.
- [ ] **Transcript dispatch**: Zoom transcript events → same `TranscriptChunk` interface → `ConditionEvaluator` + bingo auto-mark. Unified pipeline regardless of source.
- [ ] **Bet suggestion during calls** (`agent/suggester.py`): Every 5 minutes during an active meeting, run LLM pass on the last 5-minute transcript window: "Based on this conversation, suggest 3 funny, specific bets that participants could place about what will happen before the meeting ends. Format: {description, resolves_at_minutes}." Push suggestions to Go server via `POST /api/public-bets/` with `source_type=zoom` and `status=suggested` (new status — visible to room participants but not yet open for joining).
- [ ] **Tests**: mock Zoom webhook payload fixtures. Transcript dispatch integration. Suggestion generation with known transcript fixture (verify non-generic output with few-shot prompt).

---

##### Phase 14.5: Recommendation Engine
**Goal:** Ingest a creator's content history, extract behavioral patterns, and generate funny/specific bet suggestions that surface on the landing page as recommended bets.

> **Start with 2-3 cherry-picked creators** to validate before generalising. Good candidates: high-volume streamers or podcasters with distinctive verbal patterns.

- [ ] **Content ingestion** (`agent/ingestion/`): Per-creator pipeline: fetch last 30 YouTube videos or Twitch VODs → get transcripts (via youtube-transcript-api or Twitch VOD captions) → chunk and store in `agent/creator_corpus.db`.
- [ ] **Pattern extraction** (`agent/patterns.py`): LLM pass over corpus: "Identify recurring phrases, behaviors, topics, and predictable patterns for this creator. Return structured JSON: `{patterns: [{description, frequency, example_quote}]}`." Store extracted patterns in `creator_corpus.db`.
- [ ] **Bet generation** (`agent/suggester.py`): LLM pass with extracted patterns + few-shot examples of good bets: "Using these patterns, generate 5 bets that fans of this creator would find funny and specific. Each bet must be: time-bound, falsifiable, and reference a real observed pattern." Few-shot examples curated manually at first. Filter pass: second LLM call scores each suggestion on `{specificity: 0-1, humor: 0-1, falsifiability: 0-1}` — only publish if all three ≥ 0.7.
- [ ] **Publish to server**: approved suggestions → `POST /api/public-bets/` with `source_type=youtube|twitch`, `status=open`, `creator_username` tag. Appear on landing page trending section.
- [ ] **Scheduling**: full ingestion + generation run weekly per creator (heavy). Pattern extraction cached — only re-run if new content volume > 5 items since last run.
- [ ] **Tests**: pattern extraction with known creator transcript fixture. Bet generation smoke test (verify structured output schema). Score filter boundary tests. End-to-end: corpus → published bet on local server.

---

#### Phase 16: Grafana Reliability + Playwright Dashboard Smoke Tests
**Goal:** Make Grafana consistently usable in local/staging/prod and add browser-level Playwright smoke tests so dashboard regressions are detected automatically.

**Motivation:** We currently smoke-test gameplay and API flows, but monitoring UI can still silently break (datasource disconnects, missing panels, broken provisioning, login drift). This phase adds the same agentic guardrail for observability UX.

**Scope decisions:**
- Dashboard smoke tests are read-only (no panel edits, no alert mutation).
- Run against local compose and staging first; production dashboard smoke can be scheduled once auth/secrets are stable.
- Reuse the existing Playwright setup in `web-client/` with a dedicated spec folder for ops smoke.

##### Phase 16.0: Grafana Baseline Hardening
**Goal:** Eliminate known provisioning/auth drift before automating browser checks.

- [ ] Ensure datasource provisioning is deterministic on fresh and reused volumes (`grafana-provisioning/datasources/datasources.yml`).
- [ ] Ensure dashboard provisioning always loads the expected board (`grafana-provisioning/dashboards/dashboards.yml`, `grafana-dashboards/bingo-dashboard.json`).
- [ ] Add a lightweight health endpoint check in docs/scripts: Grafana up, Prometheus datasource connected, dashboard present.
- [ ] Document required env vars and test credentials for CI in `docs/MONITORING_SETUP.md`.

##### Phase 16.1: Playwright Dashboard Smoke (Local + Staging)
**Goal:** Add browser smoke tests that prove Grafana is usable end-to-end.

- [ ] Add Playwright spec file(s), e.g. `web-client/e2e/grafana-smoke.spec.js`.
- [ ] Test flow: open Grafana login, authenticate, load dashboard, assert key panels/titles render, assert no datasource error banners.
- [ ] Add script(s) in `web-client/package.json`:
  - `smoke:grafana:local`
  - `smoke:grafana:staging`
- [ ] Add artifact retention for trace/screenshot on failure.

##### Phase 16.2: CI Wiring + Incident Hooks
**Goal:** Run Grafana smoke where web smoke already runs and auto-open issues on failures.

- [ ] Extend `.github/workflows/dev-smoke.yml` to run local Grafana smoke after stack readiness.
- [ ] Extend staging smoke path in `.github/workflows/ci.yml` with Grafana smoke and failure artifacts.
- [ ] Optional: add scheduled prod synthetic workflow for read-only Grafana checks.
- [ ] On failure, auto-create issue with run link + screenshot/trace pointers (same pattern as current staging/prod smoke failures).

##### Phase 16.3: Regression Mapping
**Goal:** Track observability UI checks in the same regression framework as game flows.

- [ ] Add a "Grafana dashboard smoke" section to `tests/REGRESSION_TESTS.md` and mark automated ownership.
- [ ] Keep only manual UX judgment items in the checklist; migrate deterministic checks to Playwright.

---

## Deferred / Maybe Never

#### Phase 9.6: In-Game Chat
**Goal:** Let players send free-form text messages to everyone in the game during play.

Deferred — the existing in-game bet system (`bet: <player> wins`) already provides structured social interaction. Free-form chat may add noise without much value for a bingo game. Revisit if users ask for it.

- `say <message>` command → `chat` WebSocket action → `chat_message` broadcast
- Rate-limit: 5 messages / 10 s per player
- Display: `💬 <username>: <message>` inline below board, scrolls away on next redraw
