#!/usr/bin/env bash
# Generate crypto secrets into deploy/confenge-vps/.env (mode 0600).
# Does not print secret values. Does not touch Hostinger mailbox password.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ENVF="${CONFENGE_VPS_ENV:-$ROOT/deploy/confenge-vps/.env}"
EXAMPLE="$ROOT/deploy/confenge-vps/env.example"

if [[ ! -f "$ENVF" ]]; then
  cp "$EXAMPLE" "$ENVF"
  echo "Created $ENVF from env.example"
fi
chmod 600 "$ENVF"

need_fill() {
  local key="$1"
  local val
  val="$(grep -E "^${key}=" "$ENVF" | head -1 | cut -d= -f2- || true)"
  [[ -z "$val" ]]
}

set_key() {
  local key="$1" val="$2"
  if grep -qE "^${key}=" "$ENVF"; then
    # rewrite line in place without echoing value
    python3 - "$ENVF" "$key" "$val" <<'PY'
import sys
path, key, val = sys.argv[1], sys.argv[2], sys.argv[3]
lines = open(path, encoding="utf-8").read().splitlines()
out = []
found = False
for line in lines:
    if line.startswith(key + "="):
        out.append(f"{key}={val}")
        found = True
    else:
        out.append(line)
if not found:
    out.append(f"{key}={val}")
open(path, "w", encoding="utf-8").write("\n".join(out) + "\n")
PY
  else
    printf '%s=%s\n' "$key" "$val" >>"$ENVF"
  fi
}

if need_fill KMS_LOCAL_MASTER_KEY; then
  set_key KMS_LOCAL_MASTER_KEY "$(openssl rand -base64 32)"
  echo "filled KMS_LOCAL_MASTER_KEY"
else
  echo "keep KMS_LOCAL_MASTER_KEY"
fi
if need_fill CREDENTIALS_ENCRYPTION_KEY; then
  set_key CREDENTIALS_ENCRYPTION_KEY "$(openssl rand -hex 32)"
  echo "filled CREDENTIALS_ENCRYPTION_KEY"
else
  echo "keep CREDENTIALS_ENCRYPTION_KEY"
fi
if need_fill AUTH_SECRET; then
  set_key AUTH_SECRET "$(openssl rand -hex 32)"
  echo "filled AUTH_SECRET"
else
  echo "keep AUTH_SECRET"
fi
if need_fill INTERNAL_API_TOKEN; then
  set_key INTERNAL_API_TOKEN "$(openssl rand -hex 24)"
  echo "filled INTERNAL_API_TOKEN"
else
  echo "keep INTERNAL_API_TOKEN"
fi

# Outcome secret from confenge-plane if present and empty here
if need_fill CONFENGE_OUTCOME_WEBHOOK_SECRET && [[ -r /opt/confenge-plane/outcome.secret ]]; then
  set_key CONFENGE_OUTCOME_WEBHOOK_SECRET "$(tr -d '\n' </opt/confenge-plane/outcome.secret)"
  echo "filled CONFENGE_OUTCOME_WEBHOOK_SECRET from confenge-plane"
fi

chmod 600 "$ENVF"
echo "Secrets file ready: $ENVF (mode 0600). Back up offline separately from DB dumps."
echo "NEVER commit this file. NEVER print its contents into tickets."
