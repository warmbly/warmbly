# Warmbly Agent Notes

## Purpose

Warmbly is an email warmup and cold outreach platform.

At a product level, the app does four main things:

- manages sender accounts and their assignment to workers
- sends campaign and warmup mail through distributed workers
- syncs mailbox state back into the platform
- tracks opens, clicks, replies, suppression, and deliverability signals

The backend API is the control plane. Workers are the execution plane.

## Working In This Repo

CI is strict. `go build ./...` succeeding is not enough — `golangci-lint` runs `gofmt` as part of its checks, and a single unformatted import block or mis-indented doc comment will fail the PR even when the code compiles cleanly. Before declaring any Go change done:

- run `gofmt -w` on every Go file you touched (or `gofmt -w internal/ cmd/` to be safe)
- run `make lint` locally when the toolchain is installed, or at minimum `gofmt -l ./...` should print nothing
- do not rely on `go build` as the "ship signal" — it ignores formatting and stylistic lint rules that CI enforces

Other CI-touching rules:

- the frontend trees (`admin/`, `web/`, `site/`) each have their own CI jobs; run `pnpm typecheck` in any tree you touched and `pnpm lint` when the rules are non-trivial
- never push without first re-running the relevant `*build*` / `*typecheck*` / `*lint*` step on the affected tree
- a `make lint` (or `gofmt -l`) failure is always a real CI failure; do not push hoping it will pass

Docs stay in sync:

- the customer docs site lives in `docs/` (Fumadocs, served at docs.warmbly.com); content is MDX under `docs/content/docs/` in three sections: `guides/` (product behavior), `learn/` (fundamentals), `api/` (API reference)
- any change that alters user-visible behavior must update the matching docs page in the same change: a new or changed endpoint updates `api/endpoints.mdx` (scope map) and, where relevant, `api/authentication.mdx`; a new or changed API permission updates `api/permissions.mdx` including the permission table, presets, and all three language tabs in the constants section; a new or changed error code updates `api/error-codes.mdx`; a new or changed product feature, default, limit, or setting updates the relevant `guides/` page (or adds one, registered in `guides/meta.json` with an `icon`)
- removing or renaming a feature, endpoint, or permission means removing or updating its docs too; do not leave stale docs behind
- follow the docs conventions: frontmatter `title` is the H1 (no `#` heading in the body), every page has a lucide `icon`, sentence-case headings, no em dashes in prose, internal links use trailing slashes (`/guides/mailboxes/`)
- verify with `pnpm types:check` and `pnpm lint` in `docs/` (the site is a fully static export; `pnpm build` writes `out/`)

Commit hygiene:

- when instructed to make a commit, use the subject format `feat: <explanation>`
- one line, no body. Make the line long and specific (what changed and where), not a stub like `feat: fix docs`
- no `Co-Authored-By:` or other AI/agent attribution footers; rewrite any commit that has one before opening or updating a PR

Copy / writing style:

- do not lean on em dashes (`—`). Use them sparingly, only when one is genuinely the clearest option; prefer a period, comma, colon, or parentheses instead. This applies to user-facing copy and microcopy in `site/` and `web/`, and to docs. Overusing em dashes reads as machine-written.

Code comments:

- keep them short: one line stating the non-obvious constraint or intent. No multi-line essays; if a comment needs a paragraph, the explanation belongs in docs or the PR description

Data modeling / representation:

- we are happiest with the most **type-safe** option, but the rule is: pick the **most effective option for the actual use case**, not type-safety for its own sake.
- prefer real typed columns / enums when the data is fixed-shape, queried or filtered in SQL, or benefits from FK integrity.
- a `jsonb` column is the right call when the data is a free-form, evolving, read-then-execute blob that isn't filtered in SQL (e.g. the `sequences.conditions` branching tree and `sequences.action` node config) — keep it type-safe at the app boundary with a Go struct + validation on write, and a DB `CHECK` on any discriminator column.

### Verification: what to run, what to skip

Keep the loop fast. The signals that matter are formatting, lint, and typecheck — not local builds or browser automation.

