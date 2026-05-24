# Dev seed

`cmd/seed` populates a fresh database with a complete, internally-consistent
fixture set so every product surface can be exercised end-to-end without
manually clicking through onboarding.

It is idempotent: every insert is an `ON CONFLICT DO UPDATE` keyed on a
stable UUID, so re-running against an existing database is safe.

## Running it

### As part of docker compose

```
make dev-up
```

The compose file adds a `seed` service that runs once after Postgres becomes
healthy, then exits. The backend, consumer, and workers each `depends_on:
seed: condition: service_completed_successfully`, so by the time the API is
ready the data is in place.

To wipe state and reseed:

```
make dev-reset
```

### Against an existing local Postgres

```
make seed
```

This connects to `postgres://warmbly:warmbly@localhost:5432/warmbly_dev` by
default. Override with `PRIMARY_DB=…`.

## What it seeds

| Domain | Notes |
|---|---|
| `durations`, `plans` | Free Trial, Starter, Pro monthly + annual, Enterprise |
| `users` | super admin, org owner, trial founder, manager, viewer (all use the same password) |
| `organizations` + `organization_members` | Acme Inc (owner + manager + viewer + admin) and Globex (founder only) |
| `organization_invitations` | One pending invite into Acme |
| `subscriptions` | Acme on Pro Monthly (active), Globex on Free Trial (14 days remaining) |
| `workers` | Three workers (free shared, premium shared, dedicated) all marked `active` |
| `dedicated_worker_assignments` | Dedicated worker assigned to Acme owner |
| `email_accounts` | Two on Acme (one Gmail OAuth, one SMTP/IMAP, both warmup-enabled in the premium pool), one on Globex |
| `email_accounts_oauth` / `_smtp_imap` | Provider-specific credential rows (fake values) |
| `warmup_pool_participants` | Both Acme mailboxes enrolled in the premium pool |
| `folders`, `tags`, `categories` | A starter pair of each, owned by the Acme owner |
| `reply_templates` | "Quick yes" and "Polite no" for Acme |
| `campaigns` | Two on Acme (one active, one draft) and one on Globex |
| `sequences` | Three-step sequence on the active Acme campaign |
| `contacts` + `campaign_leads` | 8 contacts in Acme's active campaign, 3 in Globex's draft |
| `campaign_contact_progress` | Realistic funnel: every contact has step 1 sent, some opened/clicked/replied |
| `campaign_logs` | A handful of state-change events |
| `pipelines`, `pipeline_stages`, `deals`, `crm_tasks`, `contact_notes`, `contact_activities` | Full sales pipeline with two deals (one open, one won), two tasks, one note, eight activity events |
| `api_keys` | A full-access key for Acme (the plaintext is printed at the end of the run) |
| `admin_audit_logs` | Four sample admin actions |
| `enterprise_inquiries` | One pending Stark Industries inquiry |

## Credentials

The seed prints a complete credentials block at the end of its run. The
short version:

- All users share password `Test1234!`.
- `admin@warmbly.local` is the platform super-admin.
- `owner@warmbly.local` owns Acme (Pro plan, dedicated worker).
- `founder@warmbly.local` owns Globex (Free Trial).
- `manager@warmbly.local` and `viewer@warmbly.local` are non-owner members of Acme.
- API key plaintext is printed once per run, prefixed `wmbly_se`.

## Known caveats

- The worker containers in docker compose start up but cannot perform real
  KMS, S3, or DynamoDB calls without AWS credentials. They register
  themselves against the seeded worker UUIDs (via `hostname:`) and consume
  from Kafka; actually sending mail still requires real cloud creds.
- The Free Trial plan and `durations` rows are inserted by migration
  `000015`; the seed only adds the paid plans on top.
- Stripe IDs in the seeded subscription rows are placeholders. Webhooks
  pointed at the dev backend will not match real Stripe events.
