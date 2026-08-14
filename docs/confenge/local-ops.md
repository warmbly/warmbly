# CONFENGE local-first ops (WSL)

One-command local stack for operators. No Kafka, AWS, GCP, or Stripe required.

## Prerequisites

- Docker
- Go 1.25+
- pnpm

## Quick path

```bash
cp .env.confenge.example .env.confenge
# edit only if you need AI, real feed URL, or WhatsApp mock
make confenge-local
```

Abra `http://localhost:5173`. O modo operador cria uma sessão técnica
automaticamente e leva direto à Central comercial, sem tela de login ou onboarding.

As ações continuam vinculadas a `CONFENGE_OPERATOR_USER_ID` dentro da organização
`CONFENGE_OPERATOR_ORG_ID`. A configuração local usa os IDs das fixtures de seed.

```bash
make confenge-preflight
make confenge-import FEED=internal/app/confenge/testdata/demo_3_companies.json DRY_RUN=true
make confenge-import FEED=internal/app/confenge/testdata/demo_3_companies.json
```

In the dashboard: generate draft → approve (human) → enroll. Mailpit captures SMTP
at `http://localhost:18025`. Nothing real is sent.

## Make targets

| Target | Purpose |
| --- | --- |
| `make confenge-local` | Infra + migrate + seed + backend/consumer/worker/web with CONFENGE on |
| `make confenge-preflight` | DB, Redis, NATS, flags, AI, mailbox, feed, outcome, governor, WA, kill switch |
| `make confenge-bootstrap` | Workspace settings without raw SQL |
| `make confenge-import FEED=... [DRY_RUN=true]` | Import feed; prints creates/updates/blocked |
| `go run ./cmd/confenge reconcile-target-fit [--dry-run] --org-id UUID` | Reconcile historical target-fit and revoke ineligible unsent work |
| `make confenge-stop-sending` | Operational kill switch (enroll/send refuse) |
| `make confenge-resume-sending` | Clear kill-switch file |
| `make confenge-db-backup` | `pg_dump` into `data/backups/` |
| `make confenge-db-restore FILE=...` | Restore dump |

## Safety invariants

- `CONFENGE_AUTO_SEND_ENABLED` must stay `false`
- `CONFENGE_REQUIRE_HUMAN_APPROVAL=true` (fail-closed)
- AI never approves or sends
- `DO_NOT_CONTACT` / opt-out / bounce / reply block cadences
- Reimport does not re-activate DNC accounts
- Current extra-cli target-fit is mandatory through the final send gate
- Missing, stale, downgraded, or out-of-scope target-fit fails closed even with a valid email
- Public phones without opt-in stay blocked for WhatsApp API outbound
- No Baileys / WhatsApp Web in production (`WHATSAPP_EVOLUTION_ALLOW_BAILEYS=false`)
- Missing WhatsApp credentials do not break email
- Default API bind is `127.0.0.1:8080`
- Secrets stay in `.env.confenge` (gitignored)

## Email

- **Tests:** seed SMTP mailboxes → Mailpit (`SMTP` port 11025, UI 18025)
- **CONFENGE production mailbox:** Hostinger SMTP/IMAP for
  `tiago.sasaki@confenge.com.br` (not Microsoft 365 / Graph)
  - set `CONFENGE_MAILBOX_*` / `CONFENGE_SMTP_*` / `CONFENGE_IMAP_*` in `.env.confenge`
  - `scripts/confenge_hostinger_connect.sh` then `scripts/confenge_self_smoke.sh`
  - self-smoke only to an address you control (`CONFENGE_SELF_SMOKE_TO`); never leads
- Optional Outlook OAuth exists in the product for other tenants; it is **not**
  required for CONFENGE go-live

## WhatsApp

- Default off for local ops
- Offline mock: `CONFENGE_WHATSAPP_ENABLED=true` and `WHATSAPP_PROVIDER=mock`
- Production path: Evolution Cloud API (official-compatible), not Baileys

## CLI

```bash
go run ./cmd/confenge preflight
go run ./cmd/confenge bootstrap
go run ./cmd/confenge import --feed internal/app/confenge/testdata/demo_3_companies.json --dry-run
go run ./cmd/confenge reconcile-target-fit --dry-run --org-id <uuid>
go run ./cmd/confenge reconcile-target-fit --org-id <uuid>
go run ./cmd/confenge stop-sending
go run ./cmd/confenge resume-sending
```

## Readiness panel

`GET /api/v1/confenge/status` includes a `readiness` object:

- `email` ready / not_ready
- `whatsapp` ready / not_ready / blocked_by_policy / not_configured
- `feed_age`
- `outcome_loop`
- `ai` ready / fallback_template
- `governor_cap` (hourly global cap, default 10/h)
- `campaign_daily_limit` (campaign shell ceiling, default 100)
- `queue_count`
- `kill_switch` / `sending_allowed`

The dashboard CONFENGE page shows a discrete readiness card with these fields.

## Offline demo fixture

`internal/app/confenge/testdata/demo_3_companies.json` — three companies (two with
official contacts, one NEEDS_CONTACT). Covered by `go test ./internal/app/confenge/ -run Local|Demo|Kill|Preflight|Readiness`.
