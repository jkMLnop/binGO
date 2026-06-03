#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <rw|ro> <base-url>"
  exit 2
fi

MODE="$1"
BASE_URL="$2"
HOST="${BASE_URL#http://}"
HOST="${HOST#https://}"
HOST="${HOST%%/*}"

if [[ -z "$HOST" ]]; then
  echo "invalid base url: $BASE_URL"
  exit 2
fi

TIMEOUT_BIN=""
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN="gtimeout"
fi

run_with_timeout() {
  if [[ -n "$TIMEOUT_BIN" ]]; then
    "$TIMEOUT_BIN" "$@"
  else
    shift
    "$@"
  fi
}

TMP_HOME="$(mktemp -d)"
trap 'rm -rf "$TMP_HOME"' EXIT

echo "Running CLI smoke mode=$MODE target=$HOST"

case "$MODE" in
  rw)
    # Read-write host-flow check: create a game, then exit via q.
    INPUT=$'1\n\ncli-smoke-user\nq\n'
    set +e
    OUTPUT="$(printf "%s" "$INPUT" | HOME="$TMP_HOME" run_with_timeout 60s ./binGO-CLI -mode client -server "$HOST" 2>&1)"
    EXIT_CODE=$?
    set -e

    echo "$OUTPUT"

    if [[ $EXIT_CODE -ne 0 ]]; then
      echo "CLI smoke rw failed: non-zero exit $EXIT_CODE"
      exit 1
    fi

    if ! grep -q "Game created! Code:" <<<"$OUTPUT"; then
      echo "CLI smoke rw failed: missing game creation output"
      exit 1
    fi

    if ! grep -q "Share this link:" <<<"$OUTPUT"; then
      echo "CLI smoke rw failed: missing share link output"
      exit 1
    fi
    ;;
  ro)
    # Read-only check: bypass menu and attempt to join a non-existent code.
    INPUT=$'\n'
    set +e
    OUTPUT="$(printf "%s" "$INPUT" | HOME="$TMP_HOME" run_with_timeout 45s ./binGO-CLI -mode client -server "$HOST" -code BINGO-ZZZZZ 2>&1)"
    EXIT_CODE=$?
    set -e

    echo "$OUTPUT"

    if [[ $EXIT_CODE -eq 0 ]]; then
      echo "CLI smoke ro failed: expected non-zero exit when joining invalid code"
      exit 1
    fi

    if ! grep -q "Connection failed:" <<<"$OUTPUT"; then
      echo "CLI smoke ro failed: missing connection failure output"
      exit 1
    fi

    if ! grep -q "invalid game code" <<<"$OUTPUT"; then
      echo "CLI smoke ro failed: expected invalid game code rejection"
      exit 1
    fi
    ;;
  *)
    echo "unsupported mode: $MODE (expected rw or ro)"
    exit 2
    ;;
esac

echo "CLI smoke mode=$MODE passed for $HOST"
