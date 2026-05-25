# GitHub Copilot Instructions — binGO-CLI

## Project Overview

Multiplayer CLI bingo game. Module: `github.com/jkMLnop/binGO-CLI` · Go 1.25.3 · CGO required (SQLite) · TypeScript web client in `web-client/`.

Full architecture, commands, and context: see `claude.md` at the repo root.

## Branching Strategy (GitHub Flow + Git Worktrees)

- `main` is always deployable. CI deploys to staging on every push to `main`; tags `v*` deploy to production.
- New work goes on short-lived `feat/<name>` branches, named after the roadmap task (e.g. `feat/phase10-postgres`, `feat/phase10-otel-exporter`).
- One branch per roadmap task — keep scope small.
- Merge one branch at a time after manual testing on staging. Never merge two feature branches simultaneously.
- Never commit directly to `main`. Always go via a `feat/` branch.

**Parallel development via git worktrees:** Each `feat/` branch lives in its own worktree directory so multiple VS Code windows (and Copilot chats) can work simultaneously without file conflicts.

```bash
# Start new feature work
git worktree add ../binGO-feat-<name> -b feat/<name>
# Open ../binGO-feat-<name> in a new VS Code window

# After merging to main, clean up
git worktree remove ../binGO-feat-<name>
git branch -d feat/<name>
```

If you are running inside a worktree directory (not the main repo), this is expected — commit and push normally to the `feat/` branch.

## Testing

- Every new function or behaviour must have a corresponding test.
- Unit tests live alongside source files (`*_test.go`, `*.test.ts`). Integration/container tests live in `tests/`.
- Use the correct build tag for the test tier:
  - No tag → unit tests (fast, no infrastructure)
  - `-tags=integration` → DB + API tests (SQLite only, no Docker)
  - `-tags=container` → Testcontainers (Docker required)
  - `-tags=e2e` → requires `docker-compose up`
- Call `ResetMetrics()` at the start of any test that instantiates `NewServer()` to avoid Prometheus duplicate-registration panics.
- Do not write tests that require external infrastructure unless using the correct build tag to isolate them.

## Code Quality

### No Dead Code
Remove unused functions, variables, imports, and types rather than commenting them out. If something is scaffolded for future use, add a `// TODO(phaseN):` comment explaining why it exists.

### No Duplicate Logic
Before adding a helper, check whether the logic already exists. Centralise shared logic:
- Go: within the relevant package (`server/`, `shared/`, `db/`, etc.)
- TypeScript: in `web-client/src/lib/`

Avoid copy-pasting logic across packages. If the same logic is needed in two places, extract it.

### Idiomatic Go
- Wrap errors at boundaries: `fmt.Errorf("context: %w", err)`
- Record error metrics on every server error path: `s.Metrics.RecordError("type")` — valid types: `auth`, `game`, `db`, `ws`, `input`
- Respect mutex discipline: `Game.PlayersMu` for `Players` map, `Player.wsMu` for WebSocket conn, `Server.GamesMu` for `Games`/`CodeToGame` maps
- All `server/db.go` helpers must remain nil-safe (`if store == nil { return nil }`)
- Use table-driven tests with `t.Run()` for multiple cases
- Pass `context.Context` as the first argument where applicable
- Prefer explicit returns over named return values

### Idiomatic TypeScript (web-client)
- Strict TypeScript — no `any` unless unavoidable; if used, comment why
- Prefer `const` over `let`; avoid `var`
- Define shared types in `web-client/src/lib/types.ts`
- Keep API calls in `web-client/src/lib/api.ts`, not in components
- Test with Vitest (`npx vitest run` inside `web-client/`)
