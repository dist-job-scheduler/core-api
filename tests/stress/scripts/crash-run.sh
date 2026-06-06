#!/usr/bin/env bash
# Crash-recovery chaos run: load items behind a stalling sink, SIGKILL the
# scheduler mid-flight, restart it, then verify the reaper rescues every item.
#
# Knobs: CRASH_ITEMS (default 10), CRASH_SLEEP_MS (default 8000),
#        KILL_AFTER_SEC (default 3).
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="docker compose -f docker-compose.stress.yml"
RUN="crash-$(date +%s)"
N="${CRASH_ITEMS:-10}"
SLEEP_MS="${CRASH_SLEEP_MS:-8000}"
KILL_AFTER="${KILL_AFTER_SEC:-3}"

echo "[crash] run=$RUN items=$N sleep_ms=$SLEEP_MS kill_after=${KILL_AFTER}s"

echo "[crash] phase 1: load (sink stalls so items sit in 'running')"
$COMPOSE run --rm \
  -e PHASE=load -e CRASH_RUN="$RUN" -e CRASH_ITEMS="$N" -e CRASH_SLEEP_MS="$SLEEP_MS" \
  k6 run /scripts/crash.js

echo "[crash] waiting ${KILL_AFTER}s for the drainer to claim the batch..."
sleep "$KILL_AFTER"

echo "[crash] >>> SIGKILL scheduler (simulated crash) <<<"
$COMPOSE kill scheduler

echo "[crash] restarting scheduler"
$COMPOSE up -d scheduler

echo "[crash] phase 3: verify recovery (reaper rescue can take ~30-60s)"
$COMPOSE run --rm \
  -e PHASE=verify -e CRASH_RUN="$RUN" -e CRASH_ITEMS="$N" \
  k6 run /scripts/crash.js
