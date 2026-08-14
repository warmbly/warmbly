#!/usr/bin/env bash
# SELF_SMOKE: controlled operator-to-operator send via Hostinger (never leads).
# Prerequisites: stack up, Hostinger mailbox connected, worker running.
#
# CONFENGE_SELF_SMOKE_TO is REQUIRED — no fallback to mailbox or any commercial contact.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
if [ -f .env.confenge ]; then set -a; # shellcheck disable=SC1091
  . ./.env.confenge
  set +a
fi

echo "SELF_SMOKE=start"

API_HOST="${CONFENGE_API_HOST:-127.0.0.1:18080}"
BASE="http://${API_HOST}/v1"
MAILBOX="${CONFENGE_MAILBOX_EMAIL:-tiago.sasaki@confenge.com.br}"
TO="${CONFENGE_SELF_SMOKE_TO:-}"
if [ -z "$TO" ]; then
  echo "SELF_SMOKE_FAIL=missing_CONFENGE_SELF_SMOKE_TO"
  echo "Set CONFENGE_SELF_SMOKE_TO to an operator-controlled address. Never defaults to leads."
  exit 2
fi
if [[ "$TO" != *"@"* ]]; then
  echo "SELF_SMOKE_FAIL=invalid_destination to=$TO"
  exit 2
fi
SUBJECT="CONFENGE SELF_SMOKE $(date -u +%Y%m%dT%H%M%SZ)"
BODY_TEXT="CONFENGE SELF_SMOKE controlled self-send. Not a commercial lead. Reply if you receive this for IMAP ingress check."
echo "SELF_SMOKE_DESTINATION=$TO"
echo "SELF_SMOKE_MAILBOX=$MAILBOX"
echo "SELF_SMOKE_SUBJECT=$SUBJECT"

# Auth
LOGIN=$(curl -sS -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"dev@warmbly.com","password":"password123"}')
SESSION=$(echo "$LOGIN" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("session",""))')
sleep 1
OTP=$(python3 - <<'PY'
import json,re,urllib.request
try:
  msgs=json.load(urllib.request.urlopen('http://127.0.0.1:18025/api/v1/messages?limit=8'))
except Exception:
  print(''); raise SystemExit
for m in msgs.get('messages') or []:
  d=json.load(urllib.request.urlopen('http://127.0.0.1:18025/api/v1/message/'+m['ID']))
  codes=re.findall(r'\b(\d{6})\b', (d.get('Text') or '')+(d.get('HTML') or ''))
  if codes:
    print(codes[0]); break
PY
)
if [ -n "$OTP" ]; then
  TOKEN=$(curl -sS -X POST "$BASE/auth/login/confirm" -H 'Content-Type: application/json' \
    -d "{\"email\":\"dev@warmbly.com\",\"code\":\"$OTP\",\"session\":\"$SESSION\"}" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))')
else
  TOKEN=$(echo "$LOGIN" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))')
fi
ORG="${CONFENGE_FEED_SYNC_ORG_ID:-22222222-0000-0000-0000-000000000001}"
curl -sS -X POST "$BASE/organization/switch/$ORG" -H "Authorization: Bearer $TOKEN" >/dev/null || true
AUTH="Authorization: Bearer $TOKEN"

# Resolve Hostinger account id
export MAILBOX TO SUBJECT BODY_TEXT
ACC=$(curl -sS "$BASE/emails" -H "$AUTH" | python3 -c "
import sys,json,os
want=os.environ.get('MAILBOX','').lower()
d=json.load(sys.stdin)
rows=d if isinstance(d,list) else d.get('data') or d.get('emails') or []
for a in rows:
  if str(a.get('email','')).lower()==want:
    print(a.get('id','')); break
")
if [ -z "$ACC" ]; then
  echo "No connected mailbox for $MAILBOX — run scripts/confenge_hostinger_connect.sh first"
  exit 2
fi
echo "SELF_SMOKE account_id=$ACC to=$TO subject=$SUBJECT"
echo "SELF_SMOKE_PRE_SEND_CONFIRM destination=$TO (operator-controlled only)"

# Send from account (instant) — never selects a lead contact.
PAYLOAD=$(python3 -c "import json,os; print(json.dumps({'to':[os.environ['TO']],'subject':os.environ['SUBJECT'],'body_plain':os.environ['BODY_TEXT'],'body_html':'','send_mode':'instant'}))")
SEND=$(curl -sS -w '\n%{http_code}' -X POST "$BASE/emails/$ACC/send" \
  -H "$AUTH" -H 'Content-Type: application/json' -d "$PAYLOAD")
SCODE=$(echo "$SEND" | tail -1)
echo "send_http=$SCODE"
echo "$SEND" | sed '$d' | head -c 800; echo
if [ "$SCODE" != "200" ] && [ "$SCODE" != "201" ] && [ "$SCODE" != "202" ]; then
  echo "SELF_SMOKE_SEND=FAIL"
  exit 1
fi
echo "SELF_SMOKE_SEND=ok task queued task_id=$(echo "$SEND" | sed '$d' | python3 -c 'import sys,json; print(json.load(sys.stdin).get("task_id",""))' 2>/dev/null || true)"
echo "SELF_SMOKE=ok Provider: Hostinger SMTP/IMAP (not M365/Graph). Mailpit is tests-only."
echo "Next: confirm delivery in Hostinger Sent + operator inbox for subject: $SUBJECT"
echo "Then reply from another client and confirm Unibox IMAP sync; bounce only via controlled sink."
echo "SELF_SMOKE: no commercial leads contacted."