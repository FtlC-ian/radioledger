#!/bin/sh
# RadioLedger API — container entrypoint
# Runs SQL migrations then starts the API server.
set -e

echo "[entrypoint] Starting RadioLedger API..."

# ── Safety checks ──────────────────────────────────────────────────────────────
if [ -z "${RADIOLEDGER_MASTER_KEY:-}" ]; then
    echo "[entrypoint] ERROR: RADIOLEDGER_MASTER_KEY is not set." >&2
    echo "[entrypoint] Run ./init.sh in the docker/ directory to generate secrets." >&2
    exit 1
fi

if [ -z "${DATABASE_URL:-}" ]; then
    echo "[entrypoint] ERROR: DATABASE_URL is not set." >&2
    exit 1
fi

# ── SQL migrations (goose) ─────────────────────────────────────────────────────
echo "[entrypoint] Running SQL migrations..."
for attempt in $(seq 1 30); do
    if psql "${DATABASE_URL}" -v ON_ERROR_STOP=1 -f /app/bootstrap_legacy_goose.sql \
        && goose -dir /app/migrations postgres "${DATABASE_URL}" up; then
        echo "[entrypoint] SQL migrations complete."
        break
    fi
    if [ "$attempt" -eq 30 ]; then
        echo "[entrypoint] ERROR: SQL migrations failed after ${attempt} attempts." >&2
        exit 1
    fi
    echo "[entrypoint] Database not ready or migrations failed; retrying (${attempt}/30)..." >&2
    sleep 2
done

# ── River queue migrations ─────────────────────────────────────────────────────
echo "[entrypoint] Running River queue migrations..."
migrate-river
echo "[entrypoint] River migrations complete."

# ── Start server ───────────────────────────────────────────────────────────────
echo "[entrypoint] Starting API server on :${PORT:-8080}..."
exec radioledger-api "$@"
