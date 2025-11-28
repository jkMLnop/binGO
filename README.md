# binGO-CLI

A terminal bingo game written in Go that reads phrases from `buzzwords.csv` and displays a 3x3 bingo board. Supports both single-player standalone mode and multiplayer mode via WebSocket.

## Why this repo
- Quick fun CLI for meetings and random bingo-style phrases
- Single-player mode (no dependencies)
- Multiplayer support via WebSocket

## Requirements
- Go 1.25+ (the project `go.mod` currently specifies `go 1.25.3`)
- OR use pre-built binaries (see below)

## Quick Start - Prebuilt Binaries

Pre-compiled binaries are available in this repo for:
- **macOS Intel**: `binGO-CLI` or `binGO-CLI-intel-mac` (symlink to base binary)
- **Linux x86_64**: `binGO-CLI-linux`

Download the appropriate binary and run:
```bash
chmod +x binGO-CLI*
./binGO-CLI -mode standalone          # macOS Intel
./binGO-CLI-linux -mode standalone    # Linux
```

## Build & Run

### Build a local binary:

```bash
cd /path/to/binGO-CLI
go build -o binGO-CLI
./binGO-CLI -mode standalone
```

### Run directly:

```bash
go run . -mode standalone
```

## Modes

- **`standalone`** (default): Single-player game, no networking
  ```bash
  ./binGO-CLI -mode standalone
  ```

- **`server`**: Start WebSocket server (multiplayer)
  ```bash
  ./binGO-CLI -mode server -port 8080
  ```

- **`client`**: Connect to a WebSocket server and play multiplayer
  ```bash
  ./binGO-CLI -mode client -server localhost:8080
  ```

## Usage

### Standalone Mode (Speed Bingo - 3x3)
- The program reads `buzzwords.csv` in the project root and uses the first column of each row.
- Each cell displays its numpad number (1-9) in the top-left, with the phrase centered below.
- Enter a number 1-9 to mark the corresponding cell; enter `q` to quit.
- Win by marking three in a row (horizontal, vertical, or diagonal).

### Multiplayer Mode (Client/Server)

#### Option 1: Local Network
1. **On the server machine:**
   ```bash
   ./binGO-CLI -mode server -port 8080
   ```

2. **On client machines (same WiFi):**
   ```bash
   ./binGO-CLI -mode client -server <server-ip>:8080
   ```
   Find your server's local IP with `ifconfig | grep "inet "` (macOS) or `hostname -I` (Linux)

#### Option 2: Internet via ngrok (for testing with remote friends)

ngrok creates a public tunnel to your local server. Perfect for testing multiplayer across the internet without cloud hosting.

1. **Install ngrok** (free account required):
   ```bash
   brew install ngrok  # macOS
   # or visit https://ngrok.com/download
   ```

2. **Create free account** at https://dashboard.ngrok.com/signup and get your authtoken from https://dashboard.ngrok.com/get-started/your-authtoken

3. **Add authtoken:**
   ```bash
   ngrok config add-authtoken YOUR_TOKEN_HERE
   ```

4. **Start your server:**
   ```bash
   ./binGO-CLI -mode server -port 8080
   ```

5. **In another terminal, expose with ngrok:**
   ```bash
   ngrok http 8080
   ```
   You'll see output like:
   ```
   Forwarding    http://abc123xyz.ngrok-free.dev -> http://localhost:8080
   ```

6. **Share the ngrok URL with friends.** They connect with:**
   ```bash
   ./binGO-CLI -mode client -server abc123xyz.ngrok-free.dev
   ```

### Gameplay
- Enter a number (1-9) to mark a cell
- Enter `board` to redisplay the board
- First player to get 3 in a row (horizontal, vertical, diagonal) wins
- Winner sees a celebration animation, all players exit

## Board Sizes (Planned)
- **3x3 Speed Bingo** (current): Quick 9-cell game with numpad numbers 1-9
- **5x5 Classic Bingo** (planned): Traditional 25-cell board with B-I-N-G-O letters

## Architecture

```
binGO-CLI/
├── bin.go                 # Main entry point
├── shared/                # Shared game logic (all modes)
│   ├── types.go           # Type definitions
│   ├── board.go           # Board management & cell marking
│   ├── game.go            # Game session & win detection
│   ├── display.go         # Terminal rendering
│   └── utils.go           # CSV loading utilities
├── standalone/            # Single-player mode
│   └── player.go          # Game loop & input handling
├── server/                # Multiplayer WebSocket server
│   ├── server.go          # WebSocket handler, game coordination
│   ├── game.go            # Player & Game structs with thread-safe operations
│   └── server_test.go     # Unit tests
├── client/                # Multiplayer CLI client
│   └── player.go          # Connection, board sync, input handling
├── buzzwords.csv          # Sample dataset
├── cert.pem               # Self-signed SSL cert (testing)
├── key.pem                # SSL key (testing)
└── binGO-CLI-*            # Prebuilt binaries
```

## Data
`buzzwords.csv` is included as a sample dataset. If you replace it with your own file, keep the same CSV format (one phrase per row, first column used).

## Testing

### Unit Tests
```bash
go test ./server -v -race
go test ./... -v
```

### Multiplayer Test (Server + 2 Connected Clients)

The `TestMultiplayerGameFlow` test verifies the complete multiplayer experience:
- Starts a WebSocket server
- Connects 2 clients simultaneously
- Player 1 marks cells 7, 8, 9 (top row) to win
- Player 2 marks cells 1, 2 (no win)
- Verifies Player 1 is declared winner
- Verifies both players receive game_ended broadcast
- Confirms losers don't incorrectly win

**Run:**
```bash
go test ./tests -tags=integration -run TestMultiplayerGameFlow -v
```

**What it tests:**
- Server game coordination (player joining, game state)
- Client win detection logic (CheckWin)
- Win announcement (client sends action:"win" to server)
- Broadcasting (game_ended sent to all connected players)
- Correct winner identification

**Test Organization:**
- Unit tests: `./server`, `./shared` directories
- Integration tests: `./tests` directory (run with `-tags=integration`)
- All tests: `go test ./...` (unit tests only)
- All tests + integration: `go test -tags=integration ./...`

**CI/CD Integration (TODO):**
- [ ] Add GitHub Actions workflow
- [ ] Run `go test ./...` on every PR (unit tests only)
- [ ] Run `go test -tags=integration ./tests -v` before merge (integration tests)
- [ ] Catch multiplayer regressions automatically
- [ ] Report coverage for shared/board.go and game.go

## Security Notes

**Current (testing):**
- Uses plain HTTP WebSocket (ws://)
- Works on local network and ngrok
- No authentication required

**Production (coming soon):**
- Will use HTTPS WebSocket (wss://)
- Let's Encrypt SSL certificates
- Authentication & rate limiting

## TODO
- **CI/CD Integration**: Add GitHub Actions workflow for E2E testing
- Phase 7.2: Authentication (JWT tokens, player login)
- Phase 7.3: Game access control (join codes, private games)
- Phase 7.4: Rate limiting & DDoS protection
- Phase 7.5: Server-side win validation
- Phase 8: Features (leaderboards, classic 5x5 mode, chat)
- Add coverage for shared/board.go and game.go
