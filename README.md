<p align="center">
  <img src="docs/assets/banner.png" alt="Warmbly" width="640" />
</p>

<h1 align="center">Warmbly</h1>

<p align="center">
  <strong>The open-source cold email and mailbox warmup platform you can actually self-host.</strong>
  <br />
  No AWS lock-in. Distributed senders. Multi-IP workers. Admin UI included.
</p>

<p align="center">
  <a href="#quick-start"><img src="https://img.shields.io/badge/get%20started-5_minutes-22c55e?style=flat-square" alt="Quick start" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License" /></a>
  <img src="https://img.shields.io/badge/go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/postgres-16-336791?style=flat-square&logo=postgresql&logoColor=white" alt="Postgres" />
  <img src="https://img.shields.io/badge/self--hostable-yes-22c55e?style=flat-square" alt="Self-hostable" />
  <img src="https://img.shields.io/badge/aws--required-no-22c55e?style=flat-square" alt="No AWS required" />
</p>

<p align="center">
  <a href="#features">Features</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#self-hosting">Self-hosting</a> ·
  <a href="#admin-ui">Admin UI</a> ·
  <a href="docs/VENDOR_LOCKIN.md">No lock-in</a>
</p>

<br />

<p align="center">
  <img src="docs/assets/dashboard-preview.png" alt="Warmbly dashboard preview" width="900" />
</p>

<br />

---

## Why Warmbly

Most cold email platforms force you into one of two corners. Hosted SaaS — fast
to start, but your sender reputation lives in someone else's IP pool, and your
data lives in someone else's database. Or roll-your-own — full control, six
months of plumbing before you send the first email.

Warmbly is the third option: a real cold-outreach platform that runs on **your
infrastructure, your IPs, your database**, without making you give up the
features you'd expect from a hosted SaaS.

You can run it on a single $5 VPS with SQLite-free Postgres. You can run it
across a Hetzner CX32 fleet with 16 IPs per box. The same code handles both.

## Features

<table>
<tr>
<td width="33%" valign="top">

### 🛡️ Self-hostable
Single binary per service. No required calls to AWS, GCP, Stripe, or
Cloudflare. Postgres + Redis + NATS is the whole infrastructure stack.

</td>
<td width="33%" valign="top">

### 🌐 Distributed workers
One worker per IP, many workers per VPS. Multi-IP install in one command.
Reputation per IP, not per VPS.

</td>
<td width="33%" valign="top">

### 🔌 Pluggable everything
Swap KMS, blob store, event bus, codec at deploy time. AWS or local AES.
Kafka or NATS JetStream. S3 or filesystem.

</td>
</tr>
<tr>
<td valign="top">

### 🎛️ Admin UI
Production-grade React + Vite admin app with workers, egresses,
mailboxes, warmup, audit, and settings.

</td>
<td valign="top">

### 🔐 Envelope encryption
KMS-wrapped per-user DEKs. Workers fetch them over HTTPS, never touch
Postgres directly. Constant-time bearer-token auth on internal endpoints.

</td>
<td valign="top">

### ✉️ Real warmup
Pool-based warmup with anti-abuse signals, spam-score tracking, and
auto-blocking on token-forgery patterns. Free vs premium pool isolation.

</td>
</tr>
<tr>
<td valign="top">

### 📊 Multi-tier worker fleet
Shared free, shared premium, dedicated per-org. Tier migrations with
mailbox rebalancing. Per-egress health bands.

</td>
<td valign="top">

### 🧪 Mailbox-first safety
Per-mailbox send caps (default 50/day), spacing (default 600s), warmup
ramp from 10 to 40/day. Worker volume = sum of mailbox budgets, not a
flat per-worker limit.

</td>
<td valign="top">

### 🛰️ Real-time tracking
Open/click pixel + redirect service (Rust), with deduplication at both
the tracker and the consumer level. Replay-resistant.

</td>
</tr>
</table>

<br />

## Quick start

The fastest path to a running stack — one VPS, one command, everything local:

```bash
# Clone, install deps, boot everything
git clone https://github.com/warmbly/warmbly && cd warmbly
make infra   # Postgres, Redis, NATS, Kafka, Schema Registry, MailHog (~1 min)
make app     # backend, consumer, worker, tracking, realtime, dashboard (~30s)

# Open the dashboard
open http://localhost:5173
```

The admin app and marketing site live outside the compose stack and run
on demand from their own terminals:

```bash
make admin   # Vite dev server  → http://localhost:5174
make site    # Astro dev server → http://localhost:4321
```

