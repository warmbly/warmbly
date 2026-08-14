#!/usr/bin/env bash
# Connect Hostinger SMTP/IMAP mailbox to local Warmbly (no Graph/M365).
# Requires: local stack up (backend on CONFENGE_API_HOST), worker, seed user.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
if [ -f .env.confenge ]; then set -a; # shellcheck disable=SC1091
  . ./.env.confenge
  set +a
fi

API="${CONFENGE_API_BASE:-http://${CONFENGE_API_HOST:-127.0.0.1:18080}}"
API="${API%/}"
# strip trailing path if host:port form became http://host:port
case "$API" in
  http://*|https://*) ;;
  *) API="http://$API" ;;
esac
# confenge uses /v1 not /api/v1
BASE="${API}/v1"

EMAIL="${CONFENGE_MAILBOX_EMAIL:-tiago.sasaki@confenge.com.br}"
NAME="${CONFENGE_MAILBOX_NAME:-Tiago Sasaki}"
SMTP_HOST="${CONFENGE_SMTP_HOST:-smtp.hostinger.com}"
SMTP_PORT="${CONFENGE_SMTP_PORT:-587}"
IMAP_HOST="${CONFENGE_IMAP_HOST:-imap.hostinger.com}"
IMAP_PORT="${CONFENGE_IMAP_PORT:-993}"
PASS="${CONFENGE_MAILBOX_PASSWORD:-}"

if [ -z "$PASS" ]; then
  echo "BLOCKED: set CONFENGE_MAILBOX_PASSWORD in .env.confenge (Hostinger mailbox password)."
  echo "Mailbox: $EMAIL  SMTP ${SMTP_HOST}:${SMTP_PORT}  IMAP ${IMAP_HOST}:${IMAP_PORT}"
  exit 2
fi

echo "API=$BASE email=$EMAIL smtp=${SMTP_HOST}:${SMTP_PORT} imap=${IMAP_HOST}:${IMAP_PORT}"

# Login + OTP via Mailpit when enabled
LOGIN=$(curl -sS -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"dev@warmbly.com","password":"password123"}')
SESSION=$(echo "$LOGIN" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("session",""))')
if [ -z "$SESSION" ]; then
  echo "login failed: $LOGIN"
  exit 1
fi
sleep 1
OTP=$(python3 - <<'PY'
import json,re,urllib.request
try:
  msgs=json.load(urllib.request.urlopen('http://127.0.0.1:18025/api/v1/messages?limit=8'))
except Exception as e:
  print(''); raise SystemExit
for m in msgs.get('messages') or []:
  d=json.load(urllib.request.urlopen('http://127.0.0.1:18025/api/v1/message/'+m['ID']))
  codes=re.findall(r'\b(\d{6})\b', (d.get('Text') or '')+(d.get('HTML') or ''))
  if codes:
    print(codes[0]); break
PY
)
if [ -z "$OTP" ]; then
  echo "No OTP from Mailpit — is stack using login OTP? Trying confirm without..."
fi
if [ -n "$OTP" ]; then
  CONFIRM=$(curl -sS -X POST "$BASE/auth/login/confirm" -H 'Content-Type: application/json' \
    -d "{\"email\":\"dev@warmbly.com\",\"code\":\"$OTP\",\"session\":\"$SESSION\"}")
  TOKEN=$(echo "$CONFIRM" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))')
else
  TOKEN=$(echo "$LOGIN" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))')
fi
if [ -z "$TOKEN" ]; then
  echo "auth failed"
  exit 1
fi
ORG="${CONFENGE_FEED_SYNC_ORG_ID:-22222222-0000-0000-0000-000000000001}"
curl -sS -X POST "$BASE/organization/switch/$ORG" -H "Authorization: Bearer $TOKEN" >/dev/null || true
AUTH="Authorization: Bearer $TOKEN"

export EMAIL NAME SMTP_HOST SMTP_PORT IMAP_HOST IMAP_PORT PASS
BODY=$(EMAIL="$EMAIL" NAME="$NAME" SMTP_HOST="$SMTP_HOST" SMTP_PORT="$SMTP_PORT" \
  IMAP_HOST="$IMAP_HOST" IMAP_PORT="$IMAP_PORT" PASS="$PASS" python3 - <<'PY'
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
)

echo "Connecting SMTP/IMAP (worker validates live credentials)..."
RESP=$(curl -sS -w '\n%{http_code}' -X POST "$BASE/emails/onboarding/smtp-imap" \
  -H "$AUTH" -H 'Content-Type: application/json' -d "$BODY")
CODE=$(echo "$RESP" | tail -1)
BODY_OUT=$(echo "$RESP" | sed '$d')
echo "http=$CODE"
echo "$BODY_OUT" | python3 -m json.tool 2>/dev/null | head -40 || echo "$BODY_OUT" | head -c 500
if [ "$CODE" != "201" ] && [ "$CODE" != "200" ] && [ "$CODE" != "409" ]; then
  exit 1
fi
# Resolve account id (new connect or already connected)
ACC_ID=$(echo "$BODY_OUT" | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin)
except Exception:
 print(""); raise SystemExit
print(d.get("id") or (d.get("data") or {}).get("id") or "")' 2>/dev/null || true)
if [ -z "$ACC_ID" ]; then
  ACC_ID=$(curl -sS "$BASE/emails" -H "$AUTH" | python3 -c "
import sys,json,os
want=os.environ.get('EMAIL','').lower()
d=json.load(sys.stdin)
rows=d if isinstance(d,list) else d.get('data') or []
for a in rows:
  if str(a.get('email','')).lower()==want:
    print(a.get('id','')); break
")
fi
if [ -n "$ACC_ID" ]; then
  # Signature is applied by confenge.BodyToHTML at enroll: "Atenciosamente," + CID image only.
  # Do not store Best Regards / name / company / email text on the mailbox (image carries identity).
  # signature_sync=false so campaign send does not double-append after BodyToHTML.
  PATCH='{"signature_plain":"","signature_html":"","signature_sync":false}'
  PRESP=$(curl -sS -w '\n%{http_code}' -X PATCH "$BASE/emails/$ACC_ID" \
    -H "$AUTH" -H 'Content-Type: application/json' -d "$PATCH")
  PCODE=$(echo "$PRESP" | tail -1)
  echo "signature_patch_http=$PCODE account=$ACC_ID (Atenciosamente+image via BodyToHTML; mailbox sig cleared)"
fi
echo "HOSTINGER_SMTP_IMAP_CONNECTED=ok"
echo "Set CONFENGE_SIGNATURE_IMAGE_PATH=data/confenge/tiago-sasaki-assinatura.jpeg for worker SMTP CID attach."
