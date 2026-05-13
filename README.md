```bash
 /$$       /$$            /$$$$$$   /$$$$$$ 
| $$      |__/           /$$__  $$ /$$__  $$
| $$$$$$$  /$$ /$$$$$$$ | $$  \__/| $$  \ $$
| $$__  $$| $$| $$__  $$| $$ /$$$$| $$  | $$
| $$  \ $$| $$| $$  \ $$| $$|_  $$| $$  | $$
| $$  | $$| $$| $$  | $$| $$  \ $$| $$  | $$
| $$$$$$$/| $$| $$  | $$|  $$$$$$/|  $$$$$$/
|_______/ |__/|__/  |__/ \______/  \______/ 

```
# binGO-CLI

![Bingo Demo](bingo_demo.gif)

A terminal bingo game written in Go for quick fun in meetings. Reads phrases from `buzzwords.csv` and displays a 3x3 bingo board. Supports single-player (no dependencies) and multiplayer via WebSocket.

## Requirements

**To play immediately:** Just download a prebuilt binary (see Quick Start)—no setup needed!

**To build from source:**
- Go 1.25+ (the project `go.mod` currently specifies `go 1.25.3`)

## Quick Start - Prebuilt Binaries

Pre-compiled binaries are available in GitHub Releases for:
- **macOS Intel (base)**: `binGO-CLI-intel-mac`
- **Linux x86_64**: `binGO-CLI-linux`

### Download & Run

