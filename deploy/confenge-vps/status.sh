#!/usr/bin/env bash
# Compact execution-plane health for CONFENGE on VPS. Never prints secrets.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

DEPLOYED_SHA="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
DEPLOYED_DIRTY="$(git -C "$ROOT" status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
echo "DEPLOYED_SHA=$DEPLOYED_SHA"
echo "DEPLOYED_DIRTY=$DEPLOYED_DIRTY"

API="$(api_base)"
SMTP_HOST="${CONFENGE_SMTP_HOST:-smtp.hostinger.com}"
SMTP_PORT="${CONFENGE_SMTP_PORT:-587}"
IMAP_HOST="${CONFENGE_IMAP_HOST:-imap.hostinger.com}"
IMAP_PORT="${CONFENGE_IMAP_PORT:-993}"

svc_running() {
  local name="$1"
  compose_cmd ps --status running --services 2>/dev/null | grep -qx "$name"
}

http_ok() {
  local url="$1"
  curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$url" 2>/dev/null | grep -qE '^(200|204)$'
}

# BACKEND
if svc_running backend && http_ok "${API}/health"; then
  pass_fail BACKEND PASS
else
  pass_fail BACKEND FAIL
fi

# WORKER / CONSUMER
if svc_running worker; then pass_fail WORKER PASS; else pass_fail WORKER FAIL; fi
if svc_running consumer; then pass_fail CONSUMER PASS; else pass_fail CONSUMER FAIL; fi

# Infra
if svc_running postgres && compose_cmd exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; then
  pass_fail POSTGRES PASS
else
  pass_fail POSTGRES FAIL
fi
if svc_running redis && compose_cmd exec -T redis redis-cli ping 2>/dev/null | grep -qi pong; then
  pass_fail REDIS PASS
else
  pass_fail REDIS FAIL
fi
if svc_running nats; then pass_fail NATS PASS; else pass_fail NATS FAIL; fi

# Hostinger reachability (no auth)
if tcp_check "$SMTP_HOST" "$SMTP_PORT"; then
  pass_fail "HOSTINGER SMTP" PASS
else
  pass_fail "HOSTINGER SMTP" FAIL
fi
if tcp_check "$IMAP_HOST" "$IMAP_PORT"; then
  pass_fail "HOSTINGER IMAP" PASS
else
  pass_fail "HOSTINGER IMAP" FAIL
fi

# Feed freshness uses the same durable timestamp and max age as the dashboard.
FEED_STATE=FAIL
FEED_TIMESTAMP=""
FEED_SNAPSHOT=""
if TOKEN="$(ops_access_token 2>/dev/null)"; then
  STATUS_JSON="$(curl -sS --max-time 5 "${API}/v1/confenge/status" -H "Authorization: Bearer $TOKEN" 2>/dev/null || true)"
  FEED_API_STATE="$(printf '%s' "$STATUS_JSON" | grep -o '"feed_state":"[^"]*"' | head -1 | cut -d'"' -f4)"
  FEED_TIMESTAMP="$(printf '%s' "$STATUS_JSON" | grep -o '"feed_last_success_at":"[^"]*"' | head -1 | cut -d'"' -f4)"
  FEED_SNAPSHOT="$(printf '%s' "$STATUS_JSON" | grep -o '"feed_snapshot_hash":"[^"]*"' | head -1 | cut -d'"' -f4)"
  case "$FEED_API_STATE" in
    fresh) FEED_STATE=PASS ;;
    stale) FEED_STATE=STALE ;;
    missing) FEED_STATE=FAIL ;;
  esac
  TOKEN=""
  unset TOKEN
fi
pass_fail "EXTRA FEED" "$FEED_STATE"
echo "EXTRA_FEED_TIMESTAMP=${FEED_TIMESTAMP:-unknown}"
echo "EXTRA_FEED_SNAPSHOT=${FEED_SNAPSHOT:-unknown}"

# Outcome loop: receptor on host loopback 8790 via nginx 8443
if curl -sk --max-time 5 -o /dev/null -w '%{http_code}' -X POST "https://127.0.0.1:8443/webhooks/warmbly/outcome" \
  -H 'Content-Type: application/json' -d '{}' 2>/dev/null | grep -qE '^(200|400|401|403|404|405|422)$'; then
  pass_fail "OUTCOME LOOP" PASS
else
  pass_fail "OUTCOME LOOP" FAIL
fi

# Dispatch / kill switch
DISPATCH=ACTIVE
if [[ -f "${CONFENGE_KILL_SWITCH_HOST_PATH:-}" ]]; then
  DISPATCH=PAUSED
fi
# Docker volume kill-switch probe
if compose_cmd exec -T backend test -f /data/confenge-ops/kill-switch 2>/dev/null; then
  DISPATCH=PAUSED
fi
if [[ "${CONFENGE_SENDING_PAUSED:-false}" == "true" ]]; then
  DISPATCH=PAUSED
fi
pass_fail DISPATCH "$DISPATCH"

# Safety flags (profile must keep these OFF)
GREEN="${CONFENGE_GREEN_AUTORUN_ENABLED:-false}"
if [[ "$GREEN" == "true" ]]; then
  pass_fail "GREEN AUTORUN" FAIL
else
  pass_fail "GREEN AUTORUN" OFF
fi
WA="${CONFENGE_WHATSAPP_ENABLED:-false}"
if [[ "$WA" == "true" ]]; then
  pass_fail WHATSAPP FAIL
else
  pass_fail WHATSAPP OFF
fi

echo "HOSTINGER_PLAN_CLASS=${HOSTINGER_PLAN_CLASS:-unset}"
echo "RATE_MAX_PER_HOUR=${CONFENGE_RATE_MAX_PER_HOUR:-20} (operational; not Hostinger technical ceiling)"
