# Vendor Lock-in Audit

Honest accounting of where Warmbly does and doesn't lock you into a specific
cloud / SaaS provider. Status as of the pluggable-backends refactor.

**TL;DR**

- **Workers: zero vendor lock-in.** A worker runs on any Linux box with
  outbound HTTPS + SMTP/IMAP. No AWS, no GCP, no Stripe SDK, nothing.
- **Backend: mostly self-hostable.** Six pluggable interfaces (KMS, encrypted
  keys, blob store, event bus, codec, cache) all ship with self-hostable
  implementations. Three remaining concerns still pin to specific vendors:
  task scheduling, captcha, and payments. All three are optional in the sense
  that you can run the platform with them stubbed; payments + captcha are only
  needed if you're running a public SaaS.

## The pluggable layer (no lock-in)

| Concern | Interface | Self-host default | AWS / cloud option | Other options |
|---|---|---|---|---|
| **KMS / root key** | `kms.Provider` | `local` (AES-256-GCM, key from env/file) | `aws-kms` | Add `vault`, `gcp-kms`, `azure-keyvault` by implementing the interface |
| **Encrypted DEKs** | `encryptedkeys.Store` | `postgres` (backend) / `http` (worker) | `dynamodb` | ScyllaDB Alternator via DynamoDB API; add `etcd` etc. |
| **Blob storage** | `storage.Store` | `filesystem` | `s3` (AWS) | One `s3` impl also works for MinIO, Cloudflare R2, Backblaze B2, Hetzner Object Storage — set `AWS_ENDPOINT_URL_S3` |
| **Event bus** | `eventbus.EventBus` | `nats` (JetStream) | `kafka` | Redis Streams, etc. by implementing the interface |
| **Codec** | `codec.Codec` | `json` | `avro` (requires Schema Registry) | Protobuf etc. |
| **Cache** | (`*cache.Cache`) | Redis (any Redis-compatible: Valkey, KeyDB) | Redis | Could be abstracted further if needed |
| **Internal HTTP auth** | bearer token | Self-issued via `openssl rand -base64 32` | n/a | JWT-per-worker is the planned follow-up |

Selection is per-process via env vars (`KMS_PROVIDER`, `BLOB_PROVIDER`,
`EVENTBUS_PROVIDER`, etc.). Every selector has a self-hostable default; AWS is
not the implicit fall-through except for KMS where the historical default is
preserved for the hosted Warmbly install.

See `docs/INTERNAL_API_AUTH.md` for the worker → backend auth model.

## What's still vendor-pinned

### 1. Task scheduling: Google Cloud Tasks

**What:** `internal/infrastructure/gtasks/gtasks.go` schedules deferred work
(send this email at 9am, retry this in 30 min, etc.).

**Why it matters:** Required by the backend if you want scheduled cold
campaigns and warmup pacing.

