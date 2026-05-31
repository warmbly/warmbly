<p align="center">
  <img src="docs/public/banner.svg" alt="Warmbly" width="100%" />
</p>

<p align="center">
  Open-source cold email and mailbox warmup you can self-host.<br />
  Your IPs, your database, your infrastructure — no vendor lock-in.
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-0369a1?style=flat-square&labelColor=0c4a6e" alt="License: Apache 2.0" /></a>
  &nbsp;<img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&labelColor=0c4a6e&logo=go&logoColor=white" alt="Go 1.25" />
  &nbsp;<img src="https://img.shields.io/badge/PostgreSQL-16-336791?style=flat-square&labelColor=0c4a6e&logo=postgresql&logoColor=white" alt="PostgreSQL 16" />
  &nbsp;<img src="https://img.shields.io/badge/Self--hostable-yes-10b981?style=flat-square&labelColor=0c4a6e" alt="Self-hostable" />
  &nbsp;<a href="./CONTRIBUTING.md"><img src="https://img.shields.io/badge/PRs-welcome-0ea5e9?style=flat-square&labelColor=0c4a6e" alt="PRs welcome" /></a>
</p>

<p align="center">
  <a href="#features">Features</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#self-hosting">Self-hosting</a> ·
  <a href="#documentation">Docs</a> ·
  <a href="./CONTRIBUTING.md">Contributing</a>
</p>

---

## What is Warmbly

Warmbly is a cold-outreach and mailbox-warmup platform that runs on your own
infrastructure. Hosted services keep your sender reputation and your data in
someone else's IP pool and someone else's database; building it yourself is
months of plumbing. Warmbly is the middle path — the features you'd expect from
a SaaS, running on the boxes you control.

It runs on a single VPS for a small setup, or across a fleet of cheap servers
with many IPs per box. The same code handles both.

## Features

- **Self-hostable** — one binary per service. Postgres, Redis, and an event bus
  are the whole stack. No required calls to AWS, GCP, Stripe, or Cloudflare.
- **Distributed workers** — one worker per IP, many workers per box. Reputation
  is tracked per IP, not per machine. Multi-IP install in a single command.
- **Pluggable backends** — KMS, blob store, event bus, and codec are all chosen
  at deploy time: AWS or local AES, Kafka or NATS, S3 or filesystem.
- **Real warmup** — pool-based warmup with spam-score tracking and auto-blocking
  on token-forgery patterns. Free and premium pools stay isolated.
- **Mailbox-first safety** — per-mailbox send caps and spacing, gradual warmup
  ramps. A worker's safe volume is the sum of its mailboxes' budgets, not a flat
  per-worker limit.
- **Envelope encryption** — KMS-wrapped per-user data keys. Workers fetch them
  over HTTPS and never touch Postgres directly.
- **Admin UI** — a separate React app for workers, sending identities, mailboxes,
  warmup pools, audit log, and backend selection.
- **Real-time tracking** — open/click pixel and redirect service with
  deduplication at both the tracker and the consumer, resistant to replays.

## Quick start

You'll need Docker, Go 1.25, and pnpm. The fastest path brings infra up in
Docker and the app services up against it:

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly

make infra   # Postgres, Redis, Kafka + supporting services (run once, leave up)
make app     # backend, consumer, worker, tracking, realtime, dashboard

open http://localhost:5173
```

For the fastest dev loop, run the Go services natively instead of in Docker
(no image rebuilds on code changes):

```bash
make infra            # once
make run              # backend + consumer + worker in one terminal
make web              # dashboard dev server → http://localhost:5173
```

`make run` connects to infra on the same machine by default. To point it at
infra hosted on another box, pass `INFRA_HOST`:

```bash
make run INFRA_HOST=192.168.1.50
```

The admin app and marketing site run on demand from their own terminals:

```bash
make admin   # → http://localhost:5174
make site    # → http://localhost:4321
```

The admin app needs an account with admin permissions. There's no way to
bootstrap the first admin from the UI, so sign up through the dashboard, then
promote yourself from the host:

```bash
make grant-admin EMAIL=you@example.com               # super (all permissions)
make grant-admin EMAIL=you@example.com ROLE=support  # support | ops | analyst
make revoke-admin EMAIL=you@example.com
```

For production, see [Self-hosting](#self-hosting).

## Architecture

Warmbly splits cleanly into a control plane and an execution plane.

The **control plane** is the backend API, the event consumer, Postgres, Redis,
and the event bus. It owns all stateful data and decides what to send and where.

The **execution plane** is the distributed worker fleet — one Go binary per
machine, one worker process per IP. Workers receive commands over the event bus,
fetch their encryption keys over HTTPS, send and sync mail, and emit telemetry
back. **Workers never connect to Postgres.**

That separation is the point: workers scale horizontally across many cheap
machines, each one a sending identity rather than a database client, so outbound
volume spreads across many IPs instead of concentrating in one runtime. The full
write-up is in [resources/architecture.md](resources/architecture.md).

## Self-hosting

Every external dependency has an open-source path, selected by an environment
variable. A self-hoster pays only for the boxes they rent.

| Concern            | Self-host default          | Cloud option           |
|--------------------|----------------------------|-------------------------|
| Database           | PostgreSQL 16              | RDS / Cloud SQL         |
| Cache              | Redis (or Valkey)          | ElastiCache             |
| Event bus          | NATS JetStream (1 binary)  | Kafka, MSK              |
| Blob storage       | Filesystem                 | S3, MinIO, R2, B2       |
| KMS / root key     | Local AES master key       | AWS KMS, Vault, GCP     |
| Encrypted keys     | PostgreSQL table           | DynamoDB / Scylla       |
| Codec              | JSON                       | Avro + Schema Registry  |
| Captcha            | Bypass token (trusted)     | Cloudflare Turnstile    |
| Payments           | Off                        | Stripe                  |

A minimal self-hosted backend env:

```bash
KMS_PROVIDER=local
KMS_LOCAL_MASTER_KEY=$(openssl rand -base64 32)

