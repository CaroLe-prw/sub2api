#!/bin/sh
set -eu

MODE="${1:-schedule}"
if [ "$#" -gt 0 ]; then
  shift
fi

export STATE_FILE="${STATE_FILE:-/data/subscription-daily-quota-reset-state.json}"
export RUN_EVERY_SECONDS="${RUN_EVERY_SECONDS:-300}"

ensure_psql() {
  if [ "${PATCH_DAILY_WINDOW_START:-0}" = "1" ] && ! command -v psql >/dev/null 2>&1; then
    echo "[quota-reset-docker] psql is required when PATCH_DAILY_WINDOW_START=1; rebuild the Docker image"
    exit 1
  fi
}

case "$MODE" in
  schedule)
    ensure_psql
    echo "[quota-reset-docker] schedule mode: check every ${RUN_EVERY_SECONDS}s, state=${STATE_FILE}"
    while true; do
      node /app/reset-subscription-daily-quota-from-announcement.mjs || true
      sleep "$RUN_EVERY_SECONDS"
    done
    ;;
  once)
    ensure_psql
    exec node /app/reset-subscription-daily-quota-from-announcement.mjs "$@"
    ;;
  manual)
    ensure_psql
    exec node /app/manual-reset-subscription-quota-from-announcement.mjs "$@"
    ;;
  *)
    exec "$MODE" "$@"
    ;;
esac
