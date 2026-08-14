# CONFENGE VPS execution plane

Isolated always-on Warmbly deployment for CONFENGE on Netcup, co-tenant with
extra-cli but **not** sharing application state.

Full docs:

- [vps-execution-plane.md](../../docs/confenge/vps-execution-plane.md)
- [vps-execution-inventory.md](../../docs/confenge/vps-execution-inventory.md)
- [vps-disaster-recovery.md](../../docs/confenge/vps-disaster-recovery.md)

Quick start (on VPS checkout at `/opt/warmbly-confenge`):

```bash
deploy/confenge-vps/install.sh
deploy/confenge-vps/prove-hostinger-net.sh   # SMTP must pass (Netcup unlock if not)
deploy/confenge-vps/up.sh
# After Netcup unlocks outbound 465/587:
#   CONFENGE_SELF_SMOKE_TO=<you> deploy/confenge-vps/post-smtp-unlock.sh
deploy/confenge-vps/connect-hostinger.sh     # interactive password; seals in DB
deploy/confenge-vps/status.sh
```

Operator browser (from laptop):

```bash
deploy/confenge-vps/tunnel.sh
# http://127.0.0.1:5173
```

The page opens directly in PT-BR without interactive authentication. The backend
still issues an organization-scoped session to the configured technical operator.
Keep the UI and API on loopback and access them only through the SSH tunnel.
The helper keeps local port `8080` available for Evolution API and forwards the
CONFENGE backend to `18080`. Use `deploy/confenge-vps/compose.sh` for maintenance;
raw `docker compose` commands do not load this deployment's override and `.env`.

Safety: GREEN autorun OFF, auto-send OFF, WhatsApp OFF, human approval ON.
Operational pace 10→20/h, daily shell 200. Hostinger plan is **Business Email
Starter** (1000 msgs/rolling 24h/mailbox); that is only the provider ceiling,
not the commercial target (`HOSTINGER_PLAN_CLASS=BUSINESS_EMAIL_STARTER`).
