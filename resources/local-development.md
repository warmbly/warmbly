# Local Development Guide

This guide explains how to set up and run Warmbly locally for development using Docker Compose.

## Prerequisites

- **Docker** (20.10+) and **Docker Compose** (v2.0+)
- **Git**
- Optional for native development:
  - Go 1.25+
  - Rust (for tracking service)
  - Elixir 1.18+ (for realtime service)
  - Node.js 20+ (for frontend)

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/warmbly/warmbly.git
cd warmbly
```

### 2. Start Infrastructure Only

If you want to run services natively but need the infrastructure (database, cache, message queue):

```bash
cd deploy/docker

# Start only infrastructure services
docker compose up -d postgres redis zookeeper kafka schema-registry
```

Wait for all services to be healthy:

```bash
docker compose ps
```

### 3. Start All Services

To run everything in Docker:

```bash
cd deploy/docker
docker compose up
```

This will build and start:
- **postgres** - PostgreSQL 16 database
- **redis** - Redis 7 cache
- **zookeeper** - Kafka coordination
- **kafka** - Message queue
- **schema-registry** - Avro schema registry
- **mailpit** - Local email catcher (SMTP + web UI)
- **backend** - Go API server
- **consumer** - Kafka event consumer
- **worker** - Distributed worker
- **tracking** - Rust tracking service
- **realtime** - Elixir WebSocket service

### 4. Run in Background

```bash
docker compose up -d
```

View logs:

```bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f backend
```

## Service URLs

Once running, services are available at:

| Service | URL | Description |
|---------|-----|-------------|
| Backend API | http://localhost:8080 | REST API |
| Tracking | http://localhost:3000 | Pixel/click tracking |
| Realtime | http://localhost:4000 | WebSocket gateway |
| Mailpit UI | http://localhost:8025 | Email inbox (catches all outbound emails) |
| Schema Registry | http://localhost:8081 | Avro schemas |
| PostgreSQL | localhost:5432 | Database |
| Redis | localhost:6379 | Cache |
| Kafka | localhost:9092 | Message queue |

## Database Setup

### Run Migrations

The backend service runs migrations automatically on startup. To run manually:

```bash
# Connect to the backend container
docker compose exec backend sh

# Run migrations (inside container)
go run cmd/migrate/main.go up
```

### Connect to Database

```bash
# Via psql
docker compose exec postgres psql -U warmbly -d warmbly_dev

# Or use any PostgreSQL client with:
# Host: localhost
# Port: 5432
# User: warmbly
# Password: warmbly
# Database: warmbly_dev
```

## Development Workflows

### Backend (Go)

Run natively with hot reload using air:

```bash
# Install air
go install github.com/cosmtrek/air@latest

# Start infrastructure
cd deploy/docker && docker compose up -d postgres redis kafka schema-registry

# Run backend with hot reload (from repo root)
cd ../..
air -c .air.toml
```

Or build and run manually:

```bash
# Set environment variables (see deploy/config/env.example)
export PRIMARY_DB="postgres://warmbly:warmbly@localhost:5432/warmbly_dev?sslmode=disable"
export REDIS="redis://localhost:6379"
# ... other vars

go run cmd/backend/main.go
```

### Frontend (React)

```bash
cd web
npm install
npm run dev
```

Frontend runs at http://localhost:5173 by default.

### Tracking Service (Rust)

```bash
cd tracking

# With cargo watch for hot reload
cargo install cargo-watch
cargo watch -x run

# Or build and run
cargo run
```

### Realtime Service (Elixir)

```bash
cd realtime
mix deps.get
mix phx.server
```

## Environment Variables

All environment variables are documented in `deploy/config/env.example`.

For local Docker development, the `docker-compose.yml` includes sensible defaults. Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_ENV` | dev | Environment mode |
| `PRIMARY_DB` | postgres://warmbly:warmbly@postgres:5432/warmbly_dev | PostgreSQL connection |
| `REDIS` | redis://redis:6379 | Redis connection |
| `KAFKA_BOOTSTRAP_SERVERS` | kafka:29092 | Kafka brokers |
| `AUTH_SECRET` | (auto-generated) | JWT signing secret |
| `SMTP_HOST` | mailpit | SMTP server for local email delivery |
| `SMTP_PORT` | 1025 | SMTP port (Mailpit) |

## Email in Local Development

