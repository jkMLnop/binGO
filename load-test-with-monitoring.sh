#!/usr/bin/env bash
# load-test-with-monitoring.sh
#
# Run the full-system load test (e2e tag) against a configurable target and
# print a link to the relevant Grafana dashboard when done.
#
# Usage:
#   # Local docker-compose stack (default)
#   ./load-test-with-monitoring.sh
#
#   # Remote staging
#   LOAD_TEST_URL=https://bingo-server-staging.fly.dev \
#   ADMIN_API_KEY=your-staging-key \
#   GRAFANA_URL=https://your-org.grafana.net/d/bingo \
#   ./load-test-with-monitoring.sh
#
#   # Remote production
#   LOAD_TEST_URL=https://bingo-server.fly.dev \
#   ADMIN_API_KEY=your-prod-key \
#   GRAFANA_URL=https://your-org.grafana.net/d/bingo \
#   ./load-test-with-monitoring.sh

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
LOAD_TEST_URL="${LOAD_TEST_URL:-http://127.0.0.1:8080}"
ADMIN_API_KEY="${ADMIN_API_KEY:-dev-admin-key-local-only}"

# Grafana dashboard URL shown at the end of the run.
# For local docker-compose: http://localhost:3000
# For Grafana Cloud:        https://<your-org>.grafana.net/d/<dashboard-uid>
GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"

# ── Helpers ──────────────────────────────────────────────────────────────────
info()    { echo "▶  $*"; }
success() { echo "✓  $*"; }
warn()    { echo "⚠  $*" >&2; }

# ── Pre-flight ───────────────────────────────────────────────────────────────
info "Target : ${LOAD_TEST_URL}"
info "Admin  : ${ADMIN_API_KEY:0:8}…  (truncated)"
info "Grafana: ${GRAFANA_URL}"
echo ""

# Verify the server is reachable before spending time on test compilation.
if ! curl -sf "${LOAD_TEST_URL}/api/status" > /dev/null 2>&1; then
  warn "Server not reachable at ${LOAD_TEST_URL}/api/status"
  warn "Start the server first:  docker-compose up -d  OR  ./binGO -mode server"
  exit 1
fi
success "Server is reachable at ${LOAD_TEST_URL}"

# ── Run the load test ────────────────────────────────────────────────────────
info "Starting load test  (tags=e2e)…"
echo ""

START_TS=$(date +%s)

LOAD_TEST_URL="${LOAD_TEST_URL}" \
ADMIN_API_KEY="${ADMIN_API_KEY}" \
  go test -tags=e2e -timeout=5m -v ./tests -run TestFullSystemLoadWithPlayers "$@"

END_TS=$(date +%s)
DURATION=$(( END_TS - START_TS ))

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
success "Load test finished in ${DURATION}s"
echo ""
echo "┌─────────────────────────────────────────────────────────────┐"
echo "│  View metrics & dashboards                                   │"
echo "│  ${GRAFANA_URL}"
echo "│                                                              │"
echo "│  Useful PromQL queries:                                      │"
echo "│    rate(bingo_games_created_total[5m])                       │"
echo "│    rate(bingo_players_connected_total[5m])                   │"
echo "│    rate(bingo_rate_limited_total[5m])                        │"
echo "│    rate(bingo_errors_total[5m])                              │"
echo "└─────────────────────────────────────────────────────────────┘"
