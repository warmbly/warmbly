# Local Development

The whole stack runs locally via a single `docker-compose.yml` at the repo root. Profiles let you opt into heavier setups for simulation testing.

## Prerequisites

- Docker (20.10+) and Docker Compose v2
- Git

For native development (running services outside Docker against the containerized infra), also install:

- Go 1.25+
- Rust (for tracking)
- Elixir 1.18+ (for realtime)
- Node 22+ and pnpm (for web)

## Make targets

Day-one:

```bash
make infra      # postgres, redis, kafka, mailpit, localstack, etc. (leave running)
make app        # backend, consumer, worker, tracking, realtime, web (hot reload)
make sim        # adds premium + dedicated workers; full simulation (prod-image flow)
make seed       # load rich fixtures (3 orgs, 6 mailboxes, a campaign)
make tools      # debugging UIs (kafka-ui at :18090)
make reset      # nuke everything including volumes — start over
```

The split lets multiple git worktrees share the stateful stuff. `make infra`
is a "start once and forget"; every worktree uses the same project name
(`-p warmbly`), so a second worktree's `make app` recreates the language
containers in place against its source without touching infra.

Iterating on code:

```bash
# Hot reload is on by default under `make app`:
#   - Go saves       → air rebuilds the binary in-container (~2-5s)
#   - Rust saves     → cargo-watch rebuilds (~2-10s debug build)
#   - Elixir saves   → Phoenix reloads modules in-process
#   - Web saves      → Vite HMR (browser updates instantly)
#
# So normally you don't restart anything manually.

# Tail logs:
make app-logs                      # all hot-reload services
make logs                          # everything including infra
make logs backend                  # one service
make logs backend consumer         # multiple
```

If you're on the prod-image flow (`make up` / `make sim`) instead of `make app`,
binaries are baked into the image and you need `make restart <svc>` /
`make restart-go` / `make restart-all` to pick up code changes.

All targets shell out to `docker compose -p warmbly`. If you don't have Make, the equivalents are:

```bash
docker compose -p warmbly -f docker-compose.yml -f docker-compose.dev.yml up -d \
    postgres redis zookeeper kafka schema-registry mailpit \
    localstack stripe-mock cloud-tasks-emulator                # infra
docker compose -p warmbly -f docker-compose.yml -f docker-compose.dev.yml up -d --build \
    backend consumer worker-shared-1 tracking realtime web     # app
docker compose -p warmbly --profile sim up                     # sim
docker compose -p warmbly --profile seed run --rm seed         # seed
docker compose -p warmbly --profile tools up -d kafka-ui       # tools
docker compose -p warmbly --profile sim --profile seed --profile tools down -v   # reset
```

## What's running

After `make infra && make app`:

- **postgres**, **redis**, **zookeeper**, **kafka**, **schema-registry** — infra
- **localstack** — KMS + S3 emulation
- **stripe-mock** — Stripe API surrogate
- **mailpit** — SMTP catcher with a web UI
- **cloud-tasks-emulator** — Google Cloud Tasks surrogate
- **backend**, **consumer**, **tracking**, **realtime**, **web** — app services
- **worker-shared-1** — one worker bound to the shared profile

The `sim` profile adds two more workers (`worker-premium-1`, `worker-dedicated-1`) so you can exercise tier-based assignment, worker rebalancing, and per-pool routing.

## Service URLs

Mostly standard ports. A few are offset because their defaults conflict too often: Postgres `15432`, Redis `16379`, Mailpit UI `18025` and SMTP `11025`, kafka-ui `18090` (because 8080 is backend). Everything else uses its natural port. Override locally in a `docker-compose.override.yml` if you still hit a conflict.

| Service | URL |
|---------|-----|
| Backend API | http://localhost:8080 |
| Tracking | http://localhost:3000 |
| Realtime | http://localhost:4000 |
| Web (Vite dev) | http://localhost:5173 |
| Mailpit | http://localhost:18025 |
| Kafka | localhost:9092 |
| Schema Registry | http://localhost:8081 |
| Postgres | localhost:15432 |
| Redis | localhost:16379 |
| LocalStack | http://localhost:4566 |
| stripe-mock | http://localhost:12111 |
| Cloud Tasks emulator | http://localhost:8123 |
| kafka-ui (with `make tools`) | http://localhost:18090 |

## Database setup

The backend runs migrations automatically on boot (`internal/infrastructure/db/migrate.go`), so there's no separate migrate step. Migrations live in `internal/infrastructure/db/migrations/`.

## Seeding fixtures

`make seed` runs the seeder one-shot. It's idempotent — safe to re-run after schema changes.

Baseline (always loads):

| Field | Value |
|-------|-------|
| Email | `dev@warmbly.com` |
| Password | `password123` |

