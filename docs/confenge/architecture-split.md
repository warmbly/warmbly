# CONFENGE architecture split (canonical)

Warmbly is the **execution plane**. extra-cli is the **intelligence plane**.
They may share a physical VPS but **never** share application databases.

```text
┌──────────────────────────── VPS (Netcup) ────────────────────────────┐
│  extra-cli + datalake                                                 │
│  enrichment / activation / EMAIL_SEND_READY supply                    │
│  confenge.outreach.v1 feed                                            │
│  HTTPS :8443  → static feed + /webhooks/warmbly/outcome (HMAC)        │
│  NO Warmbly tables · NO mailbox passwords in extra-cli                │
│                                                                        │
│  Warmbly project warmbly-confenge (Docker, isolated volumes)          │
│  generation / human approval state / content_hash / cadence           │
│  governor · Hostinger SMTP/IMAP client · reply/bounce/DNC · CRM       │
│  confenge.outcome.v1 → loopback/HTTPS → outcome receptor              │
└──────────────────────────────────────────────────────────────────────┘
          │
          │ browser (SSH tunnel to loopback UI) when human is online
          ▼
┌──────────────── Operator laptop ─────────────────────────────────────┐
│  Review exact message · Approve / Edit / Reject / DNC · pause/resume │
│  No requirement for local Docker / WSL / worker for scheduled sends  │
└──────────────────────────────────────────────────────────────────────┘
```

Historical note: go-live docs briefly kept Warmbly on the laptop only. The
always-on execution plane moves Warmbly to the VPS; the laptop remains the
human review surface. See [vps-execution-plane.md](./vps-execution-plane.md).

## Email channel (factual)

| Item | Value |
| --- | --- |
| Address | `tiago.sasaki@confenge.com.br` |
| Host | **Hostinger** (not Exchange Online / M365) |
| SMTP | `smtp.hostinger.com:587` STARTTLS (465 also supported by Warmbly) |
| IMAP | `imap.hostinger.com:993` SSL |
| Graph / Azure app | **Not required** |
| Mailpit | Tests / system OTP only |
| Plan class | Hostinger **Business Email Starter** (`HOSTINGER_PLAN_CLASS=BUSINESS_EMAIL_STARTER`) |
| Provider ceiling | **1000** msgs / rolling 24h / mailbox (hPanel SoT; not cPanel) |
| Operational pace | adaptive **10→20/h**, daily shell **200** (reputation; not provider max) |

## Operator loop (minimal)

1. VPS: extra-cli refreshes feed; Warmbly feed sync imports on a timer.
2. Human (any browser via SSH tunnel): open `/app/confenge`, approve exact content.
3. VPS worker: when due, governor + Hostinger SMTP; IMAP for replies 24/7.
4. Outcomes → confenge-plane HMAC webhook on the same VPS.

## Public exposure

| Exposure | Required? |
| --- | --- |
| VPS :8443 feed + outcome | Yes (intelligence plane; already live) |
| VPS Warmbly UI public | **No** (loopback + SSH tunnel preferred) |
| Postgres/Redis/NATS public | **Forbidden** |
| Mailbox password in git/env long-term | **Forbidden** (sealed in Warmbly DB) |

## Kill switch

```bash
deploy/confenge-vps/pause.sh
deploy/confenge-vps/resume.sh
# or UI dispatch pause when authenticated
```
