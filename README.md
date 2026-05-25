# Warmbly

Open-source email warmup and cold outreach platform.

## Overview

Warmbly is split into a **control plane** (API, consumer, tracking, realtime, web) and a **worker fleet** (distributed sender processes running on VPSes around the world). The control plane runs in one place; workers run on as many machines as you want so cold mail flows through many distinct IPs.

Workers are added and managed from the admin dashboard over SSH. Credentials are stored encrypted (KMS envelope encryption) and live-editable. Worker images can auto-update from GitHub Releases when a new tag is published.

The project is open source and self-hostable end-to-end.

## Architecture

The frontend (React) talks to three control-plane services: the **Backend API** (Go), **Realtime** (Elixir/Phoenix), and **Tracking** (Rust). All three publish events to **Kafka** (Avro + Schema Registry). The **Consumer** (Go) reads those events and updates Postgres state. **Workers** (Go) execute sends and mailbox sync, subscribing to per-worker Kafka topics; they never touch Postgres directly.

| Component | Technology |
|-----------|------------|
| Backend API | Go 1.25, Gin |
| Consumer | Go, Kafka consumer |
| Worker | Go, distributed across VPSes |
| Tracking | Rust, Axum |
| Realtime | Elixir 1.18, Phoenix Channels |
| Primary DB | PostgreSQL 16 |
| Cache | Redis 7 |
| Message bus | Kafka + Schema Registry |
| Object store | S3 (or compatible) |
| Encryption root | AWS KMS (or compatible) |
| Per-user secrets | DynamoDB (or compatible) |

## Services

| Service | Port (local) | Plane | Description |
|---------|------|-------|-------------|
| Backend | 8080 | control | REST API, auth, business logic, worker orchestration |
| Tracking | 3000 | control | Open/click pixel + redirect service |
| Realtime | 4000 | control | WebSocket gateway |
| Consumer | – | control | Kafka event processor |
| Worker | – | execution | SSH-managed sender process, one per VPS |
| Web | 5173 | – | Vite dev server (frontend) |

## Quick Start (local dev / simulation)

There's a single `docker-compose.yml` at the repo root that runs everything for local development, including LocalStack for KMS/DynamoDB/S3, stripe-mock, mailpit, and the optional kafka-ui debugger.

The stack is split into **infra** (postgres, redis, kafka, etc.: stateful, slow to start, identical across branches) and **app** (the language services you actually iterate on). Every `make` target pins `-p warmbly` as the compose project, so multiple git worktrees share the same infra and only the app containers churn per branch.

### Daily workflow

```bash
# 1. Once, from any worktree (usually root). Brings up postgres, redis,
#    zookeeper, kafka, schema-registry, mailpit, localstack, stripe-mock,
#    and cloud-tasks-emulator. Leave running.
make infra

# 2. In the worktree you're iterating on. Brings up backend, consumer,
#    worker, tracking, realtime, web with hot reload (air / cargo-watch
#    / Phoenix / Vite). Bind-mounted source means saves rebuild in place.
make app

# 3. Switching worktrees:
cd /path/to/other-worktree
make app          # recreates app containers against the new source;
                  # infra is untouched, caches stay warm.
```

### Stopping and logs

```bash
make app-logs     # stream logs from the hot-reload services
make logs         # stream logs from everything (infra + app)
make logs backend # one or more named services

make app-down     # stop app services, keep infra running
make infra-down   # stop infra too (volumes preserved)
make stop         # full teardown (everything, all profiles)
make reset        # nuke everything including volumes (start over)
```

### Other targets

```bash
make up           # production-style images (no hot reload); smoke test
                  # "does the release binary boot?"
make sim          # adds premium + dedicated workers (prod-image flow)
make seed         # load rich fixtures (3 orgs, 6 mailboxes, a campaign,
                  # suppressed contacts). Requires `make app` or `make up`.
make tools        # debugging UIs (kafka-ui at :18090)
make status       # docker compose ps
make restart <svc>    # rebuild + restart one service (prod-image flow only;
                      # under `make app` air handles this automatically)
make restart-go       # rebuild + restart all Go services (prod-image flow)
make restart-all      # rebuild + restart Go + Rust + Elixir (prod-image flow)
```

Service URLs (all ports offset to avoid clashes with locally-installed daemons):

- Backend API: http://localhost:8080
- Tracking: http://localhost:3000
- Realtime: http://localhost:4000
- Web: http://localhost:5173
- Mailpit: http://localhost:18025
- Kafka: localhost:9092
- Schema Registry: http://localhost:8081
- Postgres: localhost:15432
- Redis: localhost:16379
- LocalStack: http://localhost:4566
- stripe-mock: http://localhost:12111

See [resources/local-development.md](resources/local-development.md) for the full setup.

## Project Structure

