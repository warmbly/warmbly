#!/usr/bin/env bash
# Emergency kill switch: pause CONFENGE outbound on the VPS.
# Works without extra-cli / AI. Prefer durable governor API; always engage file switch.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

REASON="${1:-operator_ssh_pause}"
if [[ ! "$REASON" =~ ^[A-Za-z0-9_.:-]{1,100}$ ]]; then
  echo "REFUSE: pause reason must use 1-100 safe identifier characters" >&2
  exit 3
fi

# 1) File kill-switch on the shared ops volume. If the backend is unavailable,
# write the same volume directly. Never report PAUSED without verifying the file.
OPS_VOLUME="${COMPOSE_PROJECT_NAME:-warmbly-confenge}_confenge_ops"
if ! compose_cmd exec -T backend sh -c \
  'mkdir -p /data/confenge-ops && printf "paused\nreason=%s\n" "$1" > /data/confenge-ops/kill-switch && chmod 600 /data/confenge-ops/kill-switch' sh "$REASON" \
  2>/dev/null; then
  docker run --rm -v "$OPS_VOLUME:/data" alpine sh -c \
    'printf "paused\nreason=%s\n" "$1" > /data/kill-switch && chmod 600 /data/kill-switch' sh "$REASON" >/dev/null
fi
if ! docker run --rm -v "$OPS_VOLUME:/data:ro" alpine test -f /data/kill-switch; then
  echo "BLOCKED: transport kill switch could not be verified" >&2
  exit 1
fi

# Also write host-side mirror for offline inspection
HOST_KS="${CONFENGE_KILL_SWITCH_HOST_PATH:-$ROOT/data/confenge-kill-switch}"
mkdir -p "$(dirname "$HOST_KS")"
printf 'paused\nreason=%s\nat=%s\n' "$REASON" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$HOST_KS"
chmod 644 "$HOST_KS"

# 2) Governor API pause when the loopback operator session is available.
if TOKEN="$(ops_access_token 2>/dev/null)"; then
  API="$(api_base)"
  ORG="${CONFENGE_FEED_SYNC_ORG_ID:-22222222-0000-0000-0000-000000000001}"
  curl -sS -X POST "$API/v1/organization/switch/$ORG" -H "Authorization: Bearer $TOKEN" >/dev/null || true
  CODE="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$API/v1/confenge/dispatch/pause" \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d "{\"reason\":\"$REASON\"}" || true)"
  echo "governor_pause_http=$CODE"
else
  echo "governor_pause=skipped (auth failed; file kill-switch engaged)"
fi

echo "DISPATCH=PAUSED reason=$REASON"
echo "Resume with: deploy/confenge-vps/resume.sh"