The local stack includes [Mailpit](https://github.com/axllent/mailpit), a mail catcher that captures all outbound emails sent by the backend (login codes, registration codes, password resets, etc.).

- **Web UI:** http://localhost:8025 — view all captured emails
- **SMTP:** `mailpit:1025` (inside Docker) / `localhost:1025` (from host)

When `SMTP_HOST` is set, the backend uses a plain SMTP sender instead of AWS SES. This is automatic in docker-compose — no AWS credentials needed for local dev.

If you're running the backend natively (outside Docker), set the env vars to point at Mailpit on the host:

```bash
export SMTP_HOST=localhost
export SMTP_PORT=1025
```

## Email Templates

All email templates live in `internal/notify/templates/`. They share a base layout (`base.go`) with the sky/cloud theme, logo, and legal footer. Each template file defines only its content section.

### Run tests

```bash
go test ./internal/notify/templates/ -v
```

This verifies all templates render without errors, contain the expected content, include the required legal details (Companies Act 2006), and don't leak content across templates.

### Preview in browser

Generate the HTML and open it directly:

```bash
# Login code
go test ./internal/notify/templates/ -run TestGenerateLoginCodeHTML -v

# Or dump all three to files and open them:
go test ./internal/notify/templates/ -run TestPreview -v
```

To quickly preview a template, create a one-off test or use `go run`:

```bash
go run -exec 'open' <<'EOF'
//go:build ignore

package main

import (
    "os"
    "github.com/warmbly/warmbly/internal/notify/templates"
)

func main() {
    html, _ := templates.GenerateLoginCodeHTML("123456")
    os.WriteFile("/tmp/login-code.html", []byte(html), 0644)

    html, _ = templates.GenerateRegistrationCodeHTML("789012")
    os.WriteFile("/tmp/registration-code.html", []byte(html), 0644)

    html, _ = templates.GenerateResetPasswordHTML("", "https://app.warmbly.com/reset?token=abc123")
    os.WriteFile("/tmp/reset-password.html", []byte(html), 0644)
}
EOF

# Then open in browser:
open /tmp/login-code.html
open /tmp/registration-code.html
open /tmp/reset-password.html
```

### Preview via Mailpit (full flow)

Start the dev stack and trigger the auth flow — all emails are captured:

1. `docker compose -f deploy/docker/docker-compose.yml up`
2. Open http://localhost:8025 (Mailpit inbox)
3. Register, login, or reset password via the app
4. Emails appear in Mailpit in real time

### Editing templates

| File | Purpose |
|------|---------|
| `base.go` | Shared layout (sky theme, logo, card, legal footer), business constants |
| `login_code.go` | Login verification code email |
| `registration_code.go` | Registration verification code email |
| `reset_password.go` | Password reset email with button |

To change branding, legal details, or the footer — edit the constants at the top of `base.go`. To change the sky/card/cloud design — edit the `baseHTML` template in `base.go`. To change a specific email's content — edit that template's content const.

## Common Tasks

### Rebuild a Single Service

```bash
docker compose build backend
docker compose up -d backend
```

### Reset Database

```bash
# Stop services
docker compose down

# Remove postgres volume
docker volume rm docker_postgres_data

# Start fresh
docker compose up -d
```

### View Kafka Topics

```bash
# List topics
docker compose exec kafka kafka-topics --bootstrap-server localhost:9092 --list

# Consume messages from a topic
docker compose exec kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic tracking-events \
  --from-beginning
```

### Check Schema Registry

```bash
# List schemas
curl http://localhost:8081/subjects

# Get specific schema
curl http://localhost:8081/subjects/tracking-events-value/versions/latest
```

## Troubleshooting

### Services Not Starting

1. Check if ports are already in use:
   ```bash
   lsof -i :5432  # PostgreSQL
   lsof -i :6379  # Redis
   lsof -i :9092  # Kafka
   ```

2. Check Docker logs:
   ```bash
   docker compose logs kafka
   docker compose logs backend
   ```

### Kafka Connection Issues

Kafka needs time to start. If services fail to connect:

```bash
# Restart dependent services after Kafka is ready
docker compose restart backend consumer worker tracking
```

### Database Connection Refused

Ensure PostgreSQL is healthy before starting dependent services:

```bash
docker compose up -d postgres
docker compose exec postgres pg_isready -U warmbly
# Should output: "localhost:5432 - accepting connections"
docker compose up -d
```

### Schema Registry Errors

If you see schema compatibility errors:

```bash
# Delete and recreate schemas (development only!)
curl -X DELETE http://localhost:8081/subjects/tracking-events-value
```

## Stopping Services

```bash
# Stop all services
docker compose down

# Stop and remove volumes (full reset)
docker compose down -v
```

## Next Steps

- [Architecture Overview](architecture.md) - Understand the system design
- [Events Documentation](Events.md) - Kafka event system
- [Deployment Guide](deployment-guide.md) - Production deployment
