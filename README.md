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

A terminal bingo game written in Go that reads phrases from `buzzwords.csv` and displays a 3x3 bingo board. Supports both single-player standalone mode and multiplayer mode via WebSocket.

## Why this repo
- Quick fun CLI for meetings and random bingo-style phrases
- Single-player mode (no dependencies)
- Multiplayer support via WebSocket

## Requirements

**To play immediately:** Just download a prebuilt binary (see Quick Start above)—no setup needed!

**To build from source:**
- Go 1.25+ (the project `go.mod` currently specifies `go 1.25.3`)

## Quick Start - Prebuilt Binaries

Pre-compiled binaries are available in GitHub Releases for:
- **macOS Intel (base)**: `binGO-CLI-intel-mac`
- **Linux x86_64**: `binGO-CLI-linux`

### Download & Run (No Setup Required)

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
   Find your server's local IP with `ifconfig | grep "inet "` (macOS) or `hostname -I` (Linux)

#### Option 2: Internet via ngrok (for testing with remote friends)

ngrok creates a public tunnel to your local server using a reverse proxy. Your machine initiates an outgoing connection to ngrok's servers, which then routes inbound traffic from the internet back through that connection—bypassing ISP firewalls that block direct inbound connections. Perfect for testing multiplayer across the internet without cloud hosting.

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

## Board Sizes
- **3x3 Speed Bingo** (current): Quick 9-cell game with numpad numbers 1-9
- **5x5 Classic Bingo** (planned): Traditional 25-cell board with B-I-N-G-O letters and numbers 1-5

## Architecture

```
binGO-CLI/
├── bin.go                 # Main entry point
├── client/                # Multiplayer CLI client
│   ├── auth.go            # Local session management (token storage, username prompts)
│   ├── player.go          # Connection, board sync, input handling
│   └── types.go           # Client message types
├── server/                # Multiplayer WebSocket server
│   ├── auth.go            # JWT token generation & validation (IP-bound)
│   ├── server.go          # WebSocket handler, game coordination
│   ├── game.go            # Player & Game structs with thread-safe operations
│   ├── types.go           # Message types
│   └── server_test.go     # Unit tests
├── shared/                # Shared game logic (all modes)
│   ├── board.go           # Board management & cell marking
│   ├── display.go         # Terminal rendering
│   ├── game.go            # Game session & win detection
│   ├── shared_test.go     # 37 unit tests (board, win detection, display)
│   ├── types.go           # Type definitions
│   └── utils.go           # CSV loading utilities
├── standalone/            # Single-player mode
│   └── player.go          # Game loop & input handling
├── tests/                 # Integration tests
│   ├── multiplayer_test.go # 7 integration tests (game flow, edge cases, security)
│   └── README.md          # Test documentation
├── buzzwords.csv          # Sample dataset
└── binGO-CLI*             # Prebuilt binaries (macOS, Linux)
```

## Data
`buzzwords.csv` is included as a sample dataset. If you replace it with your own file, keep the same CSV format (one phrase per row, first column used).

## Testing
https://github.com/jkMLnop/binGO-CLI/blob/main/tests/README.md

## CI/CD & Releases

This repo uses GitHub Actions to automatically:
- **Test**: Run full test suite on every push and PR
- **Build**: Compile binaries for macOS Intel and Linux x86_64
- **Release**: Create GitHub releases with checksums when you push a tag

### Creating a Release

Simply tag your commit:
```bash
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions will:
1. Run all tests
2. Build binaries for both platforms
3. Create a release with both binaries + SHA256 checksums
4. Binaries available at: `https://github.com/jkMLnop/binGO-CLI/releases`

(Free tier GitHub Actions: 2,000 minutes/month—this workflow uses ~2 min per run)

## TODO
- Phase 7.3: Game access control (join codes, private games)
- Phase 7.4: Rate limiting & DDoS protection (in multiplayer testing ngrok handles this for now)
- Phase 8: Features (leaderboards, classic 5x5 mode, chat)
