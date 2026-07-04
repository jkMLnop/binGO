# GitHub Copilot Instructions — binGO

## Project Overview

Multiplayer bingo game server. Module: `github.com/jkMLnop/binGO` · Go 1.25.3 · CGO required (SQLite). Web client served via Go embed.

Full architecture, commands, and context: see `claude.md` at the repo root.

## Branching Strategy (GitHub Flow + Git Worktrees)

- `main` is always deployable. CI deploys to staging on every push to `main`; tags `v*` deploy to production.
- New work goes on short-lived `feat/<name>` branches.
- One branch per roadmap task — keep scope small.
- Never commit directly to `main`. Always go via a `feat/` branch.

**Parallel development via git worktrees:** Each `feat/` branch lives in its own worktree directory.

```bash
git worktree add ../binGO-feat-<name> -b feat/<name>
# Open ../binGO-feat-<name> in a new VS Code window
```

## Testing

- Every new function or behaviour must have a corresponding test.
- Unit tests live alongside source files (`*_test.go`). Integration/container tests live in `tests/`.
- Use the correct build tag for the test tier:
  - No tag → unit tests (fast, no infrastructure)
  - `-tags=integration` → DB + API tests (SQLite only, no Docker)
  - `-tags=container` → Testcontainers (Docker required)
  - `-tags=e2e` → requires `docker-compose up`
- Call `ResetMetrics()` at the start of any test that instantiates `NewServer()` to avoid Prometheus duplicate-registration panics.
- Do not write tests that require external infrastructure unless using the correct build tag to isolate them.
- Extend regression coverage whenever functionality changes.

## Code Quality

### No Dead Code
Remove unused functions, variables, imports, and types rather than commenting them out. If something is scaffolded for future use, add a `// TODO(phaseN):` comment explaining why.

### No Duplicate Logic
Before adding a helper, check whether the logic already exists. Centralise shared logic within the relevant package (`server/`, `db/`, etc.).

### Idiomatic Go
- Wrap errors at boundaries: `fmt.Errorf("context: %w", err)`
- Record error metrics on every server error path: `s.Metrics.RecordError("type")` — valid types: `auth`, `game`, `db`, `ws`, `input`, `llm`
- Respect mutex discipline: `Game.PlayersMu` for `Players` map, `Player.wsMu` for WebSocket conn, `Server.GamesMu` for `Games`/`CodeToGame` maps
- All `server/db.go` helpers must remain nil-safe (`if store == nil { return nil }`)
- Use table-driven tests with `t.Run()` for multiple cases
- Pass `context.Context` as the first argument where applicable
- Prefer explicit returns over named return values
