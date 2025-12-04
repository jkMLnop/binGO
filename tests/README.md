# Integration Tests

This directory contains integration tests for binGO-CLI, specifically testing the multiplayer game flow with server coordination.

## Running Tests

**Run all tests:**
```bash
go test ./...
```

**Run only multiplayer integration tests:**
```bash
go test ./tests -v
```

**Run specific test:**
```bash
go test ./tests -run TestMultiplayerGameFlow -v
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
- Tests IP-bound JWT authentication from Phase 7.2
- Verifies that duplicate usernames from different IPs are rejected
- Status: ✅ PASSES (IP-binding prevents hijacking attacks)

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

**All tests including integration:**
```bash
go test -v ./...
```

Current status: **45/45 tests passing** ✅
- 37 unit tests (shared package: board, win detection, display)
- 8 integration tests (multiplayer: game flow, security, edge cases)

## CI/CD Integration

Tests are automatically run on every push and pull request via GitHub Actions.

See [CI/CD & Releases](../README.md#cicd--releases) in the main README for details.
