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

#### Phase 8: Production Hardening & Scaling
**Goal:** Make cloud server reliable under load; automate deployments

**Observability architecture decision (Apr 2026):**
Local `docker-compose` Prometheus+Grafana stack is for local dev only — it does not scrape staging/prod. Scraping remote Fly.io servers from a dev laptop is fragile (metrics lost when laptop is offline, no alerting, not shareable).

Chosen approach per tier:
- **Local dev:** `docker-compose up` → Prometheus scrapes `bingo-server:8080`, Grafana at `localhost:3000`
- **Staging / Production (Phase 8):** Grafana Cloud free tier (hosted Prometheus + Grafana). Scrapes `https://bingo-server-staging.fly.dev/metrics` and `https://bingo-server.fly.dev/metrics` directly. Persistent dashboards, no infra to maintain, 10k series / 14-day retention free.
- **Phase 10 (K8s):** Self-hosted Prometheus on cluster with Thanos or federation for multi-replica aggregation and long-term retention. Mirrors the `GameStore` interface pattern — swap the observability implementation when scale demands it.

**Tasks:**
- [x] Security hardening
  - Rate limiting (prevent code brute-force) — per-IP token bucket, 5 failures/60s, `getCodeLimiter` in `server/ratelimit.go`
  - DDoS mitigation (connection limits per IP) — `wsConnLimitMiddleware` blocks 6th concurrent WS connection per IP (HTTP 429)
  - Logging/monitoring for abuse patterns — `Logger.RateLimitExceeded` (structured WARN JSON), `bingo_rate_limited_total` Prometheus CounterVec (labels: `ws`, `code_guess`)

- [ ] Make load test target-configurable for remote environments
  - `full_system_load_test.go` is hardcoded to `127.0.0.1:8080` and `dev-admin-key-local-only`
  - Read `LOAD_TEST_URL` env var (default `http://127.0.0.1:8080`) for base URL
  - Read `ADMIN_API_KEY` env var (default `dev-admin-key-local-only`) for admin auth
  - Auto-derive WebSocket scheme from base URL: `https://` → `wss://`, `http://` → `ws://`
  - Usage: `LOAD_TEST_URL=https://bingo-server-staging.fly.dev ADMIN_API_KEY=test-regression-key-12a go test -tags=e2e ./tests -v`
  - No other app changes needed — admin API, WebSocket protocol, and `/metrics` endpoint are identical on all environments
  - Fill in `load-test-with-monitoring.sh` wrapper script with env var support and Grafana dashboard URL output
  - Add DDoS / rate-limit simulation phase to `full_system_load_test.go`:
    - Phase 5a — **Connection flood**: open `maxConnsPerIP+N` concurrent WS connections from the same process (same source IP); assert the `(maxConnsPerIP+1)`th attempt receives HTTP 429 and `bingo_rate_limited_total{endpoint="ws"}` increments
    - Phase 5b — **Brute-force code-guess flood**: send `codeGuessPerWindow+1` rapid sequential login attempts with a non-existent game code; assert the last attempt receives the rate-limit message and `bingo_rate_limited_total{endpoint="code_guess"}` increments
    - Phase 5c — **Server health under attack**: verify `/api/status` still responds 200 and legitimate players can still connect/play while the flood is ongoing (resilience, not just guardrail correctness)
    - Note: the two narrow wiring checks (connection limit + brute-force) already live in `container_regression_test.go` (tests 14.1 and 14.2); the e2e/load tier adds the sustained-load resilience angle that container tests can’t cover

- [ ] Grafana Cloud monitoring for staging & production
  - **Why:** Local `docker-compose` Prometheus+Grafana only runs when laptop is on — no persistent metrics, no alerting, not shareable. Grafana Cloud free tier solves all three with zero infra.
  - **Free tier limits:** 10,000 active series, 14-day retention, 3 users — more than sufficient for single-instance Fly.io deployments.
  - Setup steps:
    1. Create free account at https://grafana.com/products/cloud/
    2. In Grafana Cloud → Connections → Add new connection → Hosted Prometheus, note the remote-write URL and API key
    3. Configure Grafana Alloy (lightweight agent) or use Prometheus `remote_write` to push `bingo_*` metrics from each Fly.io app
       - Option A (simpler): Add a Grafana Alloy sidecar process to the Fly.io app that scrapes `localhost:8080/metrics` and remote-writes to Grafana Cloud
       - Option B: Run a tiny `prometheus` instance per Fly.io app with `remote_write` config pointing to Grafana Cloud
    4. Create dashboards in Grafana Cloud mirroring the local `bingo-dashboard.json` panels
    5. Add `instance` or `env` label (`staging` / `production`) to differentiate tiers in shared dashboards
    6. Set up alerting rules: `bingo_errors_total` rate spike, game count drop to 0, scrape target down
  - Update `load-test-with-monitoring.sh` to print Grafana Cloud dashboard URL instead of `localhost:3000`
  - **Do NOT add remote scrape targets to local `prometheus.yml`** — local stack stays local-only

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

#### Phase 10: Kubernetes & Scaling (Future)
**Goal:** Run multiple server instances with shared database

**Tasks:**
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