To open the admin app you need an account with `admin_permissions > 0`.
There is no way to bootstrap the first admin from inside the UI, so sign
up normally through the dashboard, then promote yourself from the host:

```bash
make grant-admin EMAIL=you@example.com               # super (all perms)
make grant-admin EMAIL=you@example.com ROLE=support  # support|ops|analyst
make revoke-admin EMAIL=you@example.com              # back to 0
```

That's it. Hot reload is wired for every language service (Go, Rust, Elixir,
React).

For production: see [Self-hosting](#self-hosting) below.

## Admin UI

The admin app lives at `admin/`, separate from the user dashboard, with a
clear amber accent so admins never confuse the two.

<p align="center">
  <img src="docs/assets/admin-preview.png" alt="Warmbly admin app" width="900" />
</p>

What it covers:

- **Overview** — fleet health, mailbox counts, send rates, bounce rates
- **Workers** — physical worker processes, SSH lifecycle (test / install / restart / upgrade / logs)
- **Egresses** — sending identities (worker × IP), health scores, quarantine
- **Mailboxes** — every connected mailbox across the platform
- **Warmup** — pools, participants, blocked accounts, appeals
- **Settings** — KMS / blob store / encrypted-keys / event bus / cache / transports — all selectable backends
- **Audit log** — every admin action with diffs

## Architecture

```
                    ┌─────────────────────────────────┐
                    │           Admin UI              │
                    │       (React + Vite app)        │
                    └────────────────┬────────────────┘
                                     │
                                     ▼
┌────────────────┐   HTTPS   ┌─────────────────────────────────────┐
│   Dashboard    │ ◄───────► │            Backend API              │
│    (React)     │           │           (Go, Gin, REST)           │
└────────────────┘           │                                     │
                             │  ┌──────────┐ ┌────────────────┐    │
                             │  │ Settings │ │ /api/v1/       │    │
                             │  │ Registrar│ │  internal/dek  │    │
                             │  └──────────┘ └────────┬───────┘    │
                             └──┬────────────────┬────┴────────────┘
                                │                │       │
                                ▼                ▼       │
                      ┌─────────────────┐  ┌──────────┐  │
                      │   PostgreSQL    │  │  Redis   │  │
                      │ users, accounts │  │  cache   │  │
                      │ DEKs (via PG)   │  │ ratelim  │  │
                      └─────────────────┘  └──────────┘  │
                                                         │
                  ┌──────────────────────────────────────┘
                  │
                  ▼  Kafka / NATS JetStream events
┌─────────────────────────────────────────────────────────────────┐
│                  Distributed worker fleet                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐         │
│  │ Worker 1 │  │ Worker 2 │  │ Worker 3 │  │ Worker N │  ...    │
│  │  IP A    │  │  IP B    │  │  IP C    │  │  IP X    │         │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘         │
│       │              │              │              │             │
│       └──────────────┴──────────────┴──────────────┘             │
│                            │                                     │
│                            ▼                                     │
│              Outbound SMTP / IMAP / OAuth APIs                   │
│            (Gmail, Microsoft 365, Zoho, custom)                  │
└─────────────────────────────────────────────────────────────────┘
```

**Control plane:** backend API + consumer + Postgres + Redis + event bus.
Decides what to send and where; owns all stateful data.

**Execution plane:** distributed worker fleet. One Go binary per VPS, one
worker process per IP. Workers receive commands over the event bus, fetch DEKs
over HTTPS, and emit telemetry back. **Workers never connect to Postgres.**

The split matters because workers are intended to scale horizontally across
many cheap VPSes — each one is a sending identity, not a database client.

## Self-hosting

Warmbly is designed so a self-hoster never has to pay AWS / GCP / Cloudflare /
Stripe for anything except the boxes they want to rent.

| Concern             | Self-host default        | Cloud option           |
|---------------------|--------------------------|------------------------|
| **Database**        | PostgreSQL 16            | RDS / Cloud SQL        |
| **Cache**           | Redis (or Valkey)        | ElastiCache            |
| **Event bus**       | NATS JetStream (1 binary) | Kafka, MSK             |
| **Blob storage**    | Filesystem               | S3, MinIO, R2, B2      |
| **KMS / root key**  | Local AES master key     | AWS KMS, Vault, GCP    |
| **Encrypted DEKs**  | PostgreSQL table         | DynamoDB / Scylla      |
| **Codec**           | JSON                     | Avro + Schema Registry |
| **Captcha**         | Bypass token (trusted)   | Cloudflare Turnstile   |
| **Payments**        | Off                      | Stripe                 |

Every adapter is selected via an env var. See [docs/VENDOR_LOCKIN.md](docs/VENDOR_LOCKIN.md)
for the honest audit of every external dependency and what to do about it.

### Minimum-viable env

```bash
# Self-hostable defaults
KMS_PROVIDER=local
KMS_LOCAL_MASTER_KEY=$(openssl rand -base64 32)

ENCRYPTED_KEYS_PROVIDER=postgres   # on the backend
INTERNAL_API_TOKEN=$(openssl rand -base64 32)

BLOB_PROVIDER=filesystem
BLOB_FS_ROOT=/var/lib/warmbly/blobs

EVENTBUS_PROVIDER=nats
NATS_URL=nats://localhost:4222

CODEC_PROVIDER=json
```

Workers get one extra:

```bash
ENCRYPTED_KEYS_PROVIDER=http
ENCRYPTED_KEYS_BACKEND_URL=https://api.yourdomain.com
ENCRYPTED_KEYS_WORKER_TOKEN=<same as INTERNAL_API_TOKEN>
```

### Multi-IP worker installation

One Hetzner CX32 with 16 attached Primary IPs becomes 16 sending identities
with one command:

```bash
sudo ./scripts/install-worker.sh \
  --kafka kafka.yourdomain.com:9092 \
  --redis redis://cache.yourdomain.com:6379 \
  --ips 5.6.7.11,5.6.7.12,5.6.7.13,5.6.7.14,5.6.7.15,\
5.6.7.16,5.6.7.17,5.6.7.18,5.6.7.19,5.6.7.20,5.6.7.21,\
5.6.7.22,5.6.7.23,5.6.7.24,5.6.7.25,5.6.7.26
```

Each IP gets its own systemd unit (`warmbly-worker@<dashed-ip>.service`) and
a deterministic UUIDv5 identity — reputation persists across reinstalls.
Full runbook in [docs/MULTI_IP_WORKERS.md](docs/MULTI_IP_WORKERS.md).

## Stack

<table>
<tr>
<th>Layer</th><th>Tech</th><th>Why</th>
</tr>
<tr>
<td>Backend API</td>
<td>Go 1.25 + Gin</td>
<td>Same binary on a Pi, an EC2 box, or a Hetzner dedicated</td>
</tr>
<tr>
<td>Consumer</td>
<td>Go (event-bus driven)</td>
<td>Same</td>
</tr>
<tr>
<td>Worker</td>
<td>Go (Kafka/NATS subscriber)</td>
<td>~50MB RAM per process; runs anywhere with port 25 open</td>
</tr>
<tr>
<td>Tracking</td>
<td>Rust + Axum</td>
<td>Open/click pixel hot path; dedup in-memory + persistent</td>
</tr>
<tr>
<td>Realtime</td>
<td>Elixir + Phoenix Channels</td>
<td>WebSocket fanout, naturally concurrent</td>
</tr>
<tr>
<td>Dashboard</td>
<td>React 19 + Vite + Tailwind v4 + shadcn</td>
<td>Modern stack, no Next.js coupling</td>
</tr>
<tr>
<td>Admin UI</td>
<td>React 19 + Vite + Tailwind v4 + shadcn</td>
<td>Same stack as dashboard; distinct visual identity</td>
</tr>
<tr>
<td>Primary DB</td>
<td>PostgreSQL 16</td>
<td>Boring, durable, well-known</td>
</tr>
<tr>
<td>Cache</td>
<td>Redis 7 (or Valkey / KeyDB)</td>
<td>Sliding-window rate limits + DEK cache</td>
</tr>
<tr>
<td>Event bus</td>
<td>NATS JetStream (default) or Kafka</td>
<td>NATS = single binary; Kafka = the historical hosted path</td>
</tr>
</table>

## Project layout

```
warmbly/
├── cmd/
│   ├── backend/        # REST API + admin orchestration
│   ├── consumer/       # event-bus consumer → Postgres
│   ├── worker/         # distributed sender (one per IP)
│   └── seed/           # local-dev fixtures
├── internal/
│   ├── api/            # HTTP handlers, routes, middleware
│   ├── app/            # business services (auth, email, campaign, ...)
│   ├── client/
│   │   ├── netbind/    # per-egress bind-IP for SMTP/IMAP
│   │   └── smtpimap/   # SMTP + IMAP client
│   ├── events/         # publisher + event schemas
│   ├── infrastructure/
│   │   ├── codec/      # Avro + JSON (pluggable)
│   │   ├── encryptedkeys/  # Postgres + DynamoDB + HTTP (pluggable)
│   │   ├── eventbus/   # Kafka + NATS JetStream (pluggable)
│   │   ├── kms/        # local + AWS (pluggable)
│   │   └── storage/    # filesystem + S3-compatible (pluggable)
│   ├── models/         # domain types
│   └── repository/     # Postgres data access
├── tracking/           # Rust open/click service
├── realtime/           # Elixir WebSocket gateway
├── web/                # User dashboard (Vite + React)
├── admin/          # Admin UI (Vite + React)
├── scripts/
│   └── install-worker.sh   # single-IP + multi-IP installer
├── docs/               # operator docs (auth, lock-in, multi-IP)
└── resources/          # technical / architectural docs
```

## Building

```bash
go build -o bin/backend  ./cmd/backend
go build -o bin/consumer ./cmd/consumer
go build -o bin/worker   ./cmd/worker

cd tracking    && cargo build --release
cd realtime    && mix deps.get && mix release
cd web         && pnpm install && pnpm build
cd admin   && pnpm install && pnpm build
```

## Testing

```bash
go test ./...
cd web         && pnpm typecheck && pnpm lint
cd admin   && pnpm typecheck && pnpm lint
cd tracking    && cargo test
cd realtime    && mix test
```

Coverage highlights:

- `internal/infrastructure/codec` — 11 tests (JSON round-trip + Avro interface conformance)
- `internal/infrastructure/encryptedkeys` — 14 tests (HTTP round-trip, factory, conflict semantics)
- `internal/infrastructure/eventbus` — 16 tests (Kafka + NATS round-trip + ack redelivery)
- `internal/infrastructure/kms` — 11 tests (local AES round-trip + tamper detection + factory)
- `internal/infrastructure/storage` — 14 tests (filesystem + traversal protection + factory)
- `internal/client/netbind` — 4 tests (dialer + TLS dialer + env fallback)
- `internal/api/middleware` — 5 internal-auth tests
- `internal/api/handler` — 9 internal-DEK handler tests
- `internal/app/settings` — 5 registrar tests
- `cmd/worker` — 5 UUID-from-IP derivation tests

Full suite runs in under 5 seconds.

## Documentation

| Doc | What it covers |
|---|---|
| [docs/VENDOR_LOCKIN.md](docs/VENDOR_LOCKIN.md) | Every external dependency and how to replace it |
| [docs/INTERNAL_API_AUTH.md](docs/INTERNAL_API_AUTH.md) | How workers authenticate to the backend |
| [docs/MULTI_IP_WORKERS.md](docs/MULTI_IP_WORKERS.md) | Hetzner CX32 + 16 Primary IPs deployment recipe |
| [resources/architecture.md](resources/architecture.md) | Control-plane vs execution-plane split, encryption model |
| [resources/local-development.md](resources/local-development.md) | Docker Compose, profiles, seeding |
| [resources/deployment-guide.md](resources/deployment-guide.md) | Production control plane + worker fleet |
| [resources/Events.md](resources/Events.md) | Event bus event reference |
| [resources/EMSG.md](resources/EMSG.md) | Encrypted-message blob format |

## Security

If you find a vulnerability please email `security@warmbly.com` rather than
opening a public issue. We prefer responsible disclosure and will credit you
in the release notes.

The encryption model is documented in
[resources/architecture.md](resources/architecture.md). The internal-API auth
model is in [docs/INTERNAL_API_AUTH.md](docs/INTERNAL_API_AUTH.md).

## Contributing

Bug reports and PRs are welcome. The codebase follows Go community conventions
(`gofmt`, `go vet`), TypeScript with the `web/` config, and Rust with
`cargo fmt`. Please run the full test suite before opening a PR.

For larger changes, open an issue first to discuss the approach. The
maintainers respond fastest to PRs that:

- Stay scoped to one logical change
- Keep the worker free of new direct-data-service dependencies (no Postgres
  in workers; route through `/api/v1/internal/*` instead)
- Add tests for new business logic
- Don't break self-hostability (any new external dependency must have an
  open-source path documented in `docs/VENDOR_LOCKIN.md`)

## License

Licensed under the **Apache License 2.0**. Copyright 2026 Mindroot Ltd.
See [LICENSE](./LICENSE) for the full text.

<br />

<p align="center">
  <sub>Built with the boring tools that actually work in production.</sub>
</p>
