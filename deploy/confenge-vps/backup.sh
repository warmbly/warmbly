#!/usr/bin/env bash
# Backup Warmbly CONFENGE operational data (DB + key material pointers).
# Never includes Hostinger plaintext password. Never prints secret values.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${CONFENGE_BACKUP_DIR:-$ROOT/data/backups/confenge-vps}"
mkdir -p "$OUT_DIR"
STAGE="$OUT_DIR/stage-$TS"
mkdir -p "$STAGE"

echo "Backing up postgres (warmbly_dev)..."
compose_cmd exec -T postgres pg_dump -U warmbly warmbly_dev >"$STAGE/warmbly_dev.sql"

# Encryption keys: copy env keys into a separate 0600 secrets bundle (not next to public dumps by default)
SECRETS_DIR="${CONFENGE_SECRETS_BACKUP_DIR:-$OUT_DIR/secrets}"
mkdir -p "$SECRETS_DIR"
SEC_BUNDLE="$SECRETS_DIR/keys-$TS.env"
umask 077
{
  echo "# CONFENGE VPS key material backup $TS"
  echo "# Store offline / encrypted separately from SQL dumps."
  for k in KMS_LOCAL_MASTER_KEY CREDENTIALS_ENCRYPTION_KEY AUTH_SECRET INTERNAL_API_TOKEN CONFENGE_OUTCOME_WEBHOOK_SECRET; do
    v="${!k:-}"
    if [[ -n "$v" ]]; then
      printf '%s=%s\n' "$k" "$v"
    fi
  done
} >"$SEC_BUNDLE"
chmod 600 "$SEC_BUNDLE"

# Config snapshot without secrets
python3 - "$ROOT/deploy/confenge-vps/.env" "$STAGE/env.redacted" <<'PY'
import re, sys
src, dst = sys.argv[1], sys.argv[2]
secret_keys = re.compile(
    r"(PASSWORD|SECRET|TOKEN|KEY|AUTH_SECRET|CREDENTIALS|KMS_|WEBHOOK_SECRET)",
    re.I,
)
try:
    lines = open(src, encoding="utf-8").read().splitlines()
except FileNotFoundError:
    open(dst, "w", encoding="utf-8").write("# no .env present\n")
    raise SystemExit(0)
out = []
for line in lines:
    if not line.strip() or line.strip().startswith("#"):
        out.append(line)
        continue
    if "=" not in line:
        out.append(line)
        continue
    k, _, _ = line.partition("=")
    if secret_keys.search(k):
        out.append(f"{k}=***REDACTED***")
    else:
        out.append(line)
open(dst, "w", encoding="utf-8").write("\n".join(out) + "\n")
PY

# Manifest
{
  echo "ts=$TS"
  echo "project=${COMPOSE_PROJECT_NAME:-warmbly-confenge}"
  echo "git_sha=$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
  echo "host=$(hostname 2>/dev/null || echo unknown)"
  echo "sql=warmbly_dev.sql"
  echo "secrets_bundle=$(basename "$SEC_BUNDLE")"
  echo "note=Keep SQL and secrets_bundle offline and separate; both needed for full restore."
} >"$STAGE/MANIFEST.txt"

ARCHIVE="$OUT_DIR/warmbly-confenge-$TS.tar.gz"
tar -C "$STAGE" -czf "$ARCHIVE" .
rm -rf "$STAGE"
chmod 600 "$ARCHIVE" 2>/dev/null || true

echo "Backup archive: $ARCHIVE"
echo "Secrets bundle: $SEC_BUNDLE (0600; store separately)"
echo "Excluded: node_modules, build cache, unlimited logs, extra-cli datalake, plaintext mailbox password"
