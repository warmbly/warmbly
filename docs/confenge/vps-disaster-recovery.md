# CONFENGE VPS disaster recovery

## What to back up

| Asset | Where | Notes |
| --- | --- | --- |
| Postgres | `deploy/confenge-vps/backup.sh` → `warmbly_dev.sql` | Approvals, queues, sealed creds, outcomes |
| Encryption roots | secrets bundle `keys-*.env` (0600) | `KMS_LOCAL_MASTER_KEY`, `CREDENTIALS_ENCRYPTION_KEY`, `AUTH_SECRET`, tokens |
| Redacted env | `env.redacted` inside archive | Non-secret config snapshot |
| confenge_ops volume | included indirectly via kill-switch host mirror | Prefer DB governor state |

**Do not back up:** plaintext Hostinger password, extra-cli datalake, unlimited logs, `node_modules`.

**Do not store** SQL dump and key bundle in the same public object store without encryption. Prefer offline or encrypted vault for keys; SQL can live in a separate private store.

## Backup

```bash
# on VPS, repo at /opt/warmbly-confenge
deploy/confenge-vps/backup.sh
# → data/backups/confenge-vps/warmbly-confenge-<ts>.tar.gz
# → data/backups/confenge-vps/secrets/keys-<ts>.env
```

Schedule (example): daily cron under root, after business window.

## Restore

1. Install Docker stack (`install.sh`, `gen-secrets.sh` **or** restore keys from secrets bundle into `deploy/confenge-vps/.env` mode 0600).
2. `deploy/confenge-vps/up.sh` until postgres healthy.
3. `deploy/confenge-vps/restore.sh /path/to/warmbly_dev.sql` (destructive to current DB).
4. Restart backend/worker: `docker compose ... restart backend worker`.
5. `deploy/confenge-vps/status.sh`
6. Confirm mailbox still decrypts (list accounts / trigger IMAP). Do **not** re-enter password unless keys were wrong era.

## Controlled restore proof

```bash
# With stack up and non-production data:
deploy/confenge-vps/backup.sh
# Note archive path and secrets path (do not commit)
# Insert probe: deploy/confenge-vps/prove-restart.sh already writes a probe table
deploy/confenge-vps/restore.sh data/backups/confenge-vps/.../warmbly_dev.sql
# Expect probe row or known approval still present
```

If restore cannot be run on the live VPS safely, run against a disposable compose project name and document the result; live VPS restore remains operator-scheduled.

## After VPS reboot

Docker + `restart: unless-stopped` bring the stack back. No laptop `make`. Verify:

```bash
deploy/confenge-vps/status.sh
```

## Emergency stop

```bash
deploy/confenge-vps/pause.sh "incident"
# or from browser: dispatch pause when available
```

## Network / provider incidents

| Symptom | Action |
| --- | --- |
| HOSTINGER SMTP FAIL | Confirm Netcup outbound 465/587; do not install MTA |
| HOSTINGER IMAP FAIL | Check DNS/firewall; worker logs |
| EXTRA FEED FAIL/STALE | Check `/opt/confenge-plane` + extra-cli feed generation |
| OUTCOME LOOP FAIL | Check receptor systemd unit + nginx 8443 proxy |

## Parallel hardening

This deployment pack must not patch `green_autorun`, policy auth, touchpoint SM, or concurrent migrations. Authorization defects → `CROSS_PR_BLOCKER` for the hardening PR.
