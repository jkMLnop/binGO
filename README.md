# binGO-CLI

A small terminal bingo CLI game written in Go that reads phrases from `buzzwords.csv` and displays a 3x3 bingo board. Mark cells by entering numbers 1-9 for corresponding positions on numpad.

## Why this repo
- Quick fun CLI for meetings and random bingo-style phrases
- Minimal dependency: standard library only

## Requirements
- Go 1.25+ (the project `go.mod` currently specifies `go 1.25.3`)

## Build & Run

Build a local binary:

```bash
cd /path/to/binGO-CLI
go build -o binGO
# run
./binGO
```

Or run directly with:

```bash
go run .
```

## Usage
- The program reads `buzzwords.csv` in the project root and uses the first column of each row.
- Enter a number 1-9 on numpad to mark corresponding cell; enter `q` to quit.

## Data
`buzzwords.csv` is included as a sample dataset. If you replace it with your own file, keep the same CSV format (one phrase per row, first column used).

## TODO
- Consider adding a GitHub Actions workflow to run `go vet`/`go test` on PRs (optional).
