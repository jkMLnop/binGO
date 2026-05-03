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



#### Phase 9: Client Features & Improved UX
**Goal:** Support hosting games on cloud server; add leaderboards; support custom buzzword lists

**Tasks:**
- [ ] Client menu system (Host vs Join)
  ```
  Connect to bingoserver.live?
  1) Host a new game
  2) Join existing game (with code)
  ```
  - Option 1: Host workflow
    - Prompt: "Enter path to JSON buzzword file (or 'skip' for defaults)"
    - If path provided: Validate JSON format, upload to server
    - If skip: Use default buzzwords.csv
    - Server creates game, assigns code, display to user
  - Option 2: Join workflow
    - Prompt for code, validate, join
  - Display game code in CLI (e.g., "Game code: ABC123")

- [ ] Buzzword suggestion system (in-game)
  - Players suggest via chat: `add_new_phrase <phrase>`
  - Suggestions ephemeral (in-memory only, no DB storage)
  - Host approves: `approve <phrase>` → adds to both current game AND host profile, saves to DB
  - Host rejects: `reject <phrase>` → discarded immediately (not stored)
  - When host creates new game: Inherits approved buzzwords from their profile
  - Host can also upload custom JSON on game creation (overrides their profile list)
  - Chat UI displays suggestion broadcasts and outcomes

- [ ] Leaderboard queries
  - Query wins_history table (created in Phase 8) to display top players
  - Display personal stats (wins, games played, win rate)
  - Sort by various metrics (wins, win rate, games played)

- [ ] Updated help text with new commands

#### Phase 9.5: Player Betting System
**Goal:** In-game social meta-game where players bet on outcomes using chat commands

**Tasks:**
- [ ] Betting commands: `bet: <player> wins|loses [AND <player> wins|loses]`
  - In-memory only (no DB persistence in MVP)
  - All bets public and broadcast to all players in game
  - AND-only compound grammar for MVP (OR/parens deferred)
  - One active bet per player per game round
- [ ] Active bets broadcast to all clients on each new bet placed
  - Displayed below game board, above user input prompt
  - Status icons: ⏳ active / ✓ won / ✗ lost
- [ ] Bet evaluation on every win event
  - `wins` condition: true if named player is the winner
  - `loses` condition: true if named player is NOT the winner
  - All conditions in a bet must be true for the bet to win
  - Results broadcast to all players immediately after win announced
- [ ] Bets cleared on game restart (fresh round = fresh bets)
- [ ] Help text updated with betting commands

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

#### Phase 11: Web Client & Shareable Links
**Goal:** Browser-based bingo client with URL-based game sharing (like Zoom meeting links)

**Tasks:**
- [ ] Web client (React + TypeScript)
  - Game board UI (3x3 grid with click-to-mark)
  - WebSocket integration (same protocol as CLI)
  - Player list + join form
  - Leaderboard display

- [ ] Shareable links feature
  - URL routing: `bingoserver.live/game/ABC123`
  - Server `GET /api/game/:code` endpoint (added in Phase 7.5) validates code
  - Web client pre-populates game code from URL
  - Share button copies link to clipboard
  - Works seamlessly from Phase 7.5 server endpoint

- [ ] CLI integration
  - When host creates game, display shareable link:
    ```
    Game created! Code: ABC123
    Share this link: https://bingoserver.live/game/ABC123
    Or use code with CLI: ./binGO-CLI -mode client -server bingoserver.live -code ABC123
    ```

- [ ] Mobile optimization
  - Responsive design (works on phone/tablet)
  - Touch-friendly board
  - PWA features (offline fallback)
