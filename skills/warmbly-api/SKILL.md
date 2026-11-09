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
Creating one in the dashboard asks you to confirm your password or a two-factor
code first; creating one with an existing key does not, because the key's scope
is already that grant.
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
| `campaign` | list, get, create, update, delete, steps, senders, preflight, start, stop, test-email, logs, plan, pause-lead / resume-lead, lead-cc / set-lead-cc / lead-cc-suggestions |
| `contact` | list (search), get, lookup, create, update, delete, notes, timeline, import, imports, import-status, import-start, import-cancel, export |
| `mailbox` | list, get, update, delete, auth-check, sync, skip-folders, identity, refresh-identity, behavior, verify, send, warmup-start/pause/resume/stop/status |
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
  opaque; never construct one, and never change the sort or filters halfway
  through a walk: a cursor belongs to the ordering it came from and is rejected
  under another.
- Errors carry `code` and `request_id`. Branch on `code`
  (`not_found`, `forbidden`, `rate_limit_exceeded`, ...), quote `request_id`
  when reporting a failure.
- On `rate_limit_exceeded` wait the `Retry-After` the error names, then retry.
- Retried writes: pass `--idempotency-key <same-key>` so a retry can never
  double-apply. Any unique string works; reuse it only for the identical retry.
  A `409 idempotency_outcome_unknown` means the first attempt ended in a server
  error and may have applied: retry with a new key only once you have
  established that it did not.
- `contact list` is a search: `--data` carries the filter body, e.g.
  `--data '{"query":"acme.com"}'`. Omit it to list everything.

## Sending safety - read before anything that sends

These commands put real mail on the wire: `campaign start`,
`campaign test-email`, `mailbox send`, `inbox reply`, `inbox compose`,
`inbox approve-draft`. Everything else is safe to run freely.

- Run `campaign preflight --id <id>` before `campaign start` and act on what
  it reports. It costs nothing and catches missing senders, empty audiences
  and broken tracking.
- When a campaign sends less than expected, `campaign plan --id <id>` is the
  answer: today's projected sends and every limit that lowered them. Read it
  before touching a cap.
- To send each lead inside their own working day, set the campaign's
  `timezone_mode` to `recipient`, then check `campaign timezones --id <id>`:
  its leads by timezone and how many have no location (those are sent on the
  campaign's own timezone). Location comes from the contacts' country, state,
  city, location or timezone fields, then phone and email domain; segment
  conditions `world_region`, `country` and `timezone` split an audience by it.
- Never raise a mailbox's daily cap casually. The platform default is 50
  campaign emails per mailbox per day with 600 seconds between sends; a fresh
  mailbox should start around 10-20. Do not set a cap above 50 unless the
  user explicitly asked for it and the mailbox has history to justify it.
- Keep warmup running on mailboxes that campaign; do not stop warmup just
  because a campaign started.
- To stop emailing ONE contact for a while, hold that lead:
  `campaign pause-lead --id <campaign> --contact <contact> --data
  '{"until":"2026-09-21T17:00:00Z"}'`. Do not unsubscribe them and do not add
  them to the suppression list for this: both are workspace-wide and permanent.
  `campaign resume-lead` lifts it. An out-of-office auto-reply already writes
  the same hold by itself, across every campaign that contact is a lead of, so
  a lead reading `paused` for that reason needs nothing from you.
- To reach two people at one company in ONE thread, copy the second on the
  first lead's emails: `campaign set-lead-cc --id <campaign> --contact <lead>
  --data '{"contact_ids":["<colleague>"]}'` (at most two;
  `campaign lead-cc-suggestions` lists likely colleagues). Do not enrol both as
  leads of the same campaign for this: a copied contact's own lead is held
  anyway, and resuming that hold sends them a second thread.
- If deliverability analytics show rising bounces or complaints, stop the
  campaign first and report; do not push volume into a degrading mailbox.

## Recovery and instance administration

Creating accounts, resetting passwords, granting admin, and instance health
are the operator half of warmblyctl and need database access, not an API key.
That is the `warmbly-ops` skill.