Always, before calling a Go change done:

- run `make fmt` (or `gofmt -w cmd internal`); `gofmt -l ./...` must print nothing
- run `make lint` (golangci-lint)

For frontend changes, run `pnpm typecheck` and `pnpm lint` in any tree you touched.

Do not:

- do not run `go build ./...`, `pnpm build`, or docker image builds as a "did it work" check. They are slow and are not what CI gates on. `go run` (via the make dev targets) already compiles; `make fmt` + `make lint` + `pnpm typecheck` are the real signals.
- do not write or run Python/Playwright (or any browser-automation) scripts to test the app. Manual, in-browser verification is the user's job against the native dev stack (`make infra` + `make backend` + `make web`). Do not add screenshot/e2e test harnesses to this repo.
- do not run the Go test suite as a default gate unless the task is specifically about those tests.
- do not push hoping CI passes; a `gofmt -l` / `make lint` / `pnpm typecheck` failure is always a real CI failure.

## Local Development

Infra runs in docker; the Go services and frontends run natively on the host for fast iteration — no docker image rebuilds when you change app code. Targets live in the `Makefile`.

- `make infra` — start the backing services in docker (postgres, redis, kafka, schema-registry, mailpit, localstack + init, cloud-tasks, stripe-mock). Run once; leave running.
- `make backend` — run the API natively on `:8080` (applies the embedded migrations on boot against the docker postgres).
- `make consumer` / `make worker` — run those Go services natively, each in its own terminal. The worker reads encrypted DEKs through the backend's `/internal/dek` endpoint (the prod `http` provider, no worker DB), so `make backend` must be running and their `INTERNAL_API_TOKEN` must match (the targets are pre-wired to match).
- `make run` — backend + consumer + worker together in one terminal (Ctrl-C stops all).
- `make tracking` / `make realtime` — the Rust tracking pixel service (:3000) and Elixir/Phoenix websocket fanout (:4000). Deliberately kept out of `make run`; start them only when needed, and only if you have the cargo / elixir toolchains on the host.
- `make web` / `make admin` / `make site` — frontend dev servers (5173 / 5174 / 4321), pointed at the native backend.
- `make seed` — load fixtures (after the backend has applied migrations).
- `make fmt` / `make lint` — format and lint Go.

Prefer native `make backend` over rebuilding the docker backend image: docker rebuilds are slow because the image bakes in the migrations and the compiled binary, so a one-line change means a full image build + container recreate. The native targets skip all of that. The dockerized hot-reload flow (`make app`) and prod-image smoke test (`make up`) remain available when you specifically need containers.

Dashboard realtime:

- dashboard experiences should be realtime by default. When emails arrive, contacts are added, records change, or any dashboard-visible feature updates, the dashboard should reflect it live without requiring a manual refresh
- aim for a responsive, Discord-like product feel: presence, counts, lists, detail panes, notifications, and workflow state should stay current across every dashboard feature where live updates are meaningful
- when changing dashboard behavior, it is acceptable to safely change the API structure if a better solution exists. Before making an API shape change, ask the user how they want to handle it, especially when the current API may already be published or backwards compatibility might require a new API version

Public API quality bar:

- treat customer-facing API changes as contract changes. Prefer additive changes inside a version, and use a new API version for incompatible behavior once an endpoint is published
- every API-key-capable route must have an explicit API permission gate and, for JWT callers, the matching organization permission gate
- side-effectful POST/PATCH/PUT/DELETE endpoints should support `Idempotency-Key` or have a documented reason why retries are naturally safe
- error responses should include stable machine-readable `code` and `request_id` fields in addition to human-readable text
- list endpoints should use consistent `data` plus `pagination` shapes with opaque cursors; invalid cursors or limits should return `400` instead of being ignored
- webhook endpoints must stay HMAC-signed, HTTPS by default, and protected against obvious SSRF targets. Only development/self-hosted environments should opt into unsafe webhook URLs

## Dashboard UI Conventions (`web/`)

Everything in the dashboard must use our own theme, not browser/library defaults.

