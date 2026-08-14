# Operator: restore Netcup VPS SSH (required for GO)

Host: 159.195.18.88 (v2202607385716487230)
Symptom: TCP 2222 and 22 Connection refused. Host ping OK. :8443 feed/outcome OK.

## Why this blocks Monday
Without SSH you cannot: re-bind `.deployed_sha`, run `status.sh`, self-smoke, IMAP reply proof, pause/resume, draft review via loopback API.

## Recovery (Netcup SCP VNC)

1. Open https://www.customercontrolpanel.de/
2. Open product **RS 2000** → **Server Control Panel (SCP)**
3. Open **VNC / Console**
4. Login as root (key console or SCP root password reset if needed)
5. Diagnose sshd:
   ```bash
   systemctl status ssh || systemctl status sshd
   journalctl -u ssh -u sshd -n 80 --no-pager
   ss -lntp | grep -E '2222|22'
   ```
6. If sshd dead / misconfigured:
   ```bash
   # Ensure Port 2222 still intended
   grep -n '^Port' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/* 2>/dev/null
   systemctl restart ssh || systemctl restart sshd
   systemctl enable ssh || systemctl enable sshd
   ss -lntp | grep 2222
   ```
7. From laptop:
   ```bash
   ssh ec-prod 'hostname; systemctl is-active ssh || systemctl is-active sshd'
   ```
8. Re-bind go-live evidence (must re-run, not reuse stale logs):
   ```bash
   cd /opt/warmbly-confenge
   git fetch origin main && git checkout --detach origin/main
   # restore deploy/confenge-vps/.env from backup if needed
   echo "$(git rev-parse HEAD)" > .deployed_sha
   bash deploy/confenge-vps/status.sh
   docker exec warmbly-confenge-backend-1 cat /data/confenge-ops/kill-switch
   CONFENGE_SELF_SMOKE_TO=<your-operator@email> bash deploy/confenge-vps/self-smoke.sh
   # Then reply from a second operator client; confirm Unibox IMAP + touchpoint cancel
   ```
9. Only after SHA MATCH + SMTP + IMAP reply-stop + status PASS → re-emit GO card.

## Do NOT
- Reinstall OS (destroys postgres volumes)
- Open 8080/5173/15432 publicly
- Enable GREEN autorun or WhatsApp to "test"
