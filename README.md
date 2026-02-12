# Warmbly

Email warmup and cold email platform.

## Overview

Warmbly is a microservices-based platform for email warmup and cold outreach. It supports Gmail (via API) and IMAP/SMTP providers, with real-time event streaming, distributed workers, and comprehensive tracking.

## Architecture

The **Frontend** (React) connects to three backend services: the **Backend API** (Go, :8080), **Realtime** (Elixir/Phoenix, :4000), and **Tracking** (Rust, :3000).

All three services publish events to **Kafka** (with Avro/Schema Registry). The Realtime service also uses **Google Pub/Sub** for cross-node message fanout.

Downstream from Kafka, the **Consumer** (Go) processes tracking events and the **Worker** (Go, 1 per machine) handles email operations. Both read/write to **PostgreSQL**, **Redis**, and **Cassandra** (DataStax Astra).

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend API | Go 1.25, Gin |
| Consumer | Go, Kafka consumer |
| Worker | Go, distributed (1 per machine) |
| Tracking | Rust, Axum |
| Realtime | Elixir 1.18, Phoenix Channels |
| Message Queue | Confluent Kafka with Avro/Schema Registry |
| Primary Database | PostgreSQL 16 (AWS RDS) |
| Time-series | DataStax Astra (Cassandra) |
| Cache | Redis 7 |
| Object Storage | AWS S3 |
| Pub/Sub | Google Cloud Pub/Sub |
| Task Queue | Google Cloud Tasks |

## Services

| Service | Port | Description |
|---------|------|-------------|
| Backend | 8080 | REST API, authentication, business logic |
| Tracking | 3000 | Pixel tracking and click tracking |
| Realtime | 4000 | WebSocket gateway for real-time events |
| Consumer | - | Kafka event consumer, processes tracking events |
| Worker | - | Distributed worker for email operations |

## Quick Start (Development)

### Prerequisites

- Docker and Docker Compose
- Go 1.25+ (for local development)
- Rust (for tracking service)
- Elixir 1.18+ (for realtime service)

### Start with Docker Compose

```bash
cd deploy/docker

# Start infrastructure only (database, redis, kafka)
docker-compose up -d postgres redis kafka schema-registry

# Start all services
docker-compose up
```

### Service URLs (Local)

- Backend API: http://localhost:8080
- Tracking: http://localhost:3000
- Realtime: http://localhost:4000
- Mailpit (email inbox): http://localhost:8025
- Schema Registry: http://localhost:8081
- PostgreSQL: localhost:5432
- Redis: localhost:6379
- Kafka: localhost:9092

## Project Structure

- **`cmd/`** — Service entrypoints: `backend/`, `consumer/`, `worker/`
- **`internal/`** — Core Go code:
  - `api/` — HTTP handlers
  - `app/` — Application services (auth, email, campaign, etc.)
  - `config/` — Configuration loading (env vars + AWS Secrets Manager)
  - `events/` — Kafka event schemas
  - `infrastructure/` — Database, cache, and queue clients
  - `models/` — Domain models
  - `notify/` — Email notification service and templates
  - `pkg/` — Internal packages (emsg, crypto, etc.)
  - `repository/` — Data access layer
- **`tracking/`** — Rust tracking service (Axum)
- **`realtime/`** — Elixir WebSocket service (Phoenix Channels)
- **`web/`** — React frontend
- **`deploy/`** — Docker Compose, Dockerfiles, Kubernetes manifests
- **`resources/`** — Technical documentation

## Documentation

- [Local Development](resources/local-development.md) - Docker Compose setup for local development
- [Deployment Guide](resources/deployment-guide.md) - Step-by-step deployment instructions
- [CI/CD Pipeline](resources/cicd.md) - CI/CD technical details
- [Architecture](resources/architecture.md) - System architecture overview
- [Events](resources/Events.md) - Kafka event system
- [EMSG Format](resources/EMSG.md) - Email message blob format
- [Gmail Integration](resources/gmail.md) - Gmail API usage
- [IMAP Integration](resources/imap.md) - IMAP/SMTP integration

## Building

```bash
# Go services (from root)
go build -o bin/backend ./cmd/backend
go build -o bin/consumer ./cmd/consumer
go build -o bin/worker ./cmd/worker

# Tracking service
cd tracking && cargo build --release

# Realtime service
cd realtime && mix deps.get && mix release
```

## CI/CD Pipeline

Warmbly uses GitHub Actions for CI/builds and GitOps for deployments.

PRs trigger CI (tests, linting, security scan). Merging to main builds and pushes Docker images. Tagging a release (`v*.*.*`) triggers a production deploy via ArgoCD.

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yml` | PR/Push | Tests, linting, security scan |
| `build-push.yml` | Push to main | Build & push Docker images |
| `release.yml` | Tag `v*.*.*` | Build release images (triggers prod deploy) |

## License

Licensed under the **Apache License 2.0**.

Copyright 2026 Mindroot Ltd

Full license: [LICENSE](./LICENSE)
