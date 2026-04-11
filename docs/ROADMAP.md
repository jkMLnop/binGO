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

**Tasks:**
- [ ] Security hardening
  - Rate limiting (prevent code brute-force)
  - DDoS mitigation (connection limits per IP)
  - Logging/monitoring for abuse patterns

- [ ] Context propagation & error wrapping audit
  - Review all Go functions for missing `context.Context` parameters (DB calls, HTTP handlers, goroutines)
  - Ensure all errors are wrapped with `fmt.Errorf("...: %w", err)` so stack context is preserved
  - Replace bare `errors.New` / `fmt.Errorf` (without `%w`) at call boundaries that discard the original error
  - Verify `ctx` is passed through to `sqlite.go` store methods and cancelled correctly on shutdown
  - Add `context.WithTimeout` where long-running DB or network operations lack a deadline

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
  - **Context**: Phase 8 metrics + logs sufficient for single monolith. Multi-service architectures need request tracing.
  - OpenTelemetry instrumentation (open standard for tracing)
  - Jaeger integration for request tracing across pods
  - Trace game creation from client request → auth service → game service → DB write → response
  - Identify cross-pod bottlenecks and service latency breakdown
  - Debug session correlation (which pod handled which request)
  - Correlate traces with Phase 8 structured logs using trace IDs

- [ ] Testing under K8s
  - Multi-replica game coordination
  - Database failover scenarios
  - Performance benchmarking under load with tracing insights

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
