#!/usr/bin/env bash
# Start or update the isolated warmbly-confenge stack (always-on).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

ENVF="${CONFENGE_VPS_ENV:-$ROOT/deploy/confenge-vps/.env}"
if [[ ! -f "$ENVF" ]]; then
  echo "Missing $ENVF — run deploy/confenge-vps/gen-secrets.sh first" >&2
  exit 2
fi
chmod 600 "$ENVF" || true

export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-warmbly-confenge}"

# Fail closed on unsafe profile flips
# shellcheck disable=SC1090
set -a; . "$ENVF"; set +a
if [[ "${CONFENGE_GREEN_AUTORUN_ENABLED:-false}" == "true" ]]; then
  echo "REFUSE: CONFENGE_GREEN_AUTORUN_ENABLED=true is not allowed on VPS execution plane bootstrap" >&2
  exit 3
fi
if [[ "${CONFENGE_AUTO_SEND_ENABLED:-false}" == "true" ]]; then
  echo "REFUSE: CONFENGE_AUTO_SEND_ENABLED=true is not allowed (human approval path only)" >&2
  exit 3
fi
if [[ "${CONFENGE_REQUIRE_HUMAN_APPROVAL:-true}" != "true" ]]; then
  echo "REFUSE: CONFENGE_REQUIRE_HUMAN_APPROVAL must stay true on the VPS execution plane" >&2
  exit 3
fi
if [[ "${CONFENGE_WHATSAPP_ENABLED:-false}" == "true" ]]; then
  echo "REFUSE: WhatsApp must stay OFF in this PR profile" >&2
  exit 3
fi
if [[ "${CONFENGE_OPERATOR_MODE:-true}" == "true" ]]; then
  if [[ -z "${CONFENGE_OPERATOR_USER_ID:-}" || -z "${CONFENGE_OPERATOR_ORG_ID:-}" ]]; then
    echo "REFUSE: operator mode requires CONFENGE_OPERATOR_USER_ID and CONFENGE_OPERATOR_ORG_ID" >&2
    exit 3
  fi
  case "${API_PUBLIC_URL:-}" in
    http://127.0.0.1:*|http://localhost:*) ;;
    *) echo "REFUSE: operator mode requires a loopback API_PUBLIC_URL" >&2; exit 3 ;;
  esac
  case "${APP_URL:-}" in
    http://127.0.0.1:*|http://localhost:*) ;;
    *) echo "REFUSE: operator mode requires a loopback APP_URL" >&2; exit 3 ;;
  esac
fi

# Every deploy and first boot starts paused. This is written before any app or
# worker container starts, so a new/empty Docker volume cannot fail open.
OPS_VOLUME="${COMPOSE_PROJECT_NAME:-warmbly-confenge}_confenge_ops"
docker volume create "$OPS_VOLUME" >/dev/null
docker run --rm -v "$OPS_VOLUME:/data" alpine \
  sh -c 'printf "paused\nreason=deploy_preflight\n" > /data/kill-switch && chown 1000:1000 /data/kill-switch && chmod 600 /data/kill-switch' >/dev/null

if [[ "${CONFENGE_VPS_SEED:-false}" == "true" ]]; then
  echo "Preparing first boot with operator mode temporarily disabled..."
  CONFENGE_OPERATOR_MODE=false compose_cmd up -d postgres redis nats mailpit backend
  for i in $(seq 1 60); do
    if curl -sS -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:8080/health" 2>/dev/null | grep -q 200; then
      break
    fi
    sleep 2
    if [[ "$i" -eq 60 ]]; then
      echo "backend not healthy for first-boot seed" >&2
      exit 1
    fi
  done
  CONFENGE_OPERATOR_MODE=false compose_cmd --profile seed run --rm --no-deps seed
fi

echo "Bringing up project=$COMPOSE_PROJECT_NAME ..."
compose_cmd up -d --remove-orphans


# Keep the kill-switch volume private and writable by the backend user (uid 1000).
docker run --rm -v "${COMPOSE_PROJECT_NAME:-warmbly-confenge}_confenge_ops:/data" alpine \
  sh -c "chown 1000:1000 /data && chmod 700 /data" >/dev/null 2>&1 || true

echo "Waiting for backend health..."
for i in $(seq 1 60); do
  if curl -sS -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:8080/health" 2>/dev/null | grep -q 200; then
    echo "backend healthy"
    break
  fi
  sleep 2
  if [[ "$i" -eq 60 ]]; then
    echo "backend not healthy after wait" >&2
    compose_cmd ps
    exit 1
  fi
done

echo "Stack up. Operator UI via SSH tunnel (see docs/confenge/vps-execution-plane.md):"
if [[ "${CONFENGE_OPERATOR_MODE:-true}" == "true" ]]; then
  WEB_CONFIG="$(curl -fsS --max-time 5 "http://127.0.0.1:5173/config.js")"
  if ! grep -Fq 'CONFENGE_OPERATOR_MODE: "true"' <<<"$WEB_CONFIG"; then
    echo "REFUSE: web runtime config did not preserve CONFENGE_OPERATOR_MODE=true" >&2
    exit 1
  fi
  if ! grep -Fq "API_URL: \"${API_PUBLIC_URL}\"" <<<"$WEB_CONFIG"; then
    echo "REFUSE: web runtime API_URL does not match API_PUBLIC_URL=${API_PUBLIC_URL}" >&2
    exit 1
  fi
  echo "operator runtime config verified"
fi
echo "  deploy/confenge-vps/tunnel.sh"
echo "  open http://127.0.0.1:5173"
echo "Connect Hostinger: deploy/confenge-vps/connect-hostinger.sh"
echo "Status: deploy/confenge-vps/status.sh"
