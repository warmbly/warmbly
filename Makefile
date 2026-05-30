GO_BIN := $(shell go env GOBIN)
ifeq ($(strip $(GO_BIN)),)
GO_BIN := $(shell go env GOPATH)/bin
endif

export PATH := $(GO_BIN):$(PATH)

# Pin the compose project name so every worktree (root + the ones
# under ~/.agentd/worktrees) targets the same stack instead of each
# directory spinning up its own postgres/kafka/redis. Only one
# worktree's app code can run at a time, but switching is cheap:
# `make app` from the new worktree re-runs the binaries in-place
# against warm caches; infra never restarts. Set via `-p` on every
# compose invocation rather than COMPOSE_PROJECT_NAME so it works in
# fresh clones without any environment setup.
COMPOSE := docker compose -p warmbly

GOLANGCI_LINT_VERSION ?= v1.64.8
PROTOC_GEN_GO_VERSION ?= v1.36.11
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.1

PROTO_DIR := internal/tasks/proto
PROTO_GEN_FILES := $(PROTO_DIR)/tasks.pb.go

# Infra services that back the natively-run Go processes. `make infra`
# starts just these in docker; the app code runs on the host.
INFRA_SERVICES := postgres redis kafka localstack

# Endpoints for host-run Go services. These mirror the docker-compose
# service env, but point at the host-side offset ports (postgres
# 15432, redis 16379, kafka 19092, localstack 14566) instead of the
# in-network hostnames (postgres:5432, redis:6379, ...).
DEV_DATABASE_URL := postgres://warmbly:warmbly@localhost:15432/warmbly?sslmode=disable

# Shared env for every native Go service (mode, redis, kafka, and the
# localstack-backed AWS/KMS/DynamoDB/S3 stack). Flattened to one line
# by make so it can be used as a command env-prefix.
GO_DEV_ENV := \
	WARMBLY_ENV=dev \
	REDIS_ADDR=localhost:16379 \
	KAFKA_BROKERS=localhost:19092 \
	AWS_REGION=us-east-1 \
	AWS_ACCESS_KEY_ID=test \
	AWS_SECRET_ACCESS_KEY=test \
	AWS_ENDPOINT_URL=http://localhost:14566 \
	KMS_KEY_ID=alias/warmbly-dek \
	DYNAMO_USER_KEYS_TABLE=warmbly_user_encrypted_keys \
	S3_BUCKET=warmbly-local

.PHONY: setup-tools fmt lint proto check-proto \
        up sim seed reset logs status stop down tools test-seed \
        restart restart-go restart-all \
        infra infra-down infra-logs \
        backend consumer worker app app-down dev \
        web admin site grant-admin revoke-admin