- Number fields: never ship a raw `<input type="number">` with the native spinner. Use the shared `NumberInput` from `@/components/ui/field` — it strips the native up/down arrows (`appearance:none`) and renders our own themed chevron steppers. Do not re-add the default stepper anywhere.
- Inputs/labels: reuse `TextInput`, `SearchInput`, `Label`, `NumberInput` from `@/components/ui/field`; don't hand-roll raw `<input>`/`<select>` with ad-hoc classes when a primitive exists.
- Pickers: tag/category multi-selects share one visual language — bordered chip box + framer-motion dropdown with a search header + checkbox-square rows (see contacts `CategoryPicker` and `popup/select/TagSelector`). Reuse `useFlipPlacement` + `useClickOutside`.
- Detail drawers + their tab bars share one pattern (see `emails/InboxDetails` and contacts `ContactEdit`): a `shrink-0 px-3 flex items-center gap-1 border-b border-slate-200` bar, each tab a `relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px]` button with a lucide icon, active = `text-slate-900 font-medium` + a `bg-sky-600` underline span.
- Theme tokens: slate borders (`border-slate-200`), sky accents (`focus:border-sky-400 focus:ring-sky-100`, `bg-sky-50 text-sky-700`), `rounded-md`, `text-[12.5px]` base, `h-7` controls, `10px uppercase tracking-[0.14em]` section labels.
- Multi-select tables: when rows are selected, show a floating bottom-center selection bar with the count + bulk actions (mirror `SelectionBar` in contacts `ContactsTable.tsx`).
- Row actions must be reachable on touch: never hide the only affordance behind `opacity-0 group-hover` with no mobile fallback. Use `opacity-100 md:opacity-0 md:group-hover:opacity-100`, or surface actions in the detail drawer.
- Confirmations: never use the native `window.confirm` / `alert` / `prompt`. Use the in-app confirm: `const confirm = useConfirm()` (from `@/hooks/context/confirm`), then `confirm.show(text, onSubmit)`. `onSubmit` is awaited and the provider renders its own loading spinner, so pass an `async` callback (prefer `mutateAsync` over callback-style `mutate`). For the synchronous `if (!window.confirm(x)) return; act()` pattern, restructure to `confirm.show(x, act)`; for close-while-dirty guards, route every close path (Escape handler, backdrop `onMouseDown`, close button) through one `requestClose()` that calls `confirm.show(...)` when dirty. ConfirmProvider is mounted in `app/app/layout.tsx`, so `useConfirm()` works anywhere under `/app`.
- Row interactions: list rows behave like the campaigns list — clicking anywhere on a row opens that item's detail (drawer or page); right-side action buttons (3-dots / "More") open a relevant detail/tab (e.g. the mailbox 3-dots opens the Settings tab of `InboxDetails`). Inner interactive controls (checkbox, dropdown trigger, action buttons) must `e.stopPropagation()` so they don't also fire the row's open handler.
- Prefer realtime over polling: subscribe to the socket and `queryClient.invalidateQueries(...)` on the relevant event instead of `refetchInterval` where an event exists (see `useRealtimeEvents` / `RealtimeManager`).

## System Shape

- `cmd/backend`: API and business orchestration
- `cmd/consumer`: consumes Kafka events and updates platform state
- `cmd/worker`: execution worker for send/sync operations
- `tracking/`: open and click tracking service
- `realtime/`: websocket fanout service
- `web/`: in-product frontend (dashboard)
- `site/`: public marketing site (Astro 5 + Tailwind v4)
- `deploy/`: production deploy manifests, infrastructure, and runtime config
- `docs/`: engineering documentation and operational runbooks
- `resources/`: architecture notes and longform design context
- `scripts/`: one-off tooling (codegen, migrations, local dev utilities)

## Worker Topology

Workers are intended to run distributed across many machines, with one worker process per machine.

That layout matters because it lets the system spread sending activity across different machine-level network identities and IP addresses instead of concentrating traffic through a single sender runtime.

In production, workers are treated as individually addressable executors:

