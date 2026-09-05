---
name: warmbly-cli
description: Use the `warmbly` CLI to drive Warmbly as a signed-in user - sign in with `warmbly auth login`, then work with campaigns, contacts, mailboxes, the unified inbox, analytics, webhooks and the live event stream on the hosted service or any self-hosted instance. Use whenever the task is to operate Warmbly the product from a terminal or a script and a `warmbly` binary is available. For instance recovery and accounts (database-level), use warmbly-ops instead.
---

# Driving Warmbly through the `warmbly` CLI

`warmbly` is the customer CLI: it signs in as a person, holds one credential
per host in `~/.config/warmbly`, and speaks only the public REST API. Every
command is bounded by the scopes the sign-in approved.

If the binary is not on PATH, install it without a toolchain or root:

```bash
curl -fsSL https://warmbly.com/cli.sh | sh     # macOS, Linux
irm https://warmbly.com/cli.ps1 | iex          # Windows
```

Add `-s -- --dir <path>` to place it somewhere specific. It is also inside the
backend image on a self-hosted instance (`docker compose -p warmbly exec
backend warmbly ...`).

It is not `warmblyctl`. That one talks to Postgres and exists for recovery and
accounts (the `warmbly-ops` skill). If both are available and the task is
product work, use `warmbly`.

## Getting authenticated

Check first, because a non-interactive agent cannot complete a browser flow:

```bash
warmbly auth status
```

- **Signed in** (exit 0): carry on.
- **Not signed in** (exit 4): you need a credential. In order of preference:
  1. `WARMBLY_TOKEN` in the environment. It overrides everything and is never
     written to disk, so it is the right answer for a script or a CI job.
  2. `echo "$KEY" | warmbly auth login --with-token`, when a key was supplied.
  3. `warmbly auth login`, which needs a human at a browser. Print the code and
     the URL it shows and hand back to the user; do not sit in the poll loop
     waiting for something only they can do.

Set `WARMBLY_HOST` (or `--host`) for a self-hosted instance, and
`WARMBLY_API_URL` only when the API is not at `api.<host>`.

Exit code 4 always means the credential: missing, rejected, or short a scope.
`warmbly auth status` names which, and where the token came from.

## Output: always ask for JSON

Tables are for humans. Every command takes `--json`, and output is JSON
automatically when stdout is not a terminal, but pass it explicitly so the
shape does not depend on how you were invoked.

```bash
warmbly campaign list --json
warmbly campaign list --json --all      # every page, cursor followed
warmbly contact list --json --limit 100
```

Lists are `{"data": [...], "pagination": {"next_cursor", "has_more"}}`. Page
with `--cursor <next_cursor>`, or let `--all` do it. Cursors are opaque, never
construct one.

## Command map

`warmbly <command> --help` lists subcommands; `warmbly <command> <sub> --help`
gives the arguments and flags. Ids are positional, not flags.

| Command | Covers |
|---|---|
| `status` | one call for "what is happening": mailboxes needing attention, what is sending, what is unread |
| `campaign` | list, view, create, edit, delete, steps, senders, segments, preflight, test, start, stop, logs |
| `contact` | list, view, create, edit, delete, lookup, timeline, emails, notes, import, export, verify |
| `mailbox` | list, view, edit, check, sync, behavior, warmup, hold, release, send |
| `inbox` | list, view, thread, read, reply, compose, drafts, scheduled, snooze |
| `suppression` | the list of addresses and domains that get no campaign mail |
| `segment`, `template`, `automation`, `form` | audiences, reply templates, automations, lead capture |
| `deal`, `pipeline`, `task` | the CRM |
| `analytics`, `audit`, `advisor` | numbers, the audit trail, recommendations |
| `webhook`, `key`, `oauth-app`, `integration` | the developer surface |
| `org`, `team`, `settings`, `warmup-routing` | the workspace surface a key can reach |
| `tool` | the AI tool registry, listed and called |
| `events tail` | the live event stream |
| `api` | any endpoint at all |

Anything without a command is reachable through the passthrough. Paths are
relative to `/v1`:

```bash
warmbly api "/campaigns?limit=10" --paginate
warmbly api /contacts -f email=jane@example.com -f first_name=Jane
warmbly api /campaigns/CAMPAIGN_ID -X PATCH -F daily_limit=40
warmbly api /contacts/search -X POST --input filter.json
```

`-f` keeps a string, `-F` guesses the type (`true`, `null`, numbers, `@file`),
`key[sub]=v` nests, repeated `key[]=v` builds an array.

## Sending safety, read before anything that sends

These put real mail on the wire and prompt before doing so:
`campaign start`, `campaign test`, `mailbox send`, `inbox reply`,
`inbox compose`, `inbox approve-draft`. Everything else is safe to run freely.

- With no terminal they refuse rather than send. `--yes` is what proceeds, so
  **only pass `--yes` when the user asked for that specific send.** Never add
  it globally to be rid of prompts.
- Run `warmbly campaign preflight CAMPAIGN_ID` before `campaign start` and act
  on what it reports. It costs nothing and catches missing senders, empty
  audiences and broken tracking.
- Never raise a mailbox's daily cap casually. The default is 50 campaign
  emails per mailbox per day with 600 seconds between sends; a fresh mailbox
  starts around 10-20. Do not go above 50 unless the user asked and the
  mailbox has the history to justify it.
- Keep warmup running on mailboxes that campaign. Do not stop warmup because a
  campaign started.
- If deliverability shows rising bounces or complaints, stop the campaign and
  report. Do not push volume into a degrading mailbox.

## Errors

Failures print the API's `code` and `request_id` to stderr. Branch on `code`
(`not_found`, `forbidden`, `rate_limit_exceeded`, ...) and quote `request_id`
when reporting. On `rate_limit_exceeded`, wait the `Retry-After` it names.

For a write you retry, pass `--idempotency-key <same-key>` so a retry cannot
double-apply. Any unique string works; reuse it only for the identical retry.

Exit codes: `0` worked, `1` failed, `2` bad command line or a prompt with no
terminal, `4` credential.

## Watching what happens

```bash
warmbly events tail --json --intent EMAIL
```

Streams the live event stream as newline-delimited JSON. Needs a key with
`REALTIME_SUBSCRIBE`. Useful for confirming a send actually went out; give it
`--count N` so it terminates rather than running forever.

## What this CLI cannot do

- Connect a mailbox. That needs OAuth consent or a credential form in a
  browser: `warmbly browse mailboxes --no-browser` prints the URL to hand over.
- Manage members, roles, invitations, workspace exports or billing. Every
  `/organization/*` and `/subscription/*` route is session-only and refuses an
  API key, so there is no command for them. `warmbly browse settings
  --no-browser` prints the URL to hand over.
- Create accounts, reset passwords, grant platform admin, back up or restore an
  instance. That is `warmblyctl` and the `warmbly-ops` skill.
