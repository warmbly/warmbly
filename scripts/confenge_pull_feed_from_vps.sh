#!/usr/bin/env bash
# Pull latest confenge.outreach feed from VPS intelligence plane (no M365).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${ROOT}/data/confenge-plane"
mkdir -p "$DEST"
SSH_KEY="${CONFENGE_VPS_SSH_KEY:-$HOME/.ssh/extra-consultoria-prod}"
SSH_HOST="${CONFENGE_VPS_HOST:-root@159.195.18.88}"
SSH_PORT="${CONFENGE_VPS_PORT:-2222}"
scp -o BatchMode=yes -i "$SSH_KEY" -P "$SSH_PORT" \
  "${SSH_HOST}:/opt/confenge-plane/feed-www/email_send_ready_feed.json" \
  "${SSH_HOST}:/opt/confenge-plane/feed-www/manifest.json" \
  "${SSH_HOST}:/opt/confenge-plane/feed-www/supply-report.json" \
  "${SSH_HOST}:/opt/confenge-plane/tls/ca.crt" \
  "$DEST/"
echo "pulled feed → $DEST"
python3 -c "import json; r=json.load(open('${DEST}/supply-report.json')); print(r.get('status'), 'ready=', r.get('EMAIL_SEND_READY_companies'))"
