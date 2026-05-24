# Warmbly Agent Notes

## Purpose

Warmbly is an email warmup and cold outreach platform.

At a product level, the app does four main things:

- manages sender accounts and their assignment to workers
- sends campaign and warmup mail through distributed workers
- syncs mailbox state back into the platform
- tracks opens, clicks, replies, suppression, and deliverability signals

The backend API is the control plane. Workers are the execution plane.

## System Shape

- `cmd/backend`: API and business orchestration
- `cmd/consumer`: consumes Kafka events and updates platform state
- `cmd/worker`: execution worker for send/sync operations
- `tracking/`: open and click tracking service
- `realtime/`: websocket fanout service
- `web/`: frontend

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
- workers may talk to infrastructure-style services that scale independently, such as S3, KMS, DynamoDB, and cache layers
- worker-local state should be minimal and disposable

Current code matches that intent in `cmd/worker/main.go`: the worker boots Kafka, Redis cache, KMS, DynamoDB, and S3 clients, but does not open a PostgreSQL connection.

When changing worker behavior, preserve that boundary unless there is a very strong reason not to.

## Encryption Model

Warmbly uses envelope encryption for application secrets and sensitive payloads.

High-level flow:

- AWS KMS is the root of trust
- each user gets a data encryption key (DEK)
- the plaintext DEK is used for application-layer encryption and decryption
- the encrypted DEK is stored, not the plaintext DEK
- decrypted DEKs are cached for reuse

Current implementation:

- KMS generates a 32-byte DEK for AES-256
- the encrypted DEK blob is base64-encoded and stored in DynamoDB
- the plaintext DEK is cached in Redis with a TTL
- encrypted fields are sealed with AES-GCM and then base64-encoded

Main code paths:

- `internal/app/cipher/cipher.go`
- `internal/app/cipher/encrypt.go`
- `internal/app/cipher/decrypt.go`
- `internal/app/cipher/cache.go`
- `internal/infrastructure/kms/encryption.go`
- `internal/infrastructure/kms/decryption.go`
- `internal/repository/dynamo_user_encrypted_keys.go`

Operational guidance:

- do not introduce plaintext storage of secrets or message content where the current design expects encrypted values
- do not move DEK storage into Postgres; keep it in the KMS + DynamoDB envelope-encryption model unless there is a migration plan
- if workers need access to encrypted payloads, prefer passing encrypted material plus access to KMS-backed decryption primitives rather than introducing direct SQL dependencies
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