- **`cmd/`** — service entrypoints: `backend/`, `consumer/`, `worker/`, `seed/`
- **`internal/`** — Go code:
  - `api/` — HTTP handlers and routes
  - `app/` — application services (auth, email, campaign, **worker_orchestrator**, **releases**, etc.)
  - `config/` — env-first config with optional AWS Secrets Manager
  - `events/` — Kafka schemas
  - `infrastructure/` — database, cache, queue, KMS, S3, Dynamo clients + SQL migrations
  - `models/` — domain types
  - `repository/` — data access
- **`tracking/`** — Rust tracking service
- **`realtime/`** — Elixir WebSocket service
- **`web/`** — React frontend (Vite + React Router + Tailwind)
- **`scripts/`** — VPS install script + LocalStack bootstrap
- **`deploy/docker/`** — Dockerfiles
- **`resources/`** — technical documentation

## Worker Deployment

Workers run on real machines so cold-mail traffic spreads across many IPs. There are two ways to bring one up:

**From the admin dashboard (recommended).** Admin opens `/app/admin/workers`, fills in the host + port + user, gets back an ed25519 public key. Admin pastes it into `~/.ssh/authorized_keys` on the VPS, clicks Test, then Install. The backend SSHes in, runs the installer, configures systemd, and starts the worker container. From then on, all lifecycle ops (restart, update, rotate keys, system updates, logs, reboot) happen from the dashboard.

**From the VPS itself.** Same installer, run by hand:

```bash
curl -fsSL https://get.warmbly.com/worker | sudo bash -s -- \
  --kafka kafka.example.com:9092 \
  --schema-registry https://schema.example.com \
  --redis redis://cache.example.com:6379 \
  --aws-region us-east-1 --aws-key ... --aws-secret ...
```

Worker identity is derived deterministically from the VPS's public IPv4 (UUIDv5), so the same IP always resolves to the same worker. Reputation persists across reinstalls.

See [resources/deployment-guide.md](resources/deployment-guide.md) for the full flow.

## Credentials and Profiles

Workers don't carry hardcoded credentials. Two reusable entities, both editable from the dashboard:

- **AWS Credentials** — named keypair; secret encrypted via KMS-wrapped DEK.
- **Worker Profile** — bundles Kafka + Schema Registry + Redis + image tag + AWS reference. One profile, many workers.

Workers reference a profile. When you edit the profile, assigned workers show a "stale config" banner; one click rewrites `/etc/warmbly/worker.env` and restarts each one.

## Auto-Update from GitHub Releases

Each worker profile picks a release channel:

- `pinned` — manual image tag
- `stable` — latest non-prerelease GitHub Release
- `dev` — latest release (including prereleases)

When `auto_update` is on and a new release fires the webhook, the backend resolves the channel, updates each assigned worker over SSH (regenerates the systemd unit with the new image, pulls, restarts), and records the running version.

Push-driven, not poll-driven: one check on backend boot, then the GitHub webhook (`POST /webhooks/github/releases`, HMAC-validated). Admin can also click "Check now."

All configuration is env-driven so self-hosters can point at their own fork:

```
RELEASES_ENABLED=true
RELEASES_GITHUB_REPO=youruser/yourfork
RELEASES_WORKER_IMAGE_REPO=ghcr.io/youruser/yourfork/worker
RELEASES_WEBHOOK_SECRET=<shared secret>
RELEASES_GITHUB_TOKEN=<optional, raises API limits>
```

## System Updates

The dashboard can also run OS package upgrades on each VPS (apt / dnf / pacman / apk), detect whether a reboot is needed, and reboot on demand. Reboots are never automatic.

## Building

```bash
go build -o bin/backend  ./cmd/backend
go build -o bin/consumer ./cmd/consumer
go build -o bin/worker   ./cmd/worker

cd tracking && cargo build --release
cd realtime && mix deps.get && mix release
cd web      && pnpm install && pnpm build
```

## CI / Release Flow

GitHub Actions builds + pushes images to GHCR:

| Workflow | Trigger | Result |
|----------|---------|--------|
| `ci.yml` | PR/push | Tests, linting, security scan |
| `build-push.yml` | Push to `main` | `:{sha}` and `:dev` tags |
| `release.yml` | Tag `vX.Y.Z` | `:vX.Y.Z`, `:vX.Y`, `:vX`, `:prod` tags + GitHub Release |

The control plane (backend / consumer / tracking / realtime / web) auto-deploys via Railway. Workers update via the dashboard or auto-update flow described above — they're the only service that needs in-band update orchestration.

See [resources/cicd.md](resources/cicd.md).

## Documentation

- [Local Development](resources/local-development.md) — Docker Compose, profiles, seeding
- [Deployment Guide](resources/deployment-guide.md) — control plane + worker fleet
- [Architecture](resources/architecture.md) — control vs execution plane, encryption model
- [CI/CD](resources/cicd.md) — workflows, image tags, release channels
- [Events](resources/Events.md) — Kafka event reference
- [EMSG Format](resources/EMSG.md) — encrypted message blob format
- [Gmail Integration](resources/gmail.md)
- [IMAP Integration](resources/imap.md)

## License

Licensed under the **Apache License 2.0**. Copyright 2026 Mindroot Ltd. See [LICENSE](./LICENSE).