setup-tools:
	@echo "Installing required Go tools into $(GO_BIN)"
	GOBIN=$(GO_BIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN=$(GO_BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(GO_BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

# Format all Go code. CI's golangci-lint enforces gofmt, so run this
# before every commit. This is the formatting signal, not `go build`.
fmt:
	gofmt -w ./cmd ./internal

lint:
	golangci-lint run --timeout=5m

proto:
	@command -v protoc >/dev/null || (echo "protoc not found in PATH"; exit 1)
	@command -v protoc-gen-go >/dev/null || (echo "protoc-gen-go not found in PATH; run 'make setup-tools'"; exit 1)
	protoc --proto_path=$(PROTO_DIR) --go_out=$(PROTO_DIR) --go_opt=paths=source_relative $(PROTO_DIR)/*.proto

check-proto:
	@tmpdir=$$(mktemp -d); \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	command -v protoc >/dev/null || { echo "protoc not found in PATH"; exit 1; }; \
	command -v protoc-gen-go >/dev/null || { echo "protoc-gen-go not found in PATH; run 'make setup-tools'"; exit 1; }; \
	protoc --proto_path=$(PROTO_DIR) --go_out="$$tmpdir" --go_opt=paths=source_relative $(PROTO_DIR)/*.proto; \
	if ! cmp -s $(PROTO_GEN_FILES) "$$tmpdir/tasks.pb.go"; then \
		echo "Generated protobuf files are out of date. Run 'make proto' and commit the changes."; \
		exit 1; \
	fi

# ─── native dev (host-run Go, dockerized infra) ───────────────────
# The fast default: infra in docker, Go services on the host via
# `go run`. No docker image rebuilds on code change. Start infra once
# with `make infra`, then run services with `make backend` /
# `make app`. The backend auto-applies the embedded migrations on
# boot against the docker postgres.

# Start only the backing services (postgres, redis, kafka, localstack).
infra:
	$(COMPOSE) up -d $(INFRA_SERVICES)

# Stop the backing services (keeps volumes).
infra-down:
	$(COMPOSE) stop $(INFRA_SERVICES)

# Tail infra logs.
infra-logs:
	$(COMPOSE) logs -f $(INFRA_SERVICES)

# API server on :8080. Auto-applies embedded migrations on boot.
backend:
	$(GO_DEV_ENV) \
	DATABASE_URL=$(DEV_DATABASE_URL) \
	SERVER_ADDR=:8080 \
	TRACKING_BASE_URL=http://localhost:8081 \
	WEBSOCKET_URI=ws://localhost:8082/ws \
	TURNSTILE_SECRET=1x0000000000000000000000000000000AA \
	go run ./cmd/backend

# Kafka -> postgres consumer.
consumer:
	$(GO_DEV_ENV) \
	DATABASE_URL=$(DEV_DATABASE_URL) \
	KAFKA_CONSUMER_GROUP=warmbly-consumer \
	go run ./cmd/consumer

# Send/sync worker. No postgres by design (kafka + infra services only).
worker:
	$(GO_DEV_ENV) \
	WORKER_ID=worker-local-1 \
	WORKER_TIER=free \
	KAFKA_CONSUMER_GROUP=warmbly-worker \
	WORKER_HEARTBEAT_INTERVAL=15s \
	go run ./cmd/worker

# Run backend + consumer + worker together in one terminal. Ctrl-C
# stops all three (kill 0 takes down go run and its child binaries).
# Run `make infra` first. For a single service, run its target alone.
app:
	@echo "Running backend + consumer + worker natively (Ctrl-C stops all). Run 'make infra' first if infra is down."
	@trap 'kill 0' INT TERM; \
	$(MAKE) --no-print-directory backend & \
	$(MAKE) --no-print-directory consumer & \
	$(MAKE) --no-print-directory worker & \
	wait

# Best-effort stop for native Go services (use Ctrl-C on `make app`
# normally; this is a safety net for detached/orphaned processes).
app-down:
	-@pkill -f 'go run ./cmd/backend'  2>/dev/null || true
	-@pkill -f 'go run ./cmd/consumer' 2>/dev/null || true
	-@pkill -f 'go run ./cmd/worker'   2>/dev/null || true
	-@pkill -f 'exe/backend'  2>/dev/null || true
	-@pkill -f 'exe/consumer' 2>/dev/null || true
	-@pkill -f 'exe/worker'   2>/dev/null || true
	@echo "stopped native go services (best effort)"

# One-liner: bring up infra, then run the app stack natively.
dev: infra app

# ─── docker stack (release-image smoke tests, infra mgmt) ─────────

# Bring up the production-style stack (one worker, foreground). Runs
# the same images prod would, wired against local infra. Slower than
# native dev (rebuilds images); use for "does the release binary boot?".
up:
	$(COMPOSE) up

# Full simulation: infra + app + premium and dedicated workers.
sim:
	$(COMPOSE) --profile sim up

# Load rich fixture data. Requires the backend up (native `make backend`
# or `make up`) so migrations have run. `-T` disables TTY allocation
# (Make's shell isn't a tty; without -T compose can swallow stdout).
seed:
	$(COMPOSE) --profile seed run --rm -T seed

# One-shot: wipe volumes and start the docker stack fresh, then seed.
reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d
	@echo "waiting for backend to apply migrations..."
	@sleep 8
	$(COMPOSE) --profile seed run --rm -T seed

# Tail logs for all services (or pass S=backend for one).
logs:
	$(COMPOSE) logs -f $(S)

# Show container status.
status:
	$(COMPOSE) ps

# Spin up debugging UIs (kafka-ui).
tools:
	$(COMPOSE) --profile tools up -d kafka-ui
	@echo "kafka-ui: http://localhost:18090"

# Stop services, keep volumes.
stop:
	$(COMPOSE) stop

# Stop and remove containers + network (keeps named volumes).
down:
	$(COMPOSE) down

# Re-run the fixture loader against the running stack.
test-seed:
	$(COMPOSE) --profile seed run --rm -T seed

# Restart all docker services (infra + app), rebuilding app images.
restart:
	$(COMPOSE) up -d --build

# Restart only the Go app containers without touching infra. Rebuilds
# the images first so code changes are picked up.
restart-go:
	$(COMPOSE) up -d --build backend consumer worker worker-premium worker-dedicated

# Convenience: rebuild + restart everything (infra stays up if already
# running; only changed services are recreated).
restart-all:
	$(COMPOSE) up -d --build

# ─── admin / ops helpers ──────────────────────────────────────────

# Grant admin to a user by email: make grant-admin EMAIL=foo@bar.com
grant-admin:
	@test -n "$(EMAIL)" || { echo "usage: make grant-admin EMAIL=you@example.com"; exit 1; }
	$(COMPOSE) exec -T postgres psql -U warmbly -d warmbly -c \
		"UPDATE users SET is_admin = true WHERE email = '$(EMAIL)';"

# Revoke admin: make revoke-admin EMAIL=foo@bar.com
revoke-admin:
	@test -n "$(EMAIL)" || { echo "usage: make revoke-admin EMAIL=you@example.com"; exit 1; }
	$(COMPOSE) exec -T postgres psql -U warmbly -d warmbly -c \
		"UPDATE users SET is_admin = false WHERE email = '$(EMAIL)';"

# ─── frontend dev servers ─────────────────────────────────────────

# Dashboard (web/) on http://localhost:5173
web:
	cd web && pnpm dev

# Admin console (admin/) on http://localhost:5174
admin:
	cd admin && pnpm dev

# Marketing site (site/) on http://localhost:4321
site:
	cd site && pnpm dev
