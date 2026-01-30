# Warmbly

Email warmup and cold email platform.

## Overview

Warmbly is a microservices-based platform for email warmup and cold outreach. It supports Gmail (via API) and IMAP/SMTP providers, with real-time event streaming, distributed workers, and comprehensive tracking.

## Architecture

```
                                    ┌─────────────────┐
                                    │   Frontend      │
                                    │   (React)       │
                                    └────────┬────────┘
                                             │
              ┌──────────────────────────────┼──────────────────────────────┐
              │                              │                              │
              v                              v                              v
    ┌─────────────────┐           ┌─────────────────┐           ┌─────────────────┐
    │  Backend API    │           │    Realtime     │           │    Tracking     │
    │  (Go, :8080)    │           │ (Elixir, :4000) │           │  (Rust, :3000)  │
    └────────┬────────┘           └────────┬────────┘           └────────┬────────┘
             │                             │                             │
             │                    Google Pub/Sub                         │
             │                             │                             │
             ├─────────────────────────────┼─────────────────────────────┤
             │                             │                             │
             v                             v                             v
    ┌─────────────────────────────────────────────────────────────────────────────┐
    │                                  Kafka                                      │
    └─────────────────────────────────────────────────────────────────────────────┘
             │                             │                             │
             v                             v                             v
    ┌─────────────────┐           ┌─────────────────┐           ┌─────────────────┐
    │    Consumer     │           │     Worker      │           │   PostgreSQL    │
    │      (Go)       │           │      (Go)       │           │   Redis, etc.   │
    └─────────────────┘           └─────────────────┘           └─────────────────┘
```

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
- Schema Registry: http://localhost:8081
- PostgreSQL: localhost:5432
- Redis: localhost:6379
- Kafka: localhost:9092

## Project Structure

```
warmbly/
├── cmd/
│   ├── backend/          # Backend API entrypoint
│   ├── consumer/         # Kafka consumer entrypoint
│   └── worker/           # Worker entrypoint
├── internal/
│   ├── api/              # HTTP handlers
│   ├── app/              # Application services
│   ├── config/           # Configuration loading
│   ├── events/           # Kafka event schemas
│   ├── infrastructure/   # Database, cache, queue clients
│   ├── models/           # Domain models
│   ├── pkg/              # Internal packages (emsg, crypto, etc.)
│   ├── repository/       # Data access layer
│   └── tasks/            # Background task processing
├── tracking/             # Rust tracking service
├── realtime/             # Elixir WebSocket service
├── deploy/
│   ├── docker/           # Docker Compose and Dockerfiles
│   └── kubernetes/       # Kubernetes manifests
└── docs/                 # Technical documentation
```

## Documentation

- [Deployment Guide](docs/deployment-guide.md) - Step-by-step deployment instructions
- [CI/CD Pipeline](docs/cicd.md) - CI/CD technical details
- [Architecture](docs/architecture.md) - System architecture overview
- [Events](docs/Events.md) - Kafka event system
- [EMSG Format](docs/EMSG.md) - Email message blob format
- [Gmail Integration](docs/gmail.md) - Gmail API usage
- [IMAP Integration](docs/imap.md) - IMAP/SMTP integration

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

```
PR Created → CI (tests) → Merge to main → Build images → Deploy
```

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yml` | PR/Push | Tests, linting, security scan |
| `build-push.yml` | Push to main | Build & push Docker images |
| `release.yml` | Tag `v*.*.*` | Build release images (triggers prod deploy) |

## License

Licensed under the **Apache License 2.0**.

Copyright 2026 Mindroot Ltd

Full license: [LICENSE](./LICENSE)
