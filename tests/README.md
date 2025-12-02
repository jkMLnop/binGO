```bash
~~~~~~~~~~~~~~~~~~~~~ /$$       /$$            /$$$$$$   /$$$$$$ ~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~| $$      |__/           /$$__  $$ /$$__  $$~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~| $$$$$$$  /$$ /$$$$$$$ | $$  \__/| $$  \ $$~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~| $$__  $$| $$| $$__  $$| $$ /$$$$| $$  | $$~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~| $$  \ $$| $$| $$  \ $$| $$|_  $$| $$  | $$~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~| $$  | $$| $$| $$  | $$| $$  \ $$| $$  | $$~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~| $$$$$$$/| $$| $$  | $$|  $$$$$$/|  $$$$$$/~~~~~~~~~~~~~~~~~~~~~
~~~~~~~~~~~~~~~~~~~~~|_______/ |__/|__/  |__/ \______/  \______/ ~~~~~~~~~~~~~~~~~~~~~
```                                    
# Integration Tests

This directory contains integration tests for binGO-CLI, specifically testing the multiplayer game flow with server coordination.

## Running Tests

All integration tests use the `integration` build tag and are located in this directory.

**Run all integration tests:**
```bash
go test -tags=integration ./tests -v
```

**Run specific test:**
```bash
go test -tags=integration ./tests -run TestMultiplayerGameFlow -v
```

## Test Files

### `multiplayer_test.go`
Tests the complete multiplayer game flow with server coordination:

**TestMultiplayerGameFlow:**
1. Starts a WebSocket server on port 9999
2. Connects 2 test clients simultaneously
3. Simulates gameplay (Player 1 wins with cells 7, 8, 9)
4. Verifies correct winner determination
5. Confirms game_ended broadcast to all players

**Key aspects tested:**
- Server accepts multiple concurrent players
- Game state coordination between players
- Win detection (local vs. server)
- Broadcast messaging (game_ended)
- Correct loser/winner identification

**Security Tests:**

**TestIPSpoofing:**
- Documents current auth vulnerability where different sources can claim same username
- Baseline test: PASSES (duplicate check prevents hijacking, but IP-binding is missing)
- After Phase 7.2: SHOULD FAIL (IP-bound JWT makes spoofing cryptographically impossible)

**TestIPSpoofingDetectionAfterAuth:**
- Placeholder test for IP-bound JWT validation
- Tests that tokens are rejected when used from different IP
- Currently SKIPPED until Phase 7.2 implementation

## Unit Tests

Comprehensive unit tests for the `shared` package are located in `shared/shared_test.go`:
- Board creation and initialization
- Cell ID generation (numpad layout for 3x3)
- Cell ID parsing
- Cell marking with error handling
- Game session creation
- Win detection across all winning patterns (rows, columns, diagonals)
- Display utility functions (text centering, strikethrough)

**Run shared unit tests:**
```bash
go test ./shared -v
```

## Running All Tests

**Unit tests only (fast):**
```bash
go test ./...
```

**Unit + integration tests:**
```bash
go test -tags=integration ./...
```

Integration tests use the `integration` build tag to exclude from standard runs since they start servers and are slower.

## CI/CD Integration

**GitHub Actions workflow (TODO):**
```yaml
- Run unit tests: go test ./...
- Run integration tests: go test -tags=integration ./tests -v
- Fail PR if either fails
```
