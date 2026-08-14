# Netcup ops notes (CONFENGE)

Canonical topology: the VPS runs **extra-cli (intelligence)** and **Warmbly
`warmbly-confenge` (execution)**. They share the host, not the database.

See [architecture-split.md](./architecture-split.md) and
[vps-execution-plane.md](./vps-execution-plane.md).

## On the VPS

| Plane | Path / project | Role |
| --- | --- | --- |
| Intelligence | `/opt/confenge-plane`, extra-cli under `/opt/extra-consultoria` | Feed :8443, outcome HMAC, datalake |
| Execution | `/opt/warmbly-confenge` project `warmbly-confenge` | Approve state, governor, Hostinger client, CRM |

Deploy pack in repo: `deploy/confenge-vps/`.

## Integration path

```text
extra-cli → HTTPS feed :8443
Warmbly feed sync (host-gateway) → confenge.outreach.v1
Warmbly outcome outbox → CONFENGE_OUTCOME_WEBHOOK_URL (HMAC retained)
```

## Operator laptop

```text
ssh tunnel → http://127.0.0.1:5173
review / approve / pause
```

No Hostinger password on the laptop after VPS connect. No local worker required
for due sends after approval.

## Safety

```env
CONFENGE_GREEN_AUTORUN_ENABLED=false
CONFENGE_AUTO_SEND_ENABLED=false
CONFENGE_REQUIRE_HUMAN_APPROVAL=true
CONFENGE_WHATSAPP_ENABLED=false
HOSTINGER_PLAN_CLASS=BUSINESS_EMAIL_STARTER
# provider ceiling 1000/rolling 24h/mailbox; ops pace stays 10→20/h, daily 200
CONFENGE_RATE_MAX_PER_HOUR=20
CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=200
```

## Public exposure

| Open on VPS | Purpose |
| --- | --- |
| TCP 8443 | Feed + outcome webhook (existing plane) |
| TCP 2222 | SSH ops + operator tunnel |
| Warmbly 8080/5173 | **Loopback only** |

## Hostinger egress

Prove with `deploy/confenge-vps/prove-hostinger-net.sh`. If SMTP 465/587 fail from
the VPS while IMAP works, request Netcup outbound SMTP unlock. Do not install
an MTA on the VPS.

## Proof bar

1. Stack recovers after reboot without laptop
2. Feed + outcome contracts only (no SQL coupling)
3. Hostinger IMAP 24/7; SMTP after egress unlock
4. Human approval persists; kill switch works
5. No lead sends in validation; GREEN off
