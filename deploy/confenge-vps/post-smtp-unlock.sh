#!/usr/bin/env bash
# After Netcup unlocks outbound SMTP 465/587, run this on the VPS.
# Order: network proof → sealed Hostinger connect → optional self-smoke.
# Never sends to feed leads. Self-smoke requires explicit CONFENGE_SELF_SMOKE_TO.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"

echo "== 1/3 Hostinger network premise =="
if ! "$ROOT/deploy/confenge-vps/prove-hostinger-net.sh"; then
  echo "BLOCKED: SMTP still unreachable from this VPS." >&2
  echo "In Netcup SCP → Firewall, DELETE the policy "netcup Mail block", then Save." >&2
  echo "Do not install Postfix/Exim/Mailcow." >&2
  exit 3
fi

echo "== 2/3 Connect Hostinger (sealed credentials) =="
echo "Will prompt for mailbox password with read -s (not logged)."
"$ROOT/deploy/confenge-vps/connect-hostinger.sh"

if [[ -z "${CONFENGE_SELF_SMOKE_TO:-}" ]]; then
  echo "== 3/3 Self-smoke SKIPPED =="
  echo "Set CONFENGE_SELF_SMOKE_TO to an operator-owned address and re-run:"
  echo "  CONFENGE_SELF_SMOKE_TO=you@example.com deploy/confenge-vps/self-smoke.sh"
  echo "POST_SMTP_UNLOCK=partial (connect done; smoke pending explicit destination)"
  exit 0
fi

echo "== 3/3 Self-smoke to ${CONFENGE_SELF_SMOKE_TO} (operator sink only) =="
"$ROOT/deploy/confenge-vps/self-smoke.sh"
echo "POST_SMTP_UNLOCK=ok"
echo "Next: confirm Sent + IMAP + Unibox; optional: sudo reboot && deploy/confenge-vps/status.sh"
