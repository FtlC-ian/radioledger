#!/usr/bin/env bash
# RadioLedger — First-run initialization script
#
# Generates .env with cryptographically random secrets if it doesn't exist.
# Safe to re-run — will not overwrite an existing .env.
#
# Usage:
#   cd docker
#   ./init.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"
EXAMPLE_FILE="$SCRIPT_DIR/.env.example"

# ── Color helpers ──────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

info()    { echo -e "${CYAN}[init]${NC} $*"; }
success() { echo -e "${GREEN}[init]${NC} $*"; }
warn()    { echo -e "${YELLOW}[init]${NC} $*"; }
error()   { echo -e "${RED}[init] ERROR:${NC} $*" >&2; }

# ── Check dependencies ─────────────────────────────────────────────────────────
if ! command -v openssl &>/dev/null; then
    error "openssl is required but not found. Install it and try again."
    exit 1
fi

if ! command -v docker &>/dev/null; then
    warn "docker not found in PATH. You will need Docker to run the stack."
fi

# ── Already initialized? ───────────────────────────────────────────────────────
if [[ -f "$ENV_FILE" ]]; then
    warn ".env already exists at $ENV_FILE"
    warn "Delete it and re-run init.sh to generate new secrets."
    warn "(This will invalidate all stored encrypted credentials!)"
    echo ""
    info "To start the stack:  docker compose build api lotw-vault web docs && docker compose up -d --no-build"
    info "Visit:               http://localhost:3000"
    exit 0
fi

# ── Generate secrets ───────────────────────────────────────────────────────────
info "Generating cryptographically random secrets..."

# 32-byte random password encoded as hex (64 hex chars)
POSTGRES_PASSWORD=$(openssl rand -hex 32)

# 32-byte master encryption key for AES-256-GCM, base64-encoded
RADIOLEDGER_MASTER_KEY=$(openssl rand -base64 32)

# Grafana admin password for optional monitoring profile
GRAFANA_ADMIN_PASSWORD=$(openssl rand -hex 24)

info "Secrets generated."

# ── Write .env ─────────────────────────────────────────────────────────────────
cp "$EXAMPLE_FILE" "$ENV_FILE"

# Replace placeholders with generated values (cross-platform sed)
if [[ "$(uname -s)" == "Darwin" ]]; then
    sed -i '' "s|REPLACE_WITH_GENERATED_PASSWORD|${POSTGRES_PASSWORD}|g" "$ENV_FILE"
    sed -i '' "s|REPLACE_WITH_GENERATED_KEY|${RADIOLEDGER_MASTER_KEY}|g"  "$ENV_FILE"
    sed -i '' "s|REPLACE_WITH_GENERATED_GRAFANA_PASSWORD|${GRAFANA_ADMIN_PASSWORD}|g" "$ENV_FILE"
else
    sed -i "s|REPLACE_WITH_GENERATED_PASSWORD|${POSTGRES_PASSWORD}|g" "$ENV_FILE"
    sed -i "s|REPLACE_WITH_GENERATED_KEY|${RADIOLEDGER_MASTER_KEY}|g"  "$ENV_FILE"
    sed -i "s|REPLACE_WITH_GENERATED_GRAFANA_PASSWORD|${GRAFANA_ADMIN_PASSWORD}|g" "$ENV_FILE"
fi

success ".env created at $ENV_FILE"
echo ""
echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BOLD}  RadioLedger — Ready to launch!${NC}"
echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${YELLOW}IMPORTANT:${NC} Back up your master encryption key separately from"
echo -e "  your database. If you lose it, encrypted credentials cannot be"
echo -e "  recovered. The key is stored in: $ENV_FILE"
echo ""
echo -e "  Next steps:"
echo -e "    ${CYAN}docker compose build api lotw-vault web docs${NC}  # build local images"
echo -e "    ${CYAN}docker compose up -d --no-build${NC}              # start the stack"
echo -e "    ${CYAN}docker compose logs -f api${NC}    # watch API startup + migrations"
echo ""
echo -e "  ${GREEN}Visit: http://localhost:3000${NC}"
echo ""
