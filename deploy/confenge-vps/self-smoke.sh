#!/usr/bin/env bash
# Controlled Hostinger self-smoke. Requires EXPLICIT destination (never a feed lead).
# Usage:
#   CONFENGE_SELF_SMOKE_TO=you@example.com deploy/confenge-vps/self-smoke.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

TO="${CONFENGE_SELF_SMOKE_TO:-}"
if [[ -z "$TO" ]]; then
  echo "BLOCKED: set CONFENGE_SELF_SMOKE_TO to an operator-owned address." >&2
  echo "Never defaults to a feed lead. Example: CONFENGE_SELF_SMOKE_TO=tiago.sasaki@confenge.com.br" >&2
  exit 2
fi

# Reuse repo script semantics with VPS API host
export CONFENGE_API_HOST="${CONFENGE_API_HOST:-127.0.0.1:8080}"
export CONFENGE_SELF_SMOKE_TO="$TO"
if [[ -x "$ROOT/scripts/confenge_self_smoke.sh" ]]; then
  exec "$ROOT/scripts/confenge_self_smoke.sh"
fi
echo "missing scripts/confenge_self_smoke.sh" >&2
exit 1
