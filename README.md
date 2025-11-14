# binGO-CLI

A terminal bingo game written in Go that reads phrases from `buzzwords.csv` and displays a 3x3 bingo board. Supports both single-player standalone mode and multiplayer mode via WebSocket.

## Why this repo
- Quick fun CLI for meetings and random bingo-style phrases
- Single-player mode (no dependencies)
- Multiplayer support via WebSocket (in development)

## Requirements
- Go 1.25+ (the project `go.mod` currently specifies `go 1.25.3`)

## Build & Run

### Build a local binary:

```bash
cd /path/to/binGO-CLI
go build -o binGO
./binGO -mode standalone
```

### Run directly:

```bash
go run . -mode standalone
```

## Modes

- **`standalone`** (default): Single-player game, no networking
  ```bash
  go run . -mode standalone
  ```

- **`server`**: Start WebSocket server (multiplayer)
  ```bash
  go run . -mode server
  ```

- **`client`**: Connect to a WebSocket server and play multiplayer
  ```bash
  go run . -mode client -server localhost:8080
  ```

- **`both`**: Dev mode - start server and client on the same machine
  ```bash
  go run . -mode both
  ```

## Usage

### Standalone Mode (Speed Bingo - 3x3)
- The program reads `buzzwords.csv` in the project root and uses the first column of each row.
- Each cell displays its numpad number (1-9) in the top-left, with the phrase centered below.
- Enter a number 1-9 on numpad to mark the corresponding cell; enter `q` to quit.
- Win by marking three in a row (horizontal, vertical, or diagonal).

### Multiplayer Modes (In Development)
- Server manages game state and broadcasts updates to all connected clients.
- When one player wins, the game ends for all players.
- Each player gets their own random board

## Board Sizes (Planned)
- **3x3 Speed Bingo** (current): Quick 9-cell game with numpad numbers 1-9
- **5x5 Classic Bingo** (planned): Traditional 25-cell board with B-I-N-G-O letters

## Data
`buzzwords.csv` is included as a sample dataset. If you replace it with your own file, keep the same CSV format (one phrase per row, first column used).

## TODO
- Streamline display.go and (for speed bingo) add 2 left/right spaces to each card cell while keeping number left-aligned
- Implement 5x5 classic bingo mode
- Add dynamic countdown that serves animation once it's ready
- Implement WebSocket server mode
- Implement WebSocket client mode
- Add support for web clients (HTML/JavaScript)
- Add support for mobile clients (iOS/Android via WebSocket)
- Consider adding GitHub Actions workflow to run `go vet`/`go test` on PRs
