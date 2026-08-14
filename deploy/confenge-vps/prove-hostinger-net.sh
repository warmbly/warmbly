#!/usr/bin/env bash
# Network premise check: VPS → Hostinger SMTP/IMAP (no auth, no password).
set -euo pipefail
SMTP_HOST="${CONFENGE_SMTP_HOST:-smtp.hostinger.com}"
IMAP_HOST="${CONFENGE_IMAP_HOST:-imap.hostinger.com}"

echo "DNS $SMTP_HOST:"
getent ahostsv4 "$SMTP_HOST" | head -3 || true
getent ahostsv6 "$SMTP_HOST" | head -2 || true
echo "DNS $IMAP_HOST:"
getent ahostsv4 "$IMAP_HOST" | head -3 || true

check_tcp() {
  local host="$1" port="$2"
  if timeout 8 bash -c "echo >/dev/tcp/${host}/${port}" 2>/dev/null; then
    echo "TCP ${host}:${port} OPEN"
    return 0
  fi
  echo "TCP ${host}:${port} FAIL"
  return 1
}

smtp_ok=0
imap_ok=0
check_tcp "$SMTP_HOST" 465 && smtp_ok=1 || true
check_tcp "$SMTP_HOST" 587 && smtp_ok=1 || true
check_tcp "$IMAP_HOST" 993 && imap_ok=1 || true

echo "TLS probe IMAP 993 (cert subject only):"
timeout 10 openssl s_client -connect "${IMAP_HOST}:993" -servername "$IMAP_HOST" </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -dates 2>/dev/null || echo "openssl imap failed"

if [[ "$smtp_ok" -eq 1 ]]; then
  echo "HOSTINGER_SMTP=PASS"
else
  echo "HOSTINGER_SMTP=FAIL"
  echo "If both 465/587 fail while local machine succeeds: likely VPS provider outbound SMTP block."
  echo "Request Netcup unlock for outbound TCP 465/587. Do not install a local MTA."
fi
if [[ "$imap_ok" -eq 1 ]]; then
  echo "HOSTINGER_IMAP=PASS"
else
  echo "HOSTINGER_IMAP=FAIL"
fi

# Exit non-zero if either fails so CI/status can gate
if [[ "$smtp_ok" -eq 1 && "$imap_ok" -eq 1 ]]; then
  exit 0
fi
exit 1
