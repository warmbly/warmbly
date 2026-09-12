# Warmbly Deployment

Two distinct planes, deployed differently.

| Plane | Services | How |
|-------|----------|-----|
| Control | backend, consumer, tracking, realtime, web | Container hosting in one region (Railway in production). Stable region-pinning so KMS/S3 calls stay local. |
| Execution | worker | One process per machine, anywhere with outbound network. Joins with one command and pulls its work; nothing connects back to it. |

## Directory layout

```
deploy/
├── docker/
│   ├── backend.Dockerfile          # also builds the seed + migrate binaries
│   ├── consumer.Dockerfile
│   ├── worker.Dockerfile
│   ├── realtime.Dockerfile
│   ├── go-dev.Dockerfile           # hot-reload dev images (make app)
│   ├── rust-dev.Dockerfile
│   ├── elixir-dev.Dockerfile
│   └── air.toml
├── config/
│   └── env.example
├── systemd/                        # one unit per service, Docker-free install
│   └── warmbly-*.service
└── nginx/
    └── warmbly.conf                # static frontends + reverse proxies
```

The tracking Dockerfile lives at `tracking/Dockerfile`, and the frontends build from `web/Dockerfile` and `admin/Dockerfile` (nginx static builds with runtime config injection). The self-host compose is `docker-compose.yml` at the repo root.

## Building images

```bash
docker build -f deploy/docker/backend.Dockerfile  -t warmbly/backend  .
docker build -f deploy/docker/consumer.Dockerfile -t warmbly/consumer .
docker build -f deploy/docker/worker.Dockerfile   -t warmbly/worker   .
docker build -f deploy/docker/realtime.Dockerfile -t warmbly/realtime .
docker build -f tracking/Dockerfile               -t warmbly/tracking tracking/
docker build -f web/Dockerfile                    -t warmbly/web      web/
docker build -f admin/Dockerfile                  -t warmbly/admin    admin/
```

The default builds have no Kafka/Avro support; add `--build-arg GO_TAGS=kafka` (Go images) or `--build-arg CARGO_FEATURES=kafka` (tracking) to opt in. Those builds link librdkafka through cgo, which cannot cross-compile, so each architecture has to be built on a machine of that architecture.

CI already publishes them, so you rarely need to: `backend`, `consumer`, `worker`, and `tracking` each get a second tag with a `-kafka` suffix (`ghcr.io/warmbly/warmbly/backend:prod-kafka`).

