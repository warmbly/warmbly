#!/usr/bin/env bash
# Bootstrap warmbly-confenge on the Netcup VPS without touching extra-cli state.
# Safe to re-run. Does not send email. Does not reboot.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
cd "$ROOT"

TARGET="${CONFENGE_VPS_INSTALL_DIR:-/opt/warmbly-confenge}"
echo "Repo root: $ROOT"
echo "Target dir: $TARGET (convention; skip copy if already there)"

if [[ "$(readlink -f "$ROOT")" != "$(readlink -f "$TARGET" 2>/dev/null || true)" ]]; then
  if [[ -d "$TARGET/.git" || -f "$TARGET/docker-compose.yml" ]]; then
    echo "Using existing checkout at $TARGET"
  else
    echo "NOTE: deploy from a git checkout at $TARGET or set ROOT there."
    echo "This script configures the current repo tree only."
  fi
fi

command -v docker >/dev/null || { echo "docker required"; exit 1; }
docker compose version >/dev/null || { echo "docker compose v2 required"; exit 1; }

# Secrets
"$ROOT/deploy/confenge-vps/gen-secrets.sh"

ENVF="$ROOT/deploy/confenge-vps/.env"
chmod 600 "$ENVF"

# Ensure docker restart on boot
if command -v systemctl >/dev/null; then
  systemctl enable docker >/dev/null 2>&1 || true
fi

# UFW: do not open Warmbly ports publicly. Only document.
if command -v ufw >/dev/null 2>&1; then
  echo "UFW present. Warmbly binds loopback only; do not allow 8080/5173/15432/16379/4222 publicly."
  echo "Keep 2222 (SSH) and existing 8443 (confenge-plane feed/outcome) as-is."
fi

# Resource snapshot
{
  echo "=== install inventory $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
  hostname
  nproc
  free -h
  df -h / | tail -1
  docker --version
  docker compose version
} | tee "$ROOT/deploy/confenge-vps/.last-install-inventory.txt" >/dev/null || true

echo "Install prep done."
echo "Next:"
echo "  1) Confirm HOSTINGER_PLAN_CLASS=BUSINESS_EMAIL_STARTER (1000/24h; not cPanel)"
echo "  2) deploy/confenge-vps/prove-hostinger-net.sh  # SMTP must OPEN (Netcup unlock if FAIL)"
echo "  3) deploy/confenge-vps/up.sh"
echo "  4) deploy/confenge-vps/connect-hostinger.sh"
echo "  5) CONFENGE_SELF_SMOKE_TO=<you> deploy/confenge-vps/self-smoke.sh"
echo "Never: install MTA, open Postgres, enable GREEN autorun, send to feed leads."