- each worker has its own `worker_id`
- email accounts are assigned to a specific worker
- worker events are delivered through worker-specific Kafka topics
- the platform can rebalance or migrate accounts between workers

This repo already models three worker modes:

- shared free-tier workers
- shared premium workers
- dedicated workers assigned to a single paying organization

The relevant code paths are in:

- `internal/app/worker/assignment.go`
- `internal/repository/pg_worker.go`
- `internal/infrastructure/db/migrations/000015_worker_tiers.up.sql`

## Warmup Pool Model

Warmup traffic is also separated by pool:

- `free`
- `premium`

This is modeled in:

- `internal/infrastructure/db/migrations/000010_warmup_pools.up.sql`
- `internal/repository/pg_warmup.go`
- `internal/tasks/email_task.go`

Keep this separation intact. Free-tier accounts should not silently mix into premium warmup traffic, and dedicated-worker accounts should still follow the intended warmup pool policy explicitly rather than by accident.

## Worker Networking Rules

The worker should stay operationally lightweight because there can be a lot of them.

Design intent:

- workers should not depend on PostgreSQL or other direct SQL access
- workers should receive commands from Kafka
- workers should publish results back through Kafka
- workers may talk to infrastructure-style services that scale independently, such as S3, KMS, and cache layers
- relational data the worker needs (encrypted DEKs, the messageId→internal-email map) is reached over the backend's internal HTTP API (`/api/v1/internal/...`), never via direct SQL
- worker-local state should be minimal and disposable

Current code matches that intent in `cmd/worker/main.go`: the worker boots Kafka, Redis cache, KMS, and S3 clients, and reaches DEKs + the email message map through the backend's internal API, but does not open a PostgreSQL connection.

When changing worker behavior, preserve that boundary unless there is a very strong reason not to.

## Encryption Model

Warmbly uses envelope encryption for application secrets and sensitive payloads.

High-level flow:

- AWS KMS is the root of trust
- each organization gets a data encryption key (DEK)
- the plaintext DEK is used for application-layer encryption and decryption
- the encrypted DEK is stored, not the plaintext DEK
- decrypted DEKs are cached for reuse

Current implementation:

