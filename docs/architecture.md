# Architecture

Warmbly is a microservices-based email warmup and cold outreach platform.

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Frontend (React)                               │
└─────────────────────────────────────────────────────────────────────────────┘
        │                           │                           │
        │ REST API                  │ WebSocket                 │ Tracking
        v                           v                           v
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│  Backend API    │       │    Realtime     │       │    Tracking     │
│  Go, :8080      │       │  Elixir, :4000  │       │  Rust, :3000    │
└────────┬────────┘       └────────┬────────┘       └────────┬────────┘
         │                         │                         │
         │                  Google Pub/Sub                   │
         │                         │                         │
         └─────────────────────────┼─────────────────────────┘
                                   │
                          ┌────────v────────┐
                          │     Kafka       │
                          │ Schema Registry │
                          └────────┬────────┘
                                   │
         ┌─────────────────────────┼─────────────────────────┐
         │                         │                         │
         v                         v                         v
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│    Consumer     │       │     Worker      │       │   Databases     │
│       Go        │       │       Go        │       │                 │
└─────────────────┘       └─────────────────┘       └─────────────────┘
```

## Services

### Backend API (Go)

The main API server handling authentication, business logic, and orchestration.

- **Port**: 8080
- **Framework**: Gin
- **Responsibilities**:
  - User authentication (Google, Apple OAuth)
  - Email account management
  - Campaign creation and management
  - Subscription and billing (Stripe)
  - API for frontend

### Consumer (Go)

Kafka consumer processing events from various sources.

- **Responsibilities**:
  - Process tracking events (opens, clicks)
  - Update analytics in Cassandra
  - Trigger realtime notifications

### Worker (Go)

Distributed worker for email operations. Runs as a DaemonSet (1 per node) in Kubernetes.

- **Responsibilities**:
  - Send emails via Gmail API or SMTP
  - Sync inboxes via Gmail API or IMAP
  - Execute warmup schedules
  - Report results back via Kafka

### Tracking (Rust)

High-performance tracking service for email opens and link clicks.

- **Port**: 3000
- **Framework**: Axum
- **Responsibilities**:
  - Serve 1x1 tracking pixels
  - Handle link redirects
  - Publish events to Kafka

### Realtime (Elixir)

WebSocket gateway for real-time frontend updates.

- **Port**: 4000
- **Framework**: Phoenix Channels
- **Responsibilities**:
  - WebSocket connections from frontend
  - Subscribe to Google Pub/Sub
  - Push events to connected clients

## Data Stores

| Store | Purpose |
|-------|---------|
| PostgreSQL | Primary database (users, accounts, campaigns) |
| Redis | Caching, rate limiting, session storage |
| Cassandra (Astra) | Time-series analytics data |
| DynamoDB | Key-value lookups |
| S3 | Email bodies (EMSG format) |

## Communication Patterns

### REST API

Frontend communicates with Backend via REST API with JWT authentication.

### Kafka

Services communicate asynchronously via Kafka topics with Avro serialization.

### Google Pub/Sub

Backend publishes realtime events to Pub/Sub, consumed by Realtime service.

### WebSocket

Frontend maintains persistent WebSocket connection to Realtime service for live updates.

## Security

- JWT tokens for API authentication
- OAuth2 for email provider connections
- Encrypted token storage in database
- RSA encryption for inter-service communication

## Deployment

- **Local**: Docker Compose
- **Production**: Kubernetes with Kustomize overlays
- **Secrets**: AWS Secrets Manager via External Secrets Operator

See [deploy/README.md](../deploy/README.md) for deployment details.
