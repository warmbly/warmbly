# CONFENGE outreach on Warmbly

Warmbly is the **execution plane** for CONFENGE commercial outreach. The
`extra-cli` remains the **intelligence plane** (datalake, signals, evidence,
scoring, Decision & Outcome Memory).

**Email channel (factual):** production mailbox
`tiago.sasaki@confenge.com.br` is **Hostinger** SMTP/IMAP on the operator
WSL laptop (local-first). Not Microsoft 365 / Graph. No Azure app registration
for go-live. Mailpit is for local tests only. See
[architecture-split.md](./architecture-split.md).

```text
datalake (VPS) → extra-cli → versioned feed → Warmbly staging → review → campaign → outcomes → extra-cli
```

Product acceptance matrix (email + WhatsApp sum, human approval, governor,
outcomes): [PRODUCT-ACCEPTANCE.md](./PRODUCT-ACCEPTANCE.md).

This tree also covers feed contract, import, staging models, review, WhatsApp
orchestration, per-touch approval, dispatch governor, reply cockpit, and
local-first ops.

## Separation of concerns

| Warmbly | extra-cli |
| --- | --- |
| Contacts, mailboxes, campaigns, sequences | Datalake, public contracts |
| Send limits, follow-ups, stop-on-reply | Commercial signals, facts |
| Inbox, replies, bounces, suppression | Evidence, hypotheses |
| CRM, tasks, analytics | Prioritization, provenance |
| Staging import + review queue | Canonical decision/outcome memory |

Warmbly **must not**:

- connect to the extra-cli production Postgres
- write into the datalake
- re-score leads or re-run commercial analytics

## Feature flag

| Variable | Default | Meaning |
| --- | --- | --- |
| `CONFENGE_OUTREACH_ENABLED` | `false` | Master switch for API + import |
| `CONFENGE_AUTO_SEND_ENABLED` | `false` | Reserved; never default on |
| `CONFENGE_REQUIRE_HUMAN_APPROVAL` | `true` | Per-touch approval required (see [touchpoints.md](./touchpoints.md)) |
| `CONFENGE_EXTRA_CLI_FEED_URL` | empty | HTTPS or `file://` feed |
| `CONFENGE_EXTRA_CLI_FEED_TOKEN` | empty | Optional Bearer for HTTPS |
| `CONFENGE_EXTRA_CLI_ALLOWED_HOSTS` | empty | Required in prod when feed URL set |
| `CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT` | `100` | Campaign-shell daily ceiling (secondary; primary pace is CONFENGE_GLOBAL_SENDS_PER_HOUR=10) |
| `CONFENGE_MAX_INITIAL_EMAIL_WORDS` | `120` | Future copy validators |
| `CONFENGE_MAX_FEED_PAYLOAD_BYTES` | `33554432` | Import payload cap |

In production, feed and outcome URLs must be `https://`, and the feed host
must be on the allowlist. Fetch uses the SSRF-hardened HTTP client.

## Contract `confenge.outreach.v1`

Top-level:

```json
{
  "schema_version": "confenge.outreach.v1",
  "generated_at": "2026-08-06T12:00:00Z",
  "source": {
    "system": "extra-cli",
    "run_id": "...",
    "snapshot_hash": "...",
    "repo_sha": "...",
    "profile_id": "confenge",
    "profile_version": "1.0.0"
  },
  "pagination": { "cursor": null, "next_cursor": null, "has_more": false },
  "leads": []
}
```

Each lead carries company (CNPJ14), priority (opaque scores from extra-cli),
moment, offer, messaging_context, contacts, contracts, evidence, and
`commercial_state`. Missing email is valid: the company enters as
`NEEDS_CONTACT`, never discarded.

Synthetic fixtures: `internal/app/confenge/testdata/`.

### Legacy adapters

If `schema_version` is absent, the importer normalizes:

- top-level `leads.json` arrays
- commercial run objects with a `leads` key
- flat contact fields (`email`, `contact_name`, …)

Missing fields are left empty. Nothing is invented.

## Data model (staging)

Multi-tenant tables (migration `000083_outreach_staging`):

- `outreach_import_runs` — audit of each dry-run/apply
- `outreach_accounts` — company staging, unique `(organization_id, cnpj14)`
- `outreach_contact_candidates` — candidates before Warmbly contact promotion
- `outreach_evidence` — sanitized text evidence per account

Human flags (`do_not_contact`, `blocked`, post-send queue states) survive
reimport. Machine fields (score, moment, offer) update on content-hash change.

## API (JWT + org context)

Base: `/api/v1/confenge` (requires organization).

| Method | Path | Permission | Notes |
| --- | --- | --- | --- |
| GET | `/status` | any org member | Works even when disabled |
| GET | `/summary` | view contacts | Queue counters |
| GET | `/accounts` | view contacts | Filters: `queue_state`, `cnpj14`, `q` |
| GET | `/accounts/:id` | view contacts | Includes contacts + evidence |
| POST | `/accounts/:id/block` | manage contacts | Optional `do_not_contact` |
| POST | `/import` | manage contacts | Body = feed JSON or `{"uri":"..."}` |
| GET | `/import-runs` | view contacts | Recent runs |
| GET | `/import-runs/:id` | view contacts | Run detail + counts |

Import query/header:

- `?dry_run=true` — validate + count only
- `Idempotency-Key` — same key + same payload returns prior run; different payload → `409`

## Local one-command ops

See [local-ops.md](./local-ops.md) for `make confenge-local`,
preflight, import, kill switch, and the offline demo path.

## Local quickstart

1. Set `CONFENGE_OUTREACH_ENABLED=true` on the backend env.
2. Apply migrations (`make backend` applies embedded migrations).
3. Import fixture:

```bash
curl -sS -X POST "$API/api/v1/confenge/import?dry_run=true" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @internal/app/confenge/testdata/native_feed_v1.json
```

4. Apply (no dry_run), then `GET /confenge/summary` and `GET /confenge/accounts`.

## Tests

```bash
go test ./internal/app/confenge/ -count=1
```

Covers native + legacy parse, dry-run, idempotency, DNC preservation, multi-tenant
isolation, evidence add, invalid feed, queue states for missing/unverified contacts.

## Upstream maintenance

See [upstream-maintenance.md](./upstream-maintenance.md).

## Outcome loop (PR3)

Outcome webhook env vars are reserved. Delivery uses HMAC + outbox; Warmbly
never writes to the extra-cli database.
