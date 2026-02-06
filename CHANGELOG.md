# Changelog

All notable changes to binGO-CLI are documented in this file.

## [Unreleased]

### Bug Fixes (2026-02-05)

#### Fixed
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