When `SEED_RICH=true` (default in `docker-compose.yml`), also loads:

- 3 orgs (Acme free, Beta pro, Gamma enterprise) each with their own owner user (password `password123`)
- 3 workers matching the `docker-compose.yml` hostnames (shared / premium / dedicated)
- 6 email accounts spread across workers, joined to the right warmup pools
- A Beta campaign with a 2-step sequence
- 10 contacts, 2 of them unsubscribed (exercises suppression behaviour)

## LocalStack

The `localstack` service provides KMS and S3 locally. A one-shot init container (`localstack-init`) creates everything Warmbly expects:

- KMS alias `alias/master-key-dev` for envelope encryption
- S3 bucket `main`

Backend, consumer, and workers point at LocalStack via `AWS_ENDPOINT_URL=http://localstack:4566`. Production deployments leave that var unset and hit real AWS.

## Connecting psql / Redis CLI

```bash
docker compose exec postgres psql -U warmbly -d warmbly_dev
docker compose exec redis redis-cli
```

External clients can use:

- Postgres: `localhost:15432` user `warmbly` password `warmbly` db `warmbly_dev`
- Redis: `localhost:16379`

## Running services natively

If you want hot reload, run a service natively and point it at the docker infra.

### Backend (Go)

```bash
make infra && make app  # in another terminal, leave running

# Install air for hot reload
go install github.com/cosmtrek/air@latest

# Point at containerized infra
export PRIMARY_DB="postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable"
export REDIS="redis://localhost:16379"
export KAFKA_BOOTSTRAP_SERVERS="localhost:9092"
export SCHEMA_REGISTRY_URL="http://localhost:8081"
export AWS_ENDPOINT_URL="http://localhost:4566"
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="test"
export AWS_SECRET_ACCESS_KEY="test"
# ... rest of env, see deploy/config/env.example

air -c .air.toml
```

### Web (React)

The web service in compose already mounts `./web` and runs `pnpm dev`. To run it locally instead:

```bash
docker compose stop web
cd web
pnpm install
pnpm dev
```

### Tracking (Rust)

```bash
docker compose stop tracking
cd tracking
cargo run
```

### Realtime (Elixir)

```bash
docker compose stop realtime
cd realtime
mix deps.get && mix phx.server
```

## Mailpit

All outbound mail is captured by Mailpit. The backend uses plain SMTP (`mailpit:1025`) in dev rather than SES, so no AWS credentials are needed.

- Web UI: http://localhost:18025
- SMTP from inside docker: `mailpit:1025`
- SMTP from host: `localhost:11025`

Note: Mailpit speaks SMTP only, not IMAP. The worker's IMAP sync path is not exercised by the default stack; for that, add a real IMAP server (e.g. GreenMail) to the stack.

## Email templates

Email templates live in `internal/notify/templates/`. Render tests:

```bash
go test ./internal/notify/templates/ -v
```

To preview templates in a browser, dump them to disk:

```bash
go test ./internal/notify/templates/ -run TestPreview -v
# Files land in the test temp dir, path printed in output
```

Or just trigger the auth flow in the running app and watch the email arrive in Mailpit.

## Common tasks

### Rebuild one service

```bash
make restart backend
```

### Reset Postgres only

```bash
docker compose stop postgres
docker volume rm warmbly_postgres_data
docker compose up -d postgres
```

### List Kafka topics

```bash
docker compose exec kafka kafka-topics --bootstrap-server localhost:29092 --list
```

### Consume a topic

```bash
docker compose exec kafka kafka-console-consumer \
  --bootstrap-server localhost:29092 \
  --topic tracking-events --from-beginning
```

Or use kafka-ui at http://localhost:18090 (`make tools`).

### Inspect schema registry

```bash
curl http://localhost:8081/subjects | jq
curl http://localhost:8081/subjects/tracking-events-value/versions/latest | jq
```

## Troubleshooting

**Port already in use** — `lsof -i :5432` (or whichever) to find what's holding it. The compose ports are offset on purpose; if you have a local Postgres on 5432 it shouldn't clash.

**Backend can't reach Kafka** — Kafka needs ~30s to fully start. Healthchecks gate the dependent services, so `docker compose up` should handle this. If you brought services up in a weird order, `docker compose restart backend consumer`.

**Schema registry "incompatible schema"** — happens if you tweaked an Avro schema and the registry already has an older version. In dev: `make reset` to nuke volumes.

**LocalStack init failed:** check `docker compose logs localstack-init`. Usually means LocalStack itself isn't ready yet; the dependency wait should handle it, but re-running `make infra` works.

## Next steps

- [Architecture](architecture.md) — control vs execution plane, encryption model
- [Deployment Guide](deployment-guide.md) — running in production
- [Events](Events.md) — Kafka event reference
