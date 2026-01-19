# Changelog

All notable changes to binGO-CLI are documented in this file.

## [Unreleased]

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