GitHub Actions publishes these to GHCR automatically. See [the self-hosting guide](https://docs.warmbly.com/development/deployment-guide/).

## Local development

```bash
make dev    # one-command native dev stack (infra + migrations + seed + app)
make infra  # postgres, redis, nats, mailpit (leave running, shared across worktrees)
make app    # backend, consumer, worker, tracking, realtime, web, admin (hot reload, in Docker)
make seed   # rich fixtures
make reset  # nuke volumes
```

Full reference: [local development](https://docs.warmbly.com/development/local-development/).

## Deploying without Docker

`deploy/split-cloud/` holds the manifests for running the two planes on
different providers: the control plane on a container host, the bus and cache
and fleet on machines you own, and the database, root key and object store in a
cloud region. Its README lists what is in it.

`deploy/systemd/` holds one unit per service and `deploy/nginx/warmbly.conf` a site that serves the static frontends and proxies the API, websocket and tracking hosts. The step-by-step guide that uses them is [Deploying without Docker](https://docs.warmbly.com/development/bare-metal/).

## Deploying the control plane

The Dockerfiles in `deploy/docker/` are the deployment unit. Production runs on Railway. Other valid targets: Fly.io, ECS Fargate, single-VPS systemd. Migrations run automatically on backend boot.

Configuration is env-driven — see `deploy/config/env.example` for the full env reference, or [the self-hosting guide](https://docs.warmbly.com/development/deployment-guide/) for a step-by-step.

### Realtime transport

Backend, consumer, and the Elixir realtime service all pick their event transport from one flag, `PUBSUB_ENABLED`, so they cannot disagree:

- `PUBSUB_ENABLED=false` (default): events bridge over Redis (`REDIS_URL`). No GCP needed. This is the local-dev and simple self-host path.
- `PUBSUB_ENABLED=true`: events flow through Google Pub/Sub. Also set `GCP_PROJECT_ID` and `GOOGLE_APPLICATION_CREDENTIALS_JSON` on every service. The backend and consumer auto-provision the realtime topics and their `<topic>-sub` pull subscriptions on boot (idempotent), so there is no manual `gcloud` step. The service account needs `roles/pubsub.editor`.

Set the flag the same on all three services. A publisher on Pub/Sub with a subscriber on Redis silently drops every realtime event.

## Adding a machine

Every Warmbly process that runs on a machine you own is a node: a `worker` sends and syncs mail, a `consumer` processes events. Both share one registry (`fleet_nodes`), and both join the same way.

Nothing is ever pushed to a node. It joins with one command and heartbeats forever, and everything the control plane wants from it comes back in the heartbeat reply.

```bash
# On the instance
warmblyctl fleet join-token

# On the new machine (needs Docker, systemd and root)
curl -fsSL https://api.example.com/join.sh | sh -s -- \
  --url https://api.example.com \
  --token <join-token> \
  --role worker \
  --region eu-central
```

The join script is embedded in the backend and served at `GET /join.sh`, so a self-hosted fleet gets a script matching its own backend. It enrols the node, writes the config the control plane hands back to `/etc/warmbly/node.env`, installs a systemd service and an update timer, and starts the node container. `--dry-run` prints what it would write without touching the machine.

`--region` is optional and only feeds worker placement. Re-running the same command on the same machine re-joins it under the same identity, so it keeps its history and its mailboxes.

The machine needs no inbound port, no SSH key and no cloud account. There is no restart, logs or reboot action anywhere, because nothing reaches into a machine.

### Why per-VPS instead of Kubernetes DaemonSet

Workers don't depend on Postgres, so cluster-level service discovery isn't needed, and k8s pods churn while IPs do not. What a mailbox provider remembers is the address an account signs in from, so a mailbox whose client address changes every deploy collects sign-in risk challenges for nothing.

### Worker env reference

A node is not configured by hand. The join endpoint renders `/etc/warmbly/node.env` from the backend's own environment (`nodeEnvKeys` in `internal/api/handler/fleet_nodes.go`), so a node runs against exactly the infrastructure the control plane uses:

| Env var | Notes |
|---------|-------|
| `APP_ENV` | |
| `EVENTBUS_PROVIDER` / `NATS_URL` | `nats` on the default stack |
| `CODEC_PROVIDER` | `json` on the default stack |
| `REDIS` | full URL with embedded password |
| `KMS_PROVIDER` / `KMS_LOCAL_MASTER_KEY` / `KMS_KEY_ID` | envelope decryption |
| `CREDENTIALS_ENCRYPTION_KEY` | mailbox credentials, read without an org context |
| `BLOB_PROVIDER` / `BLOB_FS_ROOT` / `AWS_REGION` / `S3_BUCKET` | message bodies |
| `BOX_GOOGLE_*` / `BOX_OUTLOOK_*` | mailbox OAuth clients; needed for token refresh |
| `KAFKA_*` / `SCHEMA_REGISTRY_*` | Kafka path only |

Set on the node itself, not inherited: `WARMBLY_NODE_ID`, `WARMBLY_NODE_ROLE`, `WARMBLY_NODE_REGION`, and `WORKER_ID` (equal to the node id, so the placement row and the node row are the same machine).

`PRIMARY_DB` is deliberately absent. The worker does **not** open a Postgres connection; it reaches relational data through the backend's internal API and nothing else. Do not add one.

Two settings do not survive a fleet on their defaults. `BLOB_PROVIDER=filesystem` gives a remote worker no way to read the body the backend wrote, and a stock local install hands out `NATS_URL`/`REDIS`/`ENCRYPTED_KEYS_BACKEND_URL` values that only resolve on the instance host. Set reachable addresses and `BLOB_PROVIDER=s3` before adding a node off-host.

## Auto-update

`internal/app/releases` resolves the head of the configured channel and writes the tag to `admin_settings`; it updates nothing itself. The heartbeat reply carries `desired_version`, the node writes it to a file, and a systemd timer pulls and restarts, so the process being replaced is never the process doing the replacing. A per-node `pinned_version` holds a machine back for canarying.

The backend is deliberately excluded: it is what tells everyone else their version. See [the self-hosting guide](https://docs.warmbly.com/development/deployment-guide/).


## Health checks

```bash
curl http://localhost:8080/health    # backend
curl http://localhost:3000/health   # tracking
curl http://localhost:4000/health   # realtime
```

## Documentation

- [Local development](https://docs.warmbly.com/development/local-development/)
- [Self-hosting guide](https://docs.warmbly.com/development/deployment-guide/)
- [Architecture](https://docs.warmbly.com/development/architecture/)
