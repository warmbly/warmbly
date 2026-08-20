---
name: warmbly-api
description: Operate a running Warmbly instance (hosted or self-hosted) as an agent through the warmblyctl API commands - list/create/start campaigns, manage contacts and mailboxes, read and reply in the unified inbox, change settings, read analytics. Use whenever the task is to interact with Warmbly the product (campaigns, contacts, mailboxes, inbox, warmup, webhooks) rather than to change its source code.
---

# Driving Warmbly through warmblyctl

`warmblyctl` speaks Warmbly's public REST API. Every API command prints the
API's JSON response to stdout and exits 1 with a machine-readable error on
failure, so it is safe to script against.

## Setup

Two environment variables:

```bash
export WARMBLY_API_KEY=wmbly_...                       # required
export WARMBLY_API_URL=https://api.your-instance.com  # omit for the hosted service
```

Keys are created in the dashboard under Settings > API keys, or with
`warmblyctl apikey create` if you already hold a key with the API_KEYS scope.
Everything you can do is bounded by the key's scopes; `warmblyctl me` shows
who the key is and what it holds. On the local dev stack the seeded
full-access key is `wmbly_seed_acme_owner_full_access_0000000000` with
`WARMBLY_API_URL=http://localhost:8080`.

## Command map

Run `warmblyctl <family> --help` for subcommands and `warmblyctl <family>
<sub> --help` for flags. Families:

| Family | Covers |
|---|---|
| `me` | Identity and granted scopes |
| `campaign` | list, get, create, update, delete, steps, senders, preflight, start, stop, test-email, logs |
| `contact` | list (search), get, lookup, create, update, delete, notes, timeline, import, export |
| `mailbox` | list, get, update, delete, auth-check, sync, behavior, verify, send, warmup-start/pause/resume/stop/status |
| `inbox` | list, count, thread, seen, reply, compose, agent drafts, scheduled sends |
| `analytics` | dashboard, deliverability, warmup, accounts, campaigns, usage, audit-logs |
| `settings` | outreach and suppression settings |
| `webhook` | endpoints, secrets, deliveries, event types |
| `apikey` | self-service key management |
| `template` | reply templates |
| `crm` | pipelines, deals, tasks |

Anything without a typed command is reachable through the raw passthrough:

```bash
warmblyctl api get "/campaigns?limit=10"
warmblyctl api post /contacts --data '{"email":"jane@example.com"}'
warmblyctl api patch "/campaigns/<id>" --data @changes.json
```

Paths are relative to `/v1`. Write bodies are JSON: a literal, `-` for stdin,
or `@file`.

## Conventions

- Lists return `{"data": [...], "pagination": {"next_cursor", "has_more"}}`.
  Page with `--cursor <next_cursor>` until `has_more` is false. The cursor is
  opaque; never construct one.
- Errors carry `code` and `request_id`. Branch on `code`
  (`not_found`, `forbidden`, `rate_limit_exceeded`, ...), quote `request_id`
  when reporting a failure.
- On `rate_limit_exceeded` wait the `Retry-After` the error names, then retry.
- Retried writes: pass `--idempotency-key <same-key>` so a retry can never
  double-apply. Any unique string works; reuse it only for the identical retry.
- `contact list` is a search: `--data` carries the filter body, e.g.
  `--data '{"query":"acme.com"}'`. Omit it to list everything.

## Sending safety - read before anything that sends

These commands put real mail on the wire: `campaign start`,
`campaign test-email`, `mailbox send`, `inbox reply`, `inbox compose`,
`inbox approve-draft`. Everything else is safe to run freely.

- Run `campaign preflight --id <id>` before `campaign start` and act on what
  it reports. It costs nothing and catches missing senders, empty audiences
  and broken tracking.
- Never raise a mailbox's daily cap casually. The platform default is 50
  campaign emails per mailbox per day with 600 seconds between sends; a fresh
  mailbox should start around 10-20. Do not set a cap above 50 unless the
  user explicitly asked for it and the mailbox has history to justify it.
- Keep warmup running on mailboxes that campaign; do not stop warmup just
  because a campaign started.
- If deliverability analytics show rising bounces or complaints, stop the
  campaign first and report; do not push volume into a degrading mailbox.

## Recovery and instance administration

Creating accounts, resetting passwords, granting admin, and instance health
are the operator half of warmblyctl and need database access, not an API key.
That is the `warmbly-ops` skill.
