# Phase 8: Monitoring & Observability Setup Guide

## Overview

This guide walks through setting up the complete monitoring stack for Phase 8:
- **Prometheus**: Metrics collection and storage
- **Grafana**: Dashboards and visualization
- **Structured JSON Logging**: Server-side event logging
- **Alert Rules**: Automated alerting on error conditions

## Local Development Setup

### 1. Configure Credentials

A `.env.example` file is provided with all supported variables and their safe defaults.
Copy it and fill in real values before starting the stack:

```bash
cp .env.example .env
# Edit .env with your preferred editor
```

The `.env` file is gitignored — never commit it.

**Environment variables:**

| Variable | Default (fallback) | Description |
|---|---|---|
| `GRAFANA_USER` | `admin` | Grafana UI login username |
| `GRAFANA_PASSWORD` | `change_me_in_production` | Grafana UI login password |
| `ADMIN_API_KEY` | `dev-admin-key-local-only` | Secret for authenticating admin API requests |
| `LOG_LEVEL` | `info` | Server log verbosity (`debug`, `info`, `warn`, `error`) |

**For production**, generate a strong admin key:

```bash
openssl rand -hex 32
```

Then set it in `.env` and also provide it to any admin clients or CI pipelines that call the admin API.

> **Note:** If no `.env` file is present, `docker-compose` will use the fallback defaults shown above. The fallback `ADMIN_API_KEY` is intentionally weak (`dev-admin-key-local-only`) to make it obvious if it leaks — never use it outside localhost.

### 2. Start the Complete Stack

```bash
cd /Users/jj/Documents/binGO-CLI

# Start all services (bingo-server, prometheus, grafana)
docker-compose up -d

# Verify services are running
docker-compose ps
```

This will:
- Build and start the bingo-server on `http://localhost:8080`
- Start Prometheus on `http://localhost:9090`
- Start Grafana on `http://localhost:3000`

### 3. Access the Services

**Prometheus UI:**
- URL: `http://localhost:9090`
- Explore metrics at: `http://localhost:9090/graph`
- View targets at: `http://localhost:9090/targets`

**Grafana UI:**
- URL: `http://localhost:3000`
- Login with the credentials configured in `.env` (or defaults for local dev)
- Datasource (Prometheus) should auto-configure
- Dashboard "binGO Server - Phase 8 Monitoring" should be auto-loaded

**Bingo Server:**
- WebSocket: `ws://localhost:8080/ws`
- HTTP API: `http://localhost:8080/api/...`
- Health check: `http://localhost:8080/api/status`
- Metrics endpoint: `http://localhost:8080/metrics` (Prometheus format)

### 4. Verify Metrics Collection

1. Open `http://localhost:9090/targets` and confirm `bingo-server` target is UP
2. Query a metric in Prometheus console:
   - Search: `bingo_game_count`
   - Expected: Current number of active games

3. Check the Grafana dashboard:
   - Login with your configured credentials
   - Go to Dashboards → "binGO Server - Phase 8 Monitoring"
   - Panels should show:
     - Games Created Per Minute
     - Active Games (stat)
     - Connected Players (stat)
     - Game Creation Latency (percentiles)
     - Database Query Latency (percentiles)
     - Error Rate

### 5. Test Metrics Collection

Start a game to generate metrics:

```bash
# In one terminal, run the server if not in Docker
./binGO -mode server -port 8080

# In another terminal, connect a client
./binGO -mode client -server localhost:8080

# Or use the prebuilt binary
./binGO-CLI-intel-mac -mode server -port 8080
```

Once players join and play, check:
1. Grafana dashboards update in real-time
2. Metrics appear in Prometheus at `http://localhost:9090/graph`

## Metrics Reference

### Gauges (Current Value)
- `bingo_game_count` - Active games currently running
- `bingo_player_count` - Players currently connected

### Counters (Cumulative)
- `bingo_games_created_total` - Total games created (since server start)
- `bingo_players_connected_total` - Total players who connected
- `bingo_players_disconnected_total` - Total players who disconnected
- `bingo_game_archived_total` - Total games archived
- `bingo_game_restarted_total` - Total games restarted
- `bingo_errors_total` - Total errors by type

### Histograms (Latency Distribution)
- `bingo_game_creation_duration_ms` - Game creation latency
- `bingo_database_query_duration_ms` - DB query latency

## Structured Logging

All significant events are logged as JSON to stdout with fields:
- `timestamp`: RFC3339 format
- `level`: INFO, ERROR, WARN
- `event_type`: game_created, player_joined, database_query, etc.
- `message`: Human-readable description
- `details`: Event-specific metadata

Example JSON log:
```json
{
  "timestamp": "2026-01-28T15:30:45Z",
  "level": "INFO",
  "event_type": "player_joined",
  "message": "Player joined",
  "details": {
    "game_id": "abc123",
    "player_id": "player456",
    "username": "Alice"
  }
}
```

View logs in real-time:
```bash
docker-compose logs -f bingo-server
```

## Alert Rules

Alert rules are defined in `alert-rules.yml` and check:

1. **High Error Rate** (ERROR_RATE > 5%)
   - Triggers if error rate exceeds 5% for 5 minutes
   - Severity: WARNING

2. **High Game Creation Latency** (p95 > 500ms)
   - Triggers if 95th percentile creation time exceeds 500ms for 5 minutes
   - Severity: WARNING

3. **High Database Latency** (p95 > 250ms)
   - Triggers if 95th percentile DB query time exceeds 250ms for 5 minutes
   - Severity: WARNING

4. **No Active Games**
   - Triggers when game count reaches 0
   - Severity: INFO

View alert rules in Prometheus:
- `http://localhost:9090/rules`

Current alert status:
- `http://localhost:9090/alerts`

## Dashboard Panels

The Grafana dashboard includes:

1. **Games Created Per Minute** - Rate of game creation (rolling 1-min window)
2. **Active Games** - Current number of games (stat card)
3. **Connected Players** - Current number of connected players (stat card)
4. **Game Creation Latency** - p50, p95, p99 percentiles over time
5. **Database Query Latency** - p50, p95, p99 percentiles over time
6. **Error Rate** - 5-minute average error rate over time

Thresholds are color-coded:
- GREEN: Normal (<250ms latency, <5% error rate)
- YELLOW: Caution (250-500ms latency, 2.5-5% error rate)
- RED: Alert (>500ms latency, >5% error rate)

## Stopping the Stack

```bash
# Stop all containers (keep data)
docker-compose down

# Stop and remove all data
docker-compose down -v
```

## Files Added in Phase 8

```
prometheus.yml                           - Prometheus configuration
alert-rules.yml                          - Alert rule definitions
grafana-datasources.yml                  - Grafana datasource config
grafana-dashboards/bingo-dashboard.json - Dashboard definition
grafana-provisioning/                    - Grafana provisioning configs
server/metrics.go                        - Prometheus metrics definitions
server/logger.go                         - Structured JSON logger
docker-compose.yml                       - Updated with Prometheus & Grafana
go.mod                                   - Added prometheus/client_golang
```

## Next Steps

1. Test the complete stack locally with `docker-compose up`
2. Create some games and check metrics in Prometheus and Grafana
3. Verify JSON logs appear in container output
4. Once validated, we'll move to:
   - Multi-game stability testing
   - Dagger CI/CD pipeline
   - Game lifecycle management
   - Security hardening