**Self-hostable today?** Partial. The local dev stack uses
`google/cloud-tasks-emulator` (open-source emulator) so you can run end-to-end
without GCP. The same emulator works in production if you self-host it
(a Java service from Google's own repo).

**True lock-in?** Yes for production hosted on cloud. A native replacement
would be:

- Embed a simple time-wheel scheduler in the backend using Postgres for state
- Use a job queue like River, Asynq, or Faktory
- Use Postgres `LISTEN/NOTIFY` + a polling worker

Any of these would be ~1 week of focused work. Not done because the existing
GCP emulator is a "good enough" self-host path for now.

### 2. Captcha: Cloudflare Turnstile

**What:** `internal/pkg/captcha/turnstile.go` protects signup / login /
password reset against bot abuse.

**Why it matters:** Only relevant for public SaaS where you accept signups
from the world. An internal-only Warmbly instance (e.g., one agency, one team)
doesn't need it.

**Self-hostable today?** Effectively yes if you set
`AUTH_TURNSTILE_BYPASS_TOKEN` to a value your forms always send — the captcha
check passes through. Suitable for trusted single-tenant deploys.

**True lock-in?** Soft. Cloudflare Turnstile is free and works against any
origin. If you genuinely need a captcha and don't want CF, replace it with
hCaptcha or a self-hosted alternative like
[ALTCHA](https://altcha.org/) — same shape, different SDK.

### 3. Payments: Stripe

**What:** `internal/app/stripe/service.go` and related — subscription plans,
billing portal, webhook handling.

**Why it matters:** Only relevant for SaaS that charges money. A self-hosted
Warmbly that's free or licensed by lump sum doesn't need it.

**Self-hostable today?** Yes by not configuring Stripe credentials — the
service no-ops on missing config and the relevant admin pages hide.

**True lock-in?** Soft. Same shape applies — swap Stripe for LemonSqueezy,
Paddle, or your own billing if you want. The repo doesn't go out of its way
to be Stripe-specific outside of `internal/app/stripe/`.

### 4. Secrets loader: AWS Secrets Manager / SSM

**What:** `cfg.LoadXxx()` calls in `internal/config/` pull config from AWS
Secrets Manager / SSM. Used for production hosted Warmbly so secrets don't
sit in env files.

**Self-hostable today?** Yes — all callers fall back to plain env vars when
the AWS lookup fails. Self-hosters set env vars; AWS-hosted reads from
Secrets Manager. Same code path, different source.

**True lock-in?** None for self-hosters. The AWS path exists, but it's not
required.

### 5. Geo IP database

**What:** `internal/infrastructure/geo/` loads a MaxMind-format GeoIP DB for
IP-based location lookups (admin views, abuse detection).

**Self-hostable today?** Yes. Operator drops a MaxMind GeoLite2 database
(free) at the configured path. Works fully offline.

**True lock-in?** None. Open data format, multiple sources.

### 6. Sentry (error reporting)

**Self-hostable today?** Yes. Sentry has a self-host offering. Set
`SENTRY_DSN` to point at it or leave it unset to disable.

**True lock-in?** None.

## What a fully self-hosted Warmbly looks like

Minimum viable stack (no clouds required):

```
┌────────────────────────────────────────────────────────────────┐
│ Single VPS or small fleet                                      │
│                                                                │
│ ┌──────────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│ │   Backend    │  │ Consumer │  │  Worker  │  │ Tracking │    │
│ │   (Go)       │  │  (Go)    │  │  (Go)    │  │  (Rust)  │    │
│ └──────┬───────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘    │
│        │               │              │              │         │
│        ├───────────────┴──────┬───────┘              │         │
│        │                      │                      │         │
│        ▼                      ▼                      ▼         │
│ ┌──────────────┐  ┌─────────────────┐  ┌─────────────────────┐│
│ │  PostgreSQL  │  │      NATS       │  │  Local filesystem   ││
│ │  (primary    │  │   JetStream     │  │  (blob store) +     ││
│ │   DB +       │  │   (event bus)   │  │   AES key file      ││
│ │   DEK store) │  │                 │  │   (local KMS)       ││
│ └──────────────┘  └─────────────────┘  └─────────────────────┘│
│                                                                │
│        ┌─────────────────┐                                     │
│        │      Redis      │  ← cache (any Redis-compatible)     │
│        └─────────────────┘                                     │
└────────────────────────────────────────────────────────────────┘

      Outbound: SMTP/IMAP to mailbox providers + HTTPS to AI APIs
      No required calls to AWS / GCP / Stripe / Cloudflare
```

Env-var configuration for this stack:

```bash
KMS_PROVIDER=local
KMS_LOCAL_MASTER_KEY=$(openssl rand -base64 32)

ENCRYPTED_KEYS_PROVIDER=postgres   # backend
ENCRYPTED_KEYS_PROVIDER=http       # worker
ENCRYPTED_KEYS_BACKEND_URL=https://api.yourdomain.com
ENCRYPTED_KEYS_WORKER_TOKEN=...    # same as INTERNAL_API_TOKEN
INTERNAL_API_TOKEN=$(openssl rand -base64 32)

BLOB_PROVIDER=filesystem
BLOB_FS_ROOT=/var/lib/warmbly/blobs

EVENTBUS_PROVIDER=nats
NATS_URL=nats://nats.local:4222

CODEC_PROVIDER=json   # if you don't want Schema Registry

# Optional / for SaaS only:
# STRIPE_SECRET_KEY=...
# AUTH_TURNSTILE_SECRET=...
# CLOUD_TASKS_QUEUE_NAME=...
```

## Status by component

| Component | Self-host ready? | Notes |
|---|---|---|
| Backend API (Go) | ✅ yes | Run on any Linux/Docker host |
| Consumer (Go) | ✅ yes | Same |
| Worker (Go) | ✅ yes | No DB, just outbound HTTPS + SMTP/IMAP |
| Tracking (Rust) | ✅ yes | Standalone HTTP service |
| Realtime (Elixir) | ✅ yes | Standalone Phoenix service |
| Admin UI (`admin/`) | ✅ yes | Vite build, deploy as static files |
| Dashboard (`web/`) | ✅ yes | Same |
| Postgres | ✅ yes | Any 14+ install |
| Redis | ✅ yes | Or Valkey, KeyDB |
| NATS JetStream | ✅ yes | Single binary, ~20MB |
| Kafka (optional) | ✅ yes | For Avro / Schema Registry preference |
| KMS | ✅ yes | Local impl; AWS/GCP/Vault by config |
| Blob store | ✅ yes | Filesystem; or any S3-compatible |
| Encrypted DEK store | ✅ yes | Postgres |
| Task scheduling | ⚠ partial | Needs Cloud Tasks emulator self-hosted |
| Captcha | ⚠ optional | Bypass-token mode for trusted deploys |
| Payments | ⚠ optional | Stripe — only if you charge |
| Sentry | ⚠ optional | Or self-hosted Sentry, or off |

The "⚠ partial / optional" rows are the only places a clean-room self-hoster
has to make a deliberate operational choice. Everything else just works.