- KMS generates a 32-byte DEK for AES-256
- the encrypted DEK blob is base64-encoded and stored via the pluggable `encryptedkeys.Store` (the `postgres` backend writes the `organization_encrypted_keys` table; workers use the `http` backend, which proxies to the backend's `/api/v1/internal/dek` endpoint)
- the plaintext DEK is cached in Redis with a TTL
- encrypted fields are sealed with AES-GCM and then base64-encoded

Main code paths:

- `internal/app/cipher/cipher.go`
- `internal/app/cipher/encrypt.go`
- `internal/app/cipher/decrypt.go`
- `internal/app/cipher/cache.go`
- `internal/infrastructure/kms/encryption.go`
- `internal/infrastructure/kms/decryption.go`
- `internal/infrastructure/encryptedkeys/` (`store.go`, `factory.go`, `postgres.go`, `http.go`)
- `internal/api/handler/internal_dek.go` (the worker-facing DEK proxy endpoint)

Operational guidance:

- do not introduce plaintext storage of secrets or message content where the current design expects encrypted values
- DEKs are per-organization and live in the `organization_encrypted_keys` Postgres table behind the `encryptedkeys.Store` interface (provider selected by `ENCRYPTED_KEYS_PROVIDER`: `postgres` for backend/consumer, `http` for workers). DynamoDB is no longer used anywhere; do not reintroduce it. Losing a DEK is unrecoverable, so any change to DEK storage needs a migration plan. Do not reintroduce per-user DEKs: mailboxes, integration tokens, and message content are organization assets, and keying them by user breaks when that user is offboarded
- if workers need access to encrypted payloads, prefer passing encrypted material plus access to KMS-backed decryption primitives, or an internal backend API, rather than introducing direct SQL dependencies
- be explicit about which fields are encrypted at rest in app code versus stored in infrastructure services like S3

## Sending Safety Policy

Cold email safety should be mailbox-first, not worker-first.

Do not think of a worker as having one flat global send limit. A worker's safe outbound volume should be the sum of the budgets of the mailboxes assigned to it, with volume spread across many mailboxes and many worker IPs instead of concentrated through one runtime.

### Hard product defaults in this repo

These are the current built-in defaults and guardrails:

- default cold campaign cap per mailbox: `50` emails/day
- default minimum gap per mailbox: `600` seconds between sends
- default warmup start per mailbox: `10` emails/day
- default warmup ceiling per mailbox: `40` emails/day
- default warmup ramp: `+1` email/day
- `campaign_limit` updates are validated up to `100` max

Relevant code:

- `internal/config/constants.go`
- `internal/models/email.go`
- `internal/repository/pg_email.go`
- `internal/scheduler/campaign_scheduler.go`
- `internal/scheduler/email_scheduler.go`
- `internal/scheduler/warmup_scheduler.go`

### Recommended operational posture

Treat these as operational heuristics, not protocol guarantees:

- for a fresh or recently connected mailbox, start cold outreach around `10-20/day`
- ramp slowly until the mailbox proves stable
- use `30-50/day` as the normal safe band for most cold outreach mailboxes
- do not raise a mailbox above the default `50/day` casually
- anything above `50/day` per cold mailbox should require positive reputation signals, low complaint rates, and explicit review
- never jump a new mailbox directly to high volume
- preserve spacing between emails; avoid bursty send patterns from the same mailbox

Warmup posture:

- start around the repo default of `10/day`
- ramp gradually instead of doubling volume abruptly
- keep warmup and cold outreach budgets separate in reasoning

### Worker-level distribution rule

For shared workers, distribute volume by mailbox budget and IP spread:

- no shared worker should become a concentration point for a large fraction of total cold-email traffic
- prefer adding more workers and spreading accounts rather than increasing per-worker density
- if one worker holds many active cold mailboxes, keep the total planned worker volume equal to the sum of those mailbox caps, not an independent higher target
- as a conservative planning heuristic, shared workers should usually stay near the equivalent of about `10` actively sending cold mailboxes at default settings, or roughly `500` cold campaign emails/day, unless there is explicit evidence that the worker/IP pool can safely sustain more

Dedicated workers may carry higher organization-specific volume, but those increases should come from more healthy mailboxes, not from forcing a small number of inboxes to send too much.

### Internet research constraints

Current external guidance reinforces conservative limits:

- Google bulk sender guidance requires SPF, DKIM, DMARC alignment, one-click unsubscribe for marketing/subscribed mail, and says to keep spam rate below `0.10%` and avoid reaching `0.30%`
- Microsoft Exchange Online documents platform send limits, but also explicitly says customers sending legitimate bulk commercial email should use specialized third-party providers rather than treating Exchange Online as bulk-mail infrastructure

That means Warmbly should stay conservative by default:

- low complaint rate matters more than chasing maximum throughput
- low spam rate matters more than open-rate screenshots
- gradual warmup and distributed sending matter more than maximizing one mailbox or one worker

## Warmup Process

Warmup exists to build and maintain sender reputation by sending low-risk traffic gradually, spacing it out over time, and generating normal mailbox activity patterns instead of sudden bulk spikes.

### Current product behavior

Warmup is currently a paid-only feature at the product layer.

This is enforced in:

- `internal/app/feature/gate.go`
- `internal/tasks/email_task.go`

Important nuance:

- the database and repository model still support `free` and `premium` warmup pools
- the task flow currently blocks non-paid organizations from using warmup
- so the architecture supports pool separation, but product access is effectively premium-only right now

Treat that as the current truth unless product requirements change.

### How warmup works in this codebase

The warmup task flow currently does the following:

- checks that the mailbox's organization is allowed to use warmup
- chooses a partner mailbox from the configured warmup pool
- avoids selecting the same partner too frequently
- sometimes replies to an existing warmup thread based on `warmup_reply_rate`
- otherwise sends a new plaintext warmup message
- creates a warmup verification token
- sends the email through the assigned worker
- increments daily warmup stats
- schedules the next warmup task using gradual volume progression

Relevant code:

- `internal/tasks/email_task.go`
- `internal/scheduler/warmup_scheduler.go`
- `internal/repository/pg_warmup.go`
- `internal/infrastructure/db/migrations/000010_warmup_pools.up.sql`

### Pool behavior

Warmup pools are mailbox pools, not campaign lists.

The intent is:

- only other participating mailboxes are used as warmup recipients
- recipients can be blocked from the pool if their spam score or invalid-token behavior looks bad
- repeated pairings should be reduced
- warmup should look like low-volume natural traffic, not repetitive synthetic blasting

Pool safety signals in code include:

- recent-partner avoidance
- warmup token validation
- invalid-token attempt counting
- spam-score tracking
- auto-blocking from pools

### Recommended paid-pool policy

For paid warmup pools:

- only use warmed, valid, monitored mailboxes as participants
- do not mix in trial, temporary, or low-quality inboxes just to inflate pool size
- keep volume gradual and spaced
- maintain conversational behavior, including some replies, instead of only one-way sends
- keep warmup running even after campaigns begin, rather than stopping immediately once a mailbox is "ready"

### Internet research summary

Current provider guidance and deliverability references support the same shape:

- warmup means gradual volume growth over days or weeks, not instant scale
- start with low volume, then increase only while performance stays healthy
- use authenticated domains and keep complaint/spam signals low
- shared pools can help smaller senders, while higher sustained volume may justify dedicated IPs or dedicated pools

Concrete external guidance:

- Postmark describes domain warmup as slowly and steadily increasing volume over a period of weeks, often reaching stable behavior in `3-6 weeks`
- Mailgun describes IP warmup as gradually increasing email volume from an IP to let mailbox providers observe behavior and build reputation
- Mailgun also notes that shared IPs do not need dedicated IP warmup in the same way, while dedicated IPs do

### Practical interpretation for Warmbly

For Warmbly, the safest interpretation is:

- warmup should be gradual per mailbox
- pool quality matters more than pool size
- paid warmup pools should remain isolated from lower-trust traffic
- dedicated-worker customers may still participate in premium warmup logic, but their sending reputation should be evaluated mailbox-by-mailbox, not assumed safe just because they have isolated infrastructure

## Fraud And Abuse Detection

Warmbly does not currently appear to rely on one centralized ML fraud engine.

Instead, the codebase uses layered abuse controls and trust signals across auth, API usage, warmup behavior, tracking, and mailbox sync.

### Main anti-abuse layers

- CAPTCHA on auth-sensitive entry points
- per-user API rate limiting
- WebSocket rate limiting
- warmup-token verification and invalid-attempt tracking
- warmup spam-score tracking and auto-blocking from pools
- tracking-event deduplication and replay resistance
- deliverability-event idempotency and suppression lists
- worker-side sync abuse detection
- admin ban and manual override controls

### Auth and signup protection

Authentication flows use Cloudflare Turnstile:

- login
- registration
- password reset
- confirmation flows

The Turnstile verifier also checks:

- remote IP format
- optional expected hostname
- challenge freshness to reduce replay risk

Relevant code:

- `internal/pkg/captcha/turnstile.go`
- `internal/app/auth/login.go`
- `internal/app/auth/registration.go`
- `internal/app/auth/reset_password.go`

### API and realtime throttling

The backend applies user-level rate limiting by category, backed by Redis and plan/user limits.

The realtime service separately rate-limits:

- websocket joins
- websocket messages
- websocket events

This is not just performance protection; it is also an anti-abuse boundary against automated flooding and noisy clients.

Relevant code:

- `internal/api/middleware/ratelimit.go`
- `internal/app/ratelimit/service.go`
- `realtime/lib/realtime/rate_limiter.ex`

### Warmup fraud detection

Warmup has the clearest explicit abuse-detection path in the repo.

Signals used:

- every warmup email carries a verification token
- invalid token format is recorded
- missing, expired, or mismatched tokens are treated as suspicious
- invalid token attempts are counted over time
- spam score is accumulated for abusive or suspicious behavior
- accounts can be auto-blocked from warmup pools

Current auto-block thresholds in code:

- `>= 3` invalid warmup-token attempts in `24h`
- spam score `> 50`

Relevant code:

- `internal/app/consumer/event_new_email.go`
- `internal/repository/pg_warmup.go`
- `internal/infrastructure/db/migrations/000010_warmup_pools.up.sql`

### Paid pool protection policy

Protecting shared paid-pool reputation is more important than maximizing access for one risky mailbox.

Do not wait for an inbox to reach an extreme failure state before acting.

Important:

- `80%` spam placement is not a sensible block threshold
- if a mailbox is landing in spam `80%` of the time, it has already become dangerous to the shared pool
- action should happen much earlier

Use separate metrics for separate failure modes:

- user complaint rate: recipients explicitly mark mail as spam
- spam-folder placement rate: warmup or seed observations indicate messages are landing in junk/spam
- bounce rate: especially hard bounces
- suspicious warmup-token behavior
- mailbox-sync abuse and provider throttling

Recommended internal policy for shared paid pools:

- start evaluating after a minimum sample size
- suggested sample floor for spam placement: at least `20` warmup deliveries in the last `7 days`
- suggested sample floor for complaints: at least `100` delivered emails in the last `30 days`

Suggested automatic actions:

- warning band:
  spam-folder placement `>= 10%` over the last `20+` warmup deliveries
  or complaint rate `>= 0.03%`
  Action: lower warmup volume, increase spacing, increase monitoring

- quarantine band:
  spam-folder placement `>= 20%`
  or complaint rate `>= 0.10%`
  or bounce rate `>= 5%`
  or repeated suspicious warmup-token failures
  Action: immediately remove mailbox from the shared paid warmup pool for `7 days`

- hard block band:
  spam-folder placement `>= 40%`
  or complaint rate `>= 0.30%`
  or bounce rate `>= 10%`
  or clear abuse indicators such as token forgery patterns or repeated spam flags
  Action: block mailbox from shared paid pool for `30 days` and require review before re-entry

- catastrophic band:
  spam-folder placement `>= 80%`
  Action: immediate long-duration block and full reputation reset workflow; do not allow the mailbox back into the shared paid pool automatically

These thresholds are intentionally stricter than the point where large providers start penalizing senders, because shared warmup pools should act before provider-level enforcement hits the IP reputation.

### What should happen when a paid-pool mailbox is quarantined

When a mailbox breaches the quarantine or hard-block band:

- it should not be selected as a warmup sender
- it should not be selected as a warmup recipient
- it should not continue using the shared paid warmup pool
- campaign sending should be throttled or paused if the same mailbox is also used for cold outreach

Best option:

- move it to a separate recovery state or recovery pool that is isolated from the main paid pool

If a recovery pool does not exist yet:

- block warmup access entirely until the cooldown expires and the mailbox requalifies

### Re-entry requirements

Do not automatically restore a blocked mailbox just because time elapsed.

Require the mailbox to pass re-entry checks such as:

- authentication still healthy: SPF, DKIM, DMARC, PTR where relevant
- no recent provider complaints or hard-bounce spikes
- no recent invalid warmup-token attempts
- spam-folder placement back below `10%` on a fresh probation sample
- gradual re-entry with low volume, for example `5-10/day` warmup at first

### External guidance behind these thresholds

As of `April 3, 2026`, the strongest official guidance I found supports acting early:

- Google says senders should keep user-reported spam rate below `0.1%` and avoid ever reaching `0.3%`
- Amazon SES says for best results keep complaint rate below `0.1%`; at `0.1%` SES automatically places the account under review, and at `0.5%` SES may pause sending
- Amazon SES also says to keep bounce rate below `5%`; at `5%` the account can be placed under review, and at `10%` sending may be paused

That means a shared paid warmup pool should be stricter than mailbox-provider enforcement, not looser.

### Recommended implementation shape

For this repo, the most practical implementation is:

- compute rolling mailbox health daily and on every relevant event
- maintain a mailbox health state such as:
  `healthy`, `watch`, `throttled`, `quarantined`, `blocked`
- store `blocked_until`, `health_reason`, `last_health_score`, and `last_health_evaluated_at`
- feed the score from:
  warmup spam flags
  deliverability complaints
  bounce events
  invalid warmup-token attempts
  provider rate-limit or abuse signals
- make pool selection exclude any mailbox not in `healthy`
- keep positive engagement as a weak positive signal only; it should not instantly offset complaints or spam placement

### Best product decision

If the main goal is protecting your IPs, the best default is:

- shared paid pool: strict automatic quarantine
- dedicated infrastructure: allow separate recovery handling if you want, but not on the shared paid pool
- never let a risky mailbox continue warming in the same reputation surface that healthy paying customers depend on

### Worker-side abuse detection

Workers also defend against mailbox-sync abuse.

Current sync protections in code:

- burst sync limit: `100` new emails per `5 minutes`
- hourly sync limit: `500` new emails per hour
- if exceeded, the account is rate-limited, a warning/error event is emitted, and the mailbox can be terminated or set inactive upstream

Relevant code:

- `internal/app/worker/wmail/ratelimit.go`
- `internal/app/consumer/event_email_error.go`

### Event replay and duplicate protection

Several parts of the system defend against replayed or duplicated events:

- tracking service keeps an in-memory dedupe cache keyed by task/IP or task/URL/IP
- tracking consumer keeps a persistent dedupe table as a second line of defense
- deliverability events use an idempotency key
- Stripe webhooks also use idempotency logging

Relevant code:

- `tracking/src/handlers.rs`
- `internal/repository/pg_tracking_dedupe.go`
- `internal/app/consumer/event_tracking.go`
- `internal/app/advanced/service.go`
- `internal/repository/pg_advanced_outreach.go`
- `internal/repository/pg_subscription.go`

### Suppression as abuse containment

Some fraud and abuse prevention is expressed as containment rather than user banning.

Examples:

- recipients are suppressed after bounce, complaint, or unsubscribe signals
- suspicious warmup participants are blocked from pools
- rate-limited mailboxes can be disabled
- campaigns skip suppressed recipients automatically

This is operationally important because the safest response is often to stop further traffic rather than keep sending and collect more negative signals.

### Admin enforcement

The admin surface supports manual enforcement and overrides:

- ban user
- unban user
- inspect ban history
- inspect and override rate limits

Relevant code:

- `internal/api/routes.go`
- `internal/api/handler/admin.go`
- `internal/app/admin/service.go`
- `internal/infrastructure/db/migrations/000016_admin_system.up.sql`

### Practical interpretation

When extending anti-fraud logic in this repo:

- prefer layered controls over one brittle gate
- prefer idempotency and dedupe wherever external events arrive
- prefer blocking or suppressing risky traffic early
- keep worker-side abuse checks lightweight and infrastructure-backed
- record enough structured evidence for admin review when a user or mailbox is blocked

## Control Plane vs Execution Plane

Prefer this split:

- backend/consumer own relational state and business workflows
- workers execute side effects: send, sync, validate, heartbeat
- assignment and migration decisions belong in the control plane
- workers should remain replaceable and horizontally scalable

If a new feature requires heavy joins, admin queries, billing checks, or complex campaign state transitions, it probably belongs in backend or consumer, not in the worker.

## Practical Guidance

- do not add direct Postgres usage to `cmd/worker` or `internal/app/worker` unless explicitly required
- preserve worker-specific Kafka topic routing
- preserve separation between free, premium, and dedicated worker capacity
- preserve separation between free and premium warmup pools
- optimize for many-worker deployments, not a single giant worker
- document any change that alters worker assignment, pool membership, or network boundaries

## Source Anchors

These files are the fastest way to rebuild context:

- `README.md`
- `resources/architecture.md`
- `cmd/worker/main.go`
- `internal/app/worker/assignment.go`
- `internal/tasks/email_task.go`
- `internal/repository/pg_worker.go`
- `internal/repository/pg_warmup.go`