ENCRYPTED_KEYS_PROVIDER=postgres
INTERNAL_API_TOKEN=$(openssl rand -base64 32)

BLOB_PROVIDER=filesystem
BLOB_FS_ROOT=/var/lib/warmbly/blobs

EVENTBUS_PROVIDER=nats
NATS_URL=nats://localhost:4222

CODEC_PROVIDER=json
```

Workers add three variables so they read encryption keys over HTTPS instead of
from a database:

```bash
ENCRYPTED_KEYS_PROVIDER=http
ENCRYPTED_KEYS_BACKEND_URL=https://api.yourdomain.com
ENCRYPTED_KEYS_WORKER_TOKEN=<same value as INTERNAL_API_TOKEN>
```

### Multi-IP workers

One machine with many attached IPs becomes many sending identities with a single
command. Each IP gets its own systemd unit and a deterministic identity, so
reputation persists across reinstalls:

```bash
sudo ./scripts/install-worker.sh \
  --kafka kafka.yourdomain.com:9092 \
  --redis redis://cache.yourdomain.com:6379 \
  --ips 5.6.7.11,5.6.7.12,5.6.7.13,5.6.7.14
```

Full runbook: [docs/MULTI_IP_WORKERS.md](docs/MULTI_IP_WORKERS.md).

For the complete dependency audit and how to replace each cloud service, see
[docs/VENDOR_LOCKIN.md](docs/VENDOR_LOCKIN.md).

## Stack

| Component   | Tech                                   |
|-------------|----------------------------------------|
| Backend API | Go 1.25 + Gin                          |
| Consumer    | Go (event-bus driven)                  |
| Worker      | Go (Kafka / NATS subscriber)           |
| Tracking    | Rust + Axum                            |
| Realtime    | Elixir + Phoenix Channels              |
| Dashboard   | React 19 + Vite + Tailwind v4          |
| Admin UI    | React 19 + Vite + Tailwind v4          |
| Database    | PostgreSQL 16                          |
| Cache       | Redis 7 (or Valkey / KeyDB)            |
| Event bus   | NATS JetStream (default) or Kafka      |

## Project layout

```
cmd/
  backend/      REST API + admin orchestration
  consumer/     event-bus consumer → Postgres
  worker/       distributed sender (one per IP)
  seed/         local-dev fixtures
internal/
  api/          HTTP handlers, routes, middleware
  app/          business services (auth, email, campaign, ...)
  client/       SMTP/IMAP client + per-egress bind-IP
  events/       publisher + event schemas
  infrastructure/  pluggable codec, eventbus, kms, storage, encryptedkeys
  models/       domain types
  repository/   Postgres data access
tracking/       Rust open/click service
realtime/       Elixir WebSocket gateway
web/            user dashboard (Vite + React)
admin/          admin UI (Vite + React)
scripts/        worker installer and dev tooling
docs/           operator docs (auth, lock-in, multi-IP)
resources/      architecture and design notes
```

## Building and testing

```bash
# build
go build ./cmd/...
cd tracking && cargo build --release
cd realtime && mix deps.get && mix release
cd web      && pnpm install && pnpm build
cd admin    && pnpm install && pnpm build

# test
go test ./...
cd web      && pnpm typecheck && pnpm lint
cd tracking && cargo test
cd realtime && mix test
```

## Documentation

| Doc | What it covers |
|-----|----------------|
| [docs/VENDOR_LOCKIN.md](docs/VENDOR_LOCKIN.md) | Every external dependency and how to replace it |
| [docs/INTERNAL_API_AUTH.md](docs/INTERNAL_API_AUTH.md) | How workers authenticate to the backend |
| [docs/MULTI_IP_WORKERS.md](docs/MULTI_IP_WORKERS.md) | Many-IP worker deployment recipe |
| [resources/architecture.md](resources/architecture.md) | Control plane vs execution plane, encryption model |
| [resources/local-development.md](resources/local-development.md) | Docker Compose, profiles, seeding |
| [resources/deployment-guide.md](resources/deployment-guide.md) | Production control plane + worker fleet |
| [resources/Events.md](resources/Events.md) | Event bus reference |
| [resources/EMSG.md](resources/EMSG.md) | Encrypted-message blob format |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contribution guidelines and local checks |

## Security

Found a vulnerability? Email `security@warmbly.com` rather than opening a public
issue. We prefer responsible disclosure and credit reporters in the release
notes. The encryption model is documented in
[resources/architecture.md](resources/architecture.md) and the internal-API auth
model in [docs/INTERNAL_API_AUTH.md](docs/INTERNAL_API_AUTH.md).

## License

Apache License 2.0. Copyright 2026 Mindroot Ltd. See [LICENSE](./LICENSE).