1. **Download** the binary for your platform from the [latest release](https://github.com/jkMLnop/binGO-CLI/releases):
   ```bash
   # macOS Intel
   wget https://github.com/jkMLnop/binGO-CLI/releases/latest/download/binGO-CLI-intel-mac
   chmod +x binGO-CLI-intel-mac
   ./binGO-CLI-intel-mac -mode standalone
   
   # Linux x86_64
   wget https://github.com/jkMLnop/binGO-CLI/releases/latest/download/binGO-CLI-linux
   chmod +x binGO-CLI-linux
   ./binGO-CLI-linux -mode standalone
   ```

2. **Or download manually:**
   - Visit [binGO-CLI Releases](https://github.com/jkMLnop/binGO-CLI/releases)
   - Download the binary for your OS
   - `chmod +x` the downloaded file
   - Run it: `./binGO-CLI-intel-mac -mode standalone` (or `-linux` for Linux)

## Build from Source

```bash
# Build
cd /path/to/binGO-CLI
go build -o binGO-CLI
chmod +x binGO-CLI

# Or run directly without building
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

### Standalone Mode
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
   Find your server's local IP with `ifconfig | grep "inet " | grep -v 127.0.0.1 | head -1 | awk '{print $2}'` (macOS) or `hostname -I` (Linux)

**Note:** Local network connections automatically join the game without requiring a code.

#### Option 2: Cloud Server (Fly.io)

The easiest way to play remotely—just connect to the public production server:

```bash
./binGO-CLI -mode client -server yubetcha.com -code GAME-CODE
```

Replace `GAME-CODE` with the code from a friend hosting a game, or start your own game by having someone run the server locally and share the code.

#### Option 3: Internet via ngrok (with Game Code)

ngrok creates a public tunnel to your local server using a reverse proxy. Your machine initiates an outgoing connection to ngrok's servers, which then routes inbound traffic from the internet back through that connection—bypassing ISP firewalls that block direct inbound connections. Perfect for testing multiplayer across the internet without cloud hosting.

**Important:** Remote connections via ngrok require a game code for security. Codes are automatically generated and displayed to all connected players.

**Requirements:**
- ngrok **3.0.0+** (tested with 3.34.1). Free tier requires HTTPS/WSS connections. [Download here](https://ngrok.com/download)

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
   Forwarding    https://abc123xyz.ngrok-free.dev -> http://localhost:8080
   ```
   _(Note: ngrok 3.0+ free tier requires HTTPS/WSS. The client automatically detects ngrok domains and uses secure WebSocket (`wss://`) connections.)_

6. **Share the ngrok URL and game code with friends.** They connect with:
   ```bash
   ./binGO-CLI -mode client -server abc123xyz.ngrok-free.dev -code BINGO-XXXXX
   ```
   Replace `BINGO-XXXXX` with the actual game code shown on the server.
   
   _(The client auto-detects ngrok domains and connects securely without needing to specify `wss://`)_

### Gameplay

- Enter a number (1-9) to mark a cell
- Enter `board` to redisplay the board
- First player to get 3 in a row (horizontal, vertical, diagonal) wins
- Winner sees a celebration animation, all players exit

## Admin API

The Admin API allows you to programmatically create, list, and manage games. All endpoints require authentication via the `X-Admin-Key` header.

### Quick Start

```bash
# Start server
./binGO-CLI -mode server -port 8080

# In another terminal, set credentials
export ADMIN_KEY="dev-admin-key-local-only"
export BASE_URL="http://localhost:8080"

# Create a game
curl -X POST $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"

# List all games
curl -X GET $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"

# Get game details
curl -X GET $BASE_URL/admin/api/games/game-1 -H "X-Admin-Key: $ADMIN_KEY"

# Delete a game
curl -X DELETE $BASE_URL/admin/api/games/game-1 -H "X-Admin-Key: $ADMIN_KEY"
```

**For full documentation, see [docs/ADMIN_API.md](docs/ADMIN_API.md)**

Configuration: Set `ADMIN_API_KEY` environment variable to customize the admin key for production.

## Board Sizes
- **3x3 Speed Bingo** (current): Quick 9-cell game with numpad numbers 1-9
- **5x5 Classic Bingo** (planned): Traditional 25-cell board with B-I-N-G-O letters and numbers 1-5

## Architecture

```
binGO-CLI/
├── bin.go                      # Main entry point & CLI modes
├── client/                     # Multiplayer CLI client
│   ├── auth.go                 # Local session management (token storage, username prompts)
│   ├── auth_test.go            # Auth tests
│   ├── player.go               # Connection, board sync, input handling
│   ├── display.go              # Client-side UI rendering
│   └── types.go                # Client message types
├── server/                     # Multiplayer WebSocket server
│   ├── auth.go                 # JWT token generation & validation (IP-bound)
│   ├── auth_test.go            # Auth unit tests
│   ├── server.go               # WebSocket handler, game coordination
│   ├── server_test.go          # Server unit tests
│   ├── game.go                 # Player & Game structs with thread-safe operations
│   ├── api.go                  # REST API endpoints (game lookup, leaderboard, status)
│   ├── api_test.go             # API endpoint tests
│   ├── db.go                   # Database integration & helpers
│   ├── player_db.go            # Player database tracking
│   ├── types.go                # Message types
│   ├── utils.go                # Utility functions
│   └── utils_test.go           # Utility tests
├── shared/                     # Shared game logic (all modes)
│   ├── board.go                # Board management, cell marking, win detection
│   ├── board_test.go           # Board unit tests
│   ├── display.go              # Terminal rendering and formatting
│   ├── display_test.go         # Display unit tests
│   ├── types.go                # Type definitions
│   ├── utils.go                # CSV loading utilities
│   └── utils_test.go           # Utility tests
├── standalone/                 # Single-player mode
│   └── player.go               # Game loop & input handling
├── db/                         # Database layer (Phase 7.5)
│   ├── store.go                # GameStore interface (abstract DB operations)
│   ├── sqlite.go               # SQLite implementation with full CRUD
│   └── sqlite_test.go          # Database unit tests
├── docs/                       # Documentation
│   ├── ROADMAP.md              # Development phases and roadmap
│   ├── DEPLOYMENT.md           # Cloud deployment guide (Fly.io)
│   └── MONITORING_SETUP.md     # Monitoring & observability setup
├── tests/                      # Integration & regression tests
│   ├── multiplayer_test.go         # 12+ multiplayer integration tests
│   ├── db_integration_test.go      # 7 database persistence tests
│   ├── container_e2e_test.go       # Testcontainers-based E2E tests
│   ├── container_regression_test.go # Container regression suite
│   ├── full_system_load_test.go    # E2E load test (requires docker-compose)
│   ├── README.md                   # Test documentation
│   └── REGRESSION_TESTS.md         # Manual regression test checklist
├── dagger/                     # Dagger CI/CD pipeline (separate Go module)
│   ├── main.go                 # Pipeline: test, build, publish, deploy, release
│   └── main_test.go            # Pipeline unit tests
├── .github/
│   └── workflows/
│       └── ci.yml              # GitHub Actions (thin trigger → Dagger)
├── .lefthook.yml               # Git pre-push hooks (enforces tests before push)
├── Dockerfile                  # Multi-stage Alpine build
├── docker-compose.yml          # bingo-server + Prometheus + Grafana
├── fly.toml                    # Fly.io production config
├── fly.staging.toml            # Fly.io staging config
├── prometheus.yml              # Prometheus scrape config
├── buzzwords.csv               # Default sample dataset
├── buzzwords_full.csv          # Extended buzzword set
├── bingo.db                    # SQLite database (created with -db flag)
├── go.mod                      # Go module dependencies
├── go.sum                       # Dependency checksums
├── CHANGELOG.md                # Version history
├── LICENSE                     # MIT license
└── binGO*                      # Prebuilt binaries (macOS, Linux)
    ├── binGO                   # Apple Silicon binary (arm64)
    ├── binGO-CLI-intel-mac     # Intel Mac binary (amd64)
    └── binGO-CLI-linux         # Linux binary (amd64)
```

## Data
`buzzwords.csv` is included as a sample dataset. If you replace it with your own file, keep the same CSV format (one phrase per row, first column used).

## Testing

```bash
# Unit tests (fast, no Docker)
go test ./...

# Unit + integration tests
go test -tags=integration ./tests -v

# Container regression tests (Docker must be running)
go test -tags=container -timeout=10m ./tests -v

# Run the same pipeline CI uses (via Dagger)
cd dagger && go run . test
```

See [tests/README.md](tests/README.md) for full test documentation.

## CI/CD

All pipeline logic lives in `dagger/main.go` (a separate Go module). GitHub Actions (`.github/workflows/ci.yml`) is a thin trigger layer that calls Dagger functions. [Lefthook](.lefthook.yml) enforces the same pipeline locally before every `git push`.

| Trigger | Pipeline |
|---------|----------|
| PR to `main` | `dagger test` (unit + integration) |
| Push to `main` | `dagger test` → build Docker image → publish to ghcr.io → deploy to staging (Fly.io) |
| Tag `v*` | Full pipeline → deploy to production (Fly.io) + GitHub Release with cross-compiled binaries |

### Creating a Release

Tag a commit and push — the pipeline runs automatically:
```bash
git tag v2.0.0
git push origin v2.0.0
```

GitHub Actions will run the full pipeline and create a GitHub Release with cross-compiled binaries (macOS Intel, Linux x86_64) and SHA256 checksums.

### Local pre-push enforcement (Lefthook)

```bash
go install github.com/evilmartians/lefthook@latest && lefthook install
# Every git push now runs unit+integration tests (via Dagger) and container tests automatically
git push --no-verify  # bypass in emergencies
```

## Project Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md) for the development roadmap and upcoming phases.
