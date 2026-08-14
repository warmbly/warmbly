#!/usr/bin/env bash
# Provision Hostinger SMTP/IMAP mailbox into Warmbly on the VPS.
# Password is read with read -s (or CONFENGE_MAILBOX_PASSWORD env for CI only).
# Never put the password in argv, git, logs, or process list via `ps` args.
#
# After success, Warmbly seals credentials with CREDENTIALS_ENCRYPTION_KEY + DEK.
# Unset temporary password from the shell.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

API="$(api_base)"
BASE="${API}/v1"
EMAIL="${CONFENGE_MAILBOX_EMAIL:-tiago.sasaki@confenge.com.br}"
NAME="${CONFENGE_MAILBOX_NAME:-Tiago Sasaki}"
SMTP_HOST="${CONFENGE_SMTP_HOST:-smtp.hostinger.com}"
SMTP_PORT="${CONFENGE_SMTP_PORT:-587}"
IMAP_HOST="${CONFENGE_IMAP_HOST:-imap.hostinger.com}"
IMAP_PORT="${CONFENGE_IMAP_PORT:-993}"

PASS="${CONFENGE_MAILBOX_PASSWORD:-}"
if [[ -z "$PASS" ]]; then
  if [[ ! -t 0 ]]; then
    echo "BLOCKED: no TTY and CONFENGE_MAILBOX_PASSWORD unset. Use interactive shell." >&2
    exit 2
  fi
  printf 'Hostinger mailbox password for %s (input hidden): ' "$EMAIL" >&2
  # read -s: no echo; password not in argv
  read -r -s PASS
  echo >&2
fi
if [[ -z "$PASS" ]]; then
  echo "BLOCKED: empty password" >&2
  exit 2
fi

echo "API=$BASE email=$EMAIL smtp=${SMTP_HOST}:${SMTP_PORT} imap=${IMAP_HOST}:${IMAP_PORT}"
echo "(password not logged)"

if ! TOKEN="$(ops_access_token)"; then
  echo "BLOCKED: sessão técnica CONFENGE indisponível" >&2
  exit 2
fi
ORG="${CONFENGE_FEED_SYNC_ORG_ID:-22222222-0000-0000-0000-000000000001}"
curl -sS -X POST "$BASE/organization/switch/$ORG" -H "Authorization: Bearer $TOKEN" >/dev/null || true
AUTH="Authorization: Bearer $TOKEN"

# Build JSON on a 0600 temp file; POST via --data-binary @file so password never appears in curl argv/ps.
BODY_FILE="$(mktemp)"
chmod 600 "$BODY_FILE"
PASS="$PASS" EMAIL="$EMAIL" NAME="$NAME" SMTP_HOST="$SMTP_HOST" SMTP_PORT="$SMTP_PORT" \
  IMAP_HOST="$IMAP_HOST" IMAP_PORT="$IMAP_PORT" python3 - <<'PY' >"$BODY_FILE"
import json, os
print(json.dumps({
  "email": os.environ["EMAIL"],
  "name": os.environ["NAME"],
  "smtp": {
    "host": os.environ["SMTP_HOST"],
    "port": int(os.environ["SMTP_PORT"]),
    "username": os.environ["EMAIL"],
    "password": os.environ["PASS"],
  },
  "imap": {
    "host": os.environ["IMAP_HOST"],
    "port": int(os.environ["IMAP_PORT"]),
    "username": os.environ["EMAIL"],
    "password": os.environ["PASS"],
  },
}))
PY

# Drop password from shell env ASAP after JSON build
PASS=""
unset PASS
unset CONFENGE_MAILBOX_PASSWORD || true

echo "Connecting SMTP/IMAP (worker validates live credentials)..."
RESP="$(curl -sS -w '\n%{http_code}' -X POST "$BASE/emails/onboarding/smtp-imap" \
  -H "$AUTH" -H 'Content-Type: application/json' --data-binary @"$BODY_FILE")"
rm -f "$BODY_FILE"
CODE="$(printf '%s' "$RESP" | tail -1)"
BODY_OUT="$(printf '%s' "$RESP" | sed '$d')"
echo "http=$CODE"
# Redact any accidental password fields if API echoed them
printf '%s' "$BODY_OUT" | python3 -c '
import sys, json, re
raw = sys.stdin.read()
try:
    d = json.loads(raw)
except Exception:
    print(raw[:500])
    raise SystemExit
def scrub(o):
    if isinstance(o, dict):
        return {k: ("***" if "pass" in k.lower() or "secret" in k.lower() else scrub(v)) for k,v in o.items()}
    if isinstance(o, list):
        return [scrub(x) for x in o]
    return o
print(json.dumps(scrub(d), indent=2)[:2000])
' 2>/dev/null || printf '%s' "$BODY_OUT" | head -c 400

if [[ "$CODE" != "201" && "$CODE" != "200" && "$CODE" != "409" ]]; then
  echo "HOSTINGER_CONNECT=FAIL" >&2
  exit 1
fi

ACC_ID="$(printf '%s' "$BODY_OUT" | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin)
except Exception:
 print(""); raise SystemExit
print(d.get("id") or (d.get("data") or {}).get("id") or "")' 2>/dev/null || true)"

if [[ -z "$ACC_ID" ]]; then
  ACC_ID="$(curl -sS "$BASE/emails" -H "$AUTH" | EMAIL="$EMAIL" python3 -c '
import sys,json,os
want=os.environ.get("EMAIL","").lower()
d=json.load(sys.stdin)
rows=d if isinstance(d,list) else d.get("data") or []
for a in rows:
  if str(a.get("email","")).lower()==want:
    print(a.get("id","")); break
')"
fi

if [[ -n "$ACC_ID" ]]; then
  SIG_PLAIN=$'Atenciosamente,\n\nEng. Tiago Sasaki\nCONFENGE\ntiago.sasaki@confenge.com.br'
  SIG_HTML='<div style="margin-top:16px;font-family:Arial,Helvetica,sans-serif;font-size:13px;color:#1e293b"><p style="margin:0 0 8px 0">Atenciosamente,</p><p style="margin:0"><strong>Eng. Tiago Sasaki</strong><br>CONFENGE<br><a href="mailto:tiago.sasaki@confenge.com.br">tiago.sasaki@confenge.com.br</a></p><p style="margin:12px 0 0 0"><img src="cid:tiago-sasaki-signature@confenge" alt="Assinatura Tiago Sasaki" width="400" style="max-width:100%;height:auto;border:0" /></p></div>'
  PATCH="$(SIG_PLAIN="$SIG_PLAIN" SIG_HTML="$SIG_HTML" python3 -c "import json,os; print(json.dumps({'signature_plain':os.environ['SIG_PLAIN'],'signature_html':os.environ['SIG_HTML'],'signature_sync':True}))")"
  PRESP="$(curl -sS -w '\n%{http_code}' -X PATCH "$BASE/emails/$ACC_ID" \
    -H "$AUTH" -H 'Content-Type: application/json' -d "$PATCH")"
  PCODE="$(printf '%s' "$PRESP" | tail -1)"
  echo "signature_patch_http=$PCODE account=$ACC_ID"
fi

echo "HOSTINGER_SMTP_IMAP_CONNECTED=ok"
echo "Credentials are sealed in Warmbly DB. Plaintext password was not written to .env."
echo "Restart-safe: keep KMS_LOCAL_MASTER_KEY + CREDENTIALS_ENCRYPTION_KEY backed up."
