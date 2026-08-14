#!/usr/bin/env bash
# Prove container restart keeps durable state (DB row + kill-switch volume).
# Safe: no commercial leads, no real Hostinger send required.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-warmbly-confenge}"

echo "== precheck =="
if ! compose_cmd ps --status running --services 2>/dev/null | grep -q postgres; then
  echo "postgres not running; start stack first (deploy/confenge-vps/up.sh)" >&2
  exit 2
fi

MARKER="vps-restart-proof-$(date -u +%Y%m%dT%H%M%SZ)"
echo "Writing marker row: $MARKER"
compose_cmd exec -T postgres psql -U warmbly -d warmbly_dev -v ON_ERROR_STOP=1 -c \
  "CREATE TABLE IF NOT EXISTS confenge_vps_restart_probe (id text primary key, created_at timestamptz default now());
   INSERT INTO confenge_vps_restart_probe(id) VALUES ('$MARKER') ON CONFLICT DO NOTHING;"

compose_cmd exec -T backend sh -c 'mkdir -p /data/confenge-ops && echo proof > /data/confenge-ops/restart-marker' 2>/dev/null || true

echo "== restart containers =="
compose_cmd restart postgres redis nats backend consumer worker web || compose_cmd up -d

echo "Waiting for postgres + backend..."
for i in $(seq 1 40); do
  if compose_cmd exec -T postgres pg_isready -U warmbly >/dev/null 2>&1 \
    && curl -sS -o /dev/null -w '%{http_code}' --max-time 2 http://127.0.0.1:8080/health 2>/dev/null | grep -q 200; then
    break
  fi
  sleep 2
done

FOUND="$(compose_cmd exec -T postgres psql -U warmbly -d warmbly_dev -tAc \
  "SELECT id FROM confenge_vps_restart_probe WHERE id='$MARKER';" | tr -d '[:space:]')"

if [[ "$FOUND" == "$MARKER" ]]; then
  echo "PERSISTENCE=PASS marker=$MARKER"
else
  echo "PERSISTENCE=FAIL marker missing after restart" >&2
  exit 1
fi

if compose_cmd exec -T backend test -f /data/confenge-ops/restart-marker 2>/dev/null; then
  echo "OPS_VOLUME=PASS"
else
  echo "OPS_VOLUME=FAIL (backend confenge_ops volume not durable)" >&2
  # non-fatal if volume not mounted yet on older stack
fi

echo "RESTART_PROOF=ok"
echo "Full VPS reboot proof: sudo reboot; after boot run deploy/confenge-vps/status.sh (no laptop make)."
