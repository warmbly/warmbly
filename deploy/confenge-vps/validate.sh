#!/usr/bin/env bash
# Offline validation of the VPS deployment pack (no secrets, no lead mail).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
PACK=deploy/confenge-vps
fail=0

echo "== required files =="
for f in \
  docker-compose.override.yml env.example lib.sh compose.sh tunnel.sh \
  gen-secrets.sh connect-hostinger.sh status.sh pause.sh resume.sh \
  backup.sh restore.sh up.sh down.sh install.sh \
  prove-hostinger-net.sh prove-restart.sh self-smoke.sh post-smtp-unlock.sh validate.sh
do
  if [[ -f "$PACK/$f" ]]; then
    echo "OK $f"
  else
    echo "MISSING $f"
    fail=1
  fi
done

if grep -qF 'reason=deploy_preflight' "$PACK/up.sh" &&
   grep -qF 'chmod 600 /data/kill-switch' "$PACK/up.sh"; then
  echo "OK deploy engages private kill switch before startup"
else
  echo "FAIL deploy does not engage kill switch before startup"
  fail=1
fi

echo "== bash -n =="
for s in "$PACK"/*.sh; do
  if bash -n "$s"; then
    echo "syntax OK $(basename "$s")"
  else
    echo "syntax FAIL $(basename "$s")"
    fail=1
  fi
done

if command -v shellcheck >/dev/null 2>&1; then
  echo "== shellcheck =="
  shellcheck -x "$PACK"/*.sh || fail=1
else
  echo "== shellcheck SKIP (not installed; bash -n used) =="
fi

echo "== safety flags in env.example =="
for pair in \
  "CONFENGE_GREEN_AUTORUN_ENABLED=false" \
  "CONFENGE_AUTO_SEND_ENABLED=false" \
  "CONFENGE_REQUIRE_HUMAN_APPROVAL=true" \
  "CONFENGE_OUTREACH_ENABLED=true" \
  "CONFENGE_OPERATOR_MODE=true" \
  "CONFENGE_OPERATOR_USER_ID=11111111-0000-0000-0000-000000000001" \
  "CONFENGE_OPERATOR_ORG_ID=22222222-0000-0000-0000-000000000001" \
  "CONFENGE_WHATSAPP_ENABLED=false" \
  "CONFENGE_RATE_MAX_PER_HOUR=20" \
  "HOSTINGER_PLAN_CLASS=BUSINESS_EMAIL_STARTER" \
  "CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=200"
do
  if grep -qF "$pair" "$PACK/env.example"; then
    echo "OK $pair"
  else
    echo "MISSING $pair"
    fail=1
  fi
done

# Must not set RATE_MAX above 20
if grep -E 'CONFENGE_RATE_MAX_PER_HOUR=[3-9][0-9]' "$PACK/env.example" "$PACK/docker-compose.override.yml" 2>/dev/null; then
  echo "FAIL RATE_MAX above 20 in pack"
  fail=1
else
  echo "OK RATE_MAX not raised above 20"
fi

# No permanent mailbox password assignment (allow empty template / read -s / unset only)
echo "== secret scan (pack tracked files) =="
# Match only non-empty assignments outside comments and the scanner itself.
hits="$(
  grep -RInE '^\s*CONFENGE_MAILBOX_PASSWORD=[^#[:space:]]+' "$PACK" \
    --include='*.example' --include='*.yml' --include='*.sh' --include='*.md' --include='env.example' 2>/dev/null \
    | grep -v 'validate\.sh' \
    | grep -v 'CONFENGE_MAILBOX_PASSWORD=\${' \
    || true
)"
if [[ -n "$hits" ]]; then
  echo "$hits"
  echo "FAIL possible committed mailbox password assignment"
  fail=1
else
  echo "OK no permanent mailbox password assignment in pack"
fi

echo "== docker compose config =="
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  TMPENV="$(mktemp)"
  cp "$PACK/env.example" "$TMPENV"
  # fill dummy secrets so compose expands
  {
    echo "KMS_LOCAL_MASTER_KEY=dGVzdC1rbXMta2V5LTMyYnl0ZXMhMTIzNDU2Nzg="
    echo "CREDENTIALS_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    echo "AUTH_SECRET=local-dev-auth-secret-minimum-32-characters-long"
    echo "INTERNAL_API_TOKEN=local-dev-internal-token"
    echo "CONFENGE_OUTCOME_WEBHOOK_SECRET=test-outcome-secret-not-real"
  } >>"$TMPENV"
  COMPOSE_OUT="$(mktemp)"
  COMPOSE_LOG="$(mktemp)"
  if COMPOSE_PROJECT_NAME=warmbly-confenge docker compose \
    -f docker-compose.yml \
    -f "$PACK/docker-compose.override.yml" \
    --env-file "$TMPENV" \
    config >"$COMPOSE_OUT" 2>"$COMPOSE_LOG"; then
    echo "OK docker compose config"
    # Assert loopback binds for sensitive ports
    if grep -E 'published: (15432|16379|4222|8080|5173)' "$COMPOSE_OUT" | head -20; then
      :
    fi
    if grep -q '0.0.0.0:15432' "$COMPOSE_OUT" 2>/dev/null; then
      echo "FAIL postgres published on all interfaces"
      fail=1
    else
      echo "OK postgres not on 0.0.0.0"
    fi
    for port in 8080 5173; do
      if grep -B3 -A1 "published: \"${port}\"" "$COMPOSE_OUT" | grep -q 'host_ip: 127.0.0.1'; then
        echo "OK operator surface port $port is loopback-only"
      else
        echo "FAIL operator surface port $port is not explicitly loopback-only"
        fail=1
      fi
    done
    if grep -q 'WARMBLY_CONFENGE_OPERATOR_MODE: "true"' "$COMPOSE_OUT"; then
      echo "OK web operator mode survives compose rendering"
    else
      echo "FAIL web operator mode missing from compose rendering"
      fail=1
    fi
    if grep -q 'WARMBLY_API_URL: http://127.0.0.1:18080' "$COMPOSE_OUT"; then
      echo "OK web API uses the persistent laptop tunnel port 18080"
    else
      echo "FAIL web API is not configured for laptop tunnel port 18080"
      fail=1
    fi
    if grep -q 'CONFENGE_GREEN_AUTORUN_ENABLED: "false"\|CONFENGE_GREEN_AUTORUN_ENABLED: false' "$COMPOSE_OUT" \
      || grep -q 'CONFENGE_GREEN_AUTORUN_ENABLED: \${CONFENGE_GREEN_AUTORUN_ENABLED:-false}' "$COMPOSE_OUT"; then
      echo "OK green autorun default false in compose model"
    fi
  else
    echo "FAIL docker compose config (see compose-config.log)"
    tail -40 "$COMPOSE_LOG"
    fail=1
  fi
  rm -f "$TMPENV" "$COMPOSE_OUT" "$COMPOSE_LOG"
else
  echo "SKIP docker compose (docker not available)"
fi

echo "== no MTA install steps =="
if grep -RInE 'apt(-get)? install.*(postfix|exim|mailcow|mailu|sendmail)' "$PACK" docs/confenge/vps-*.md 2>/dev/null; then
  echo "FAIL MTA install referenced"
  fail=1
else
  echo "OK no MTA install"
fi

if [[ "$fail" -ne 0 ]]; then
  echo "VALIDATE=FAIL"
  exit 1
fi
echo "VALIDATE=PASS"
