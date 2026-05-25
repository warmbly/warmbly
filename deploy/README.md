# Warmbly Deployment

Two distinct planes, deployed differently.

| Plane | Services | How |
|-------|----------|-----|
| Control | backend, consumer, tracking, realtime, web | Container hosting in one region (Railway in production). Stable region-pinning so KMS/DynamoDB/S3 calls stay local. |
| Execution | worker | One process per VPS, anywhere with a public IPv4. Managed from the admin dashboard over SSH. |

## Directory layout

```
deploy/
├── docker/
│   ├── backend.Dockerfile
│   ├── consumer.Dockerfile
│   ├── worker.Dockerfile
│   └── realtime.Dockerfile
└── config/
    └── env.example
```

The tracking Dockerfile lives at `tracking/Dockerfile`. The local-dev compose is `docker-compose.yml` at the repo root.

## Building images

```bash
docker build -f deploy/docker/backend.Dockerfile  -t warmbly/backend  .
docker build -f deploy/docker/consumer.Dockerfile -t warmbly/consumer .
docker build -f deploy/docker/worker.Dockerfile   -t warmbly/worker   .
docker build -f deploy/docker/realtime.Dockerfile -t warmbly/realtime .
docker build -f tracking/Dockerfile               -t warmbly/tracking tracking/
```

GitHub Actions publishes these to GHCR automatically. See [../resources/cicd.md](../resources/cicd.md).

## Local development

```bash
make infra  # postgres, redis, kafka, etc. (leave running, shared across worktrees)
make app    # backend, consumer, worker, tracking, realtime, web (hot reload)
make sim    # adds premium + dedicated workers (prod-image flow)
make seed   # rich fixture
make tools  # kafka-ui at :18090
make reset  # nuke volumes
```

Full reference: [../resources/local-development.md](../resources/local-development.md).

## Deploying the control plane

The Dockerfiles in `deploy/docker/` are the deployment unit. Production runs on Railway. Other valid targets: Fly.io, ECS Fargate, single-VPS systemd. Migrations run automatically on backend boot.

Configuration is env-driven — see `deploy/config/env.example` for the full env reference, or [../resources/deployment-guide.md](../resources/deployment-guide.md) for a step-by-step.

## Worker deployment

Workers run on per-VPS machines so cold-mail traffic spreads across many IPs. Worker identity is a deterministic UUIDv5 derived from the VPS's public IPv4 — same IP, same worker.

Add a worker from the admin dashboard:

1. Provision a VPS, note its public IP + root user
2. Admin → Workers → Add Worker
3. Paste the generated SSH public key into the VPS's `~/.ssh/authorized_keys`
4. Click Test, then Install

The backend SSHes in, uploads `scripts/install-worker.sh`, configures systemd, and starts the worker container. From then on, all lifecycle operations (restart, update, system updates, reboot, rotate keys, logs, uninstall) happen from the dashboard.

Manual install on the VPS is also supported:

```bash
sudo bash scripts/install-worker.sh \
  --kafka kafka.example.com:9092 \
  --schema-registry https://schema.example.com \
  --redis redis://cache.example.com:6379 \
  --aws-region us-east-1 --aws-key ... --aws-secret ...
```

### Why per-VPS instead of Kubernetes DaemonSet

Cold-mail reputation lives at the IP level. K8s nodes typically NAT pods through a small set of egress IPs, so a per-node DaemonSet does not deliver IP diversity. Workers don't depend on Postgres, so cluster-level service discovery isn't needed. Spreading across VPS providers and regions is the only thing that actually moves the deliverability needle.

### Worker env reference

Workers in production should be assigned to a worker profile in the dashboard. The profile bundles all of these:

| Env var | Source | Notes |
|---------|--------|-------|
| `APP_ENV` | profile | `prod` selects `alias/master-key`; otherwise `alias/master-key-dev` |
| `AWS_REGION` | profile (via AWS credentials row) | |
| `AWS_ACCESS_KEY_ID` | profile (via AWS credentials row) | |
| `AWS_SECRET_ACCESS_KEY` | profile (via AWS credentials row) | encrypted at rest |
| `KAFKA_BOOTSTRAP_SERVERS` | profile | |
| `KAFKA_SASL_USERNAME` | profile | |
| `KAFKA_SASL_PASSWORD` | profile | encrypted at rest |
| `SCHEMA_REGISTRY_URL` | profile | |
| `SCHEMA_REGISTRY_KEY` | profile | |
| `SCHEMA_REGISTRY_SECRET` | profile | encrypted at rest |
| `REDIS` | profile | full URL with embedded password; encrypted at rest |
| `WORKER_TIER` | (worker row) | `shared` or `dedicated` |

The worker does **not** open a Postgres connection. Do not add one.

## Auto-update

Each worker profile picks a release channel (`pinned` / `stable` / `dev`) and an `auto_update` toggle. When a GitHub release fires the webhook, the backend resolves the channel and (if `auto_update=true`) rolls every assigned worker. See [../resources/cicd.md](../resources/cicd.md#deploying-workers).

## Health checks

```bash
curl http://localhost:8080/health    # backend
curl http://localhost:3000/health   # tracking
curl http://localhost:4000/health   # realtime
```

## Documentation

- [Local Development](../resources/local-development.md)
- [Deployment Guide](../resources/deployment-guide.md)
- [Architecture](../resources/architecture.md)
- [CI/CD](../resources/cicd.md)
