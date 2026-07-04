# DevOps & CI/CD

How binGO automates quality enforcement from developer laptop to production.

## Philosophy

**Automate guardrails as far as possible.** If an SOP (standard operating procedure) can be enforced by tooling, it should be. Human discipline is unreliable — tooling is not. The goal is a development workflow where doing the right thing is the path of least resistance, and cutting corners requires a deliberate `--no-verify` escape hatch.

This means:
- **Every `git push`** runs the full test suite locally (unit + integration + container regression) before code leaves the machine. Enforced by Lefthook, not by memory.
- **Every CI run** executes the same pipeline functions that developers run locally. Dagger is both the local tool and the CI engine — no "works on my machine" divergence.
- **Container tests are not replaced by deployment.** Deploying to staging proves the image builds and runs. Container tests prove behavioral correctness: graceful shutdown broadcasts to WebSocket clients, volume persistence across restarts, cleanup goroutines fire on boot, orphan detection works end-to-end. These are complementary layers, not redundant ones.

## Tool Roles

| Tool | Role | Analogy |
|---|---|---|
| **Dagger** (`dagger/main.go`) | The pipeline — test, build, publish, deploy, release | The engine |
| **Lefthook** (`.lefthook.yml`) | Triggers the pipeline at the right Git event | The ignition switch |
| **GitHub Actions** (`.github/workflows/ci.yml`) | Triggers the same pipeline in CI | The ignition switch (server-side) |
| **Fly.io** (`fly.toml`, `fly.staging.toml`) | The deployment target | Where the engine drives to |
| **ghcr.io** | Docker image registry | The parking lot between build and deploy |

No tool duplicates another. Lefthook and GitHub Actions both call Dagger functions — they differ only in *when* they fire (pre-push vs. CI), not in *what* they run.

## Test Tiers

| Tier | What runs | Where | Enforced by | Duration |
|---|---|---|---|---|
| **Unit + Integration** | `go test ./...` + `-tags=integration` | Dagger container (CGO + SQLite) | Lefthook pre-push + CI | ~30s |
| **Container Regression** | `-tags=container` via Testcontainers | Dagger container (Docker socket pass-through) | Lefthook pre-push | ~10min |
| **Build + Deploy** | Docker image build → ghcr.io push → Fly.io deploy | CI (GitHub Actions → Dagger) | CI on push to main / v* tag | ~3min |

### What only container tests catch

These behavioral regressions are invisible to unit tests and to a simple "does it deploy?" check:

- **SIGTERM → graceful shutdown broadcast** — `docker stop` sends SIGTERM, both WebSocket clients receive `server_shutdown` message before the process exits
- **Volume persistence across container restarts** — SQLite DB written in container A survives `docker stop` and is readable by container B on the same bind mount
- **Cleanup goroutine fires on boot** — stale game archives (>4 days) auto-deleted when a new container starts against an existing DB
- **Orphan detection end-to-end** — all WebSocket clients disconnect → game marked orphaned → rejoin with same code is rejected
- **Admin API status-code matrix** — 9+ combinations of auth state × CRUD operation through real Docker networking
- **50 concurrent admin requests** through container port mapping (not just in-process goroutines)

This is why container tests are run on every push, not just occasionally. They are the guardrail for a class of bugs that nothing else catches.

## Pipeline Functions

All pipeline stages are Go functions in `dagger/main.go`. Same binary runs locally and in CI.

```
go run ./dagger <command> [flags]

Commands:
  test             Unit + integration tests (~30s)
  test-container   Container regression suite (~10min, needs Docker)
  build            Build Docker image with version injection
  publish          Push image to ghcr.io
  deploy           Deploy to Fly.io (--env=staging|production)
  release          Cross-compile binaries + GitHub Release
  all              Full pipeline: test → build → publish → deploy

Flags:
  --version        Version tag (default: dev)
  --env            Target environment: staging or production
  --registry-user  ghcr.io username (for publish/all)
```

## CI Triggers

| Event | What runs | Why |
|---|---|---|
| PR to `main` | `test` | Fast feedback on proposed changes |
| Push to `main` | `all --env staging` | Deploy every merged change to staging |
| Tag `v*` | `all --env production` + `release` | Deploy to production + GitHub Release |

## Local Workflow

```bash
# One-time setup
go install github.com/evilmartians/lefthook@latest
lefthook install

# Normal development — Lefthook fires automatically
git add -A && git commit -m "phase 8.8: dagger pipeline"
git push  # ← Lefthook runs: dagger test + dagger test-container (~10min)

# Emergency bypass (you know what you're skipping)
git push --no-verify
```

## Environments

| Environment | Fly.io app | Config file | Deployed on | URL |
|---|---|---|---|---|
| **Staging** | `bingo-server-staging` | `fly.staging.toml` | Every push to `main` | `bingo-server-staging.fly.dev` |
| **Production** | `bingo-server` | `fly.toml` | `v*` tags | `bingo-server.fly.dev` |

Both environments have identical configuration (port 8080, SQLite volume at `/app/data`, same health check). The only difference is the app name.

## Required Secrets

| Secret | Where | Purpose |
|---|---|---|
| `FLY_API_TOKEN` | GitHub repo secrets + local env | Fly.io deployment |
| `GHCR_TOKEN` | Local env (CI uses `GITHUB_TOKEN`) | Push Docker images to ghcr.io |
| `GH_TOKEN` | Local env (CI uses `GITHUB_TOKEN`) | Create GitHub Releases |
| `ADMIN_API_KEY` | Fly.io secrets (both apps) | Admin API authentication in deployed server |

## Version Injection

Every deployed binary knows its exact version:

```
./binGO -version    # prints "v8.2.0" or "abc1234" (short SHA)
```

Injected at build time via `-ldflags "-X main.version=<value>"`:
- `bin.go` declares `var version = "dev"` (default for local builds)
- `Dockerfile` accepts `ARG VERSION=dev` and passes it to `go build`
- Dagger's `build` function passes the version arg through
- CI sets version to the git short SHA (staging) or tag name (production)

## Fly.io Setup (One-Time)

```bash
# Create staging app
flyctl apps create bingo-server-staging --org personal
flyctl volumes create bingo_data --region sjc --app bingo-server-staging
flyctl secrets set ADMIN_API_KEY=<your-key> --app bingo-server-staging

# Production already exists; ensure secrets are set
flyctl secrets set ADMIN_API_KEY=<your-key> --app bingo-server
```
