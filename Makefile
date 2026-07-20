GO_BIN := $(shell go env GOBIN)
ifeq ($(strip $(GO_BIN)),)
GO_BIN := $(shell go env GOPATH)/bin
endif

export PATH := $(GO_BIN):$(PATH)

# Pin the compose project name so every worktree (root + the ones
# under ~/.agentd/worktrees) targets the same stack instead of each
# directory spinning up its own postgres/kafka/redis. Only one
# worktree's app code can run at a time, but switching is cheap:
# `make app` from the new worktree rebuilds the binaries in-place
# against warm caches; infra never restarts. Set via `-p` on every
# compose invocation rather than COMPOSE_PROJECT_NAME so it works in
# fresh clones without any environment setup.
COMPOSE := docker compose -p warmbly

GOLANGCI_LINT_VERSION ?= v1.64.8
PROTOC_GEN_GO_VERSION ?= v1.36.11
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.1

PROTO_DIR := internal/tasks/proto
PROTO_GEN_FILES := $(PROTO_DIR)/tasks.pb.go

.PHONY: setup-tools fmt lint proto check-proto \
        up seed seed-plan sandbox sandbox-seed sandbox-simulate reset logs status stop down test-seed \
        restart restart-go restart-all infra infra-down app app-down app-logs \
        backend consumer worker run dev tracking realtime web \
        admin site docs grant-admin revoke-admin gen-key

setup-tools:
	@echo "Installing required Go tools into $(GO_BIN)"
	GOBIN=$(GO_BIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN=$(GO_BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(GO_BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

# Format all Go code. CI's golangci-lint enforces gofmt, so this is the
# formatting signal to run before committing — not `go build`.
fmt:
	gofmt -w ./cmd ./internal

lint:
	$(GO_BIN)/golangci-lint run --timeout=5m

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

# ─── dev / simulation stack ─────────────────────────────────────────────

# One-command no-cloud self-host: the entire stack in Docker, no cloud account
# (no AWS/GCP/Stripe/Kafka). Builds the images (CGO-free, so fast) and starts
# everything detached. Dashboard :5173, admin :5174, API :8080.
up:
	@command -v docker >/dev/null || { echo "docker is required: https://docs.docker.com/get-docker/"; exit 1; }
	$(COMPOSE) up -d --build
	@echo ""
	@echo "Warmbly is starting (the first build compiles the images once)."
	@echo "  Dashboard: http://localhost:5173"
	@echo "  Admin:     http://localhost:5174"
	@echo "  API:       http://localhost:8080"
	@echo ""
	@echo "  Demo data: $(COMPOSE) --profile seed run --rm seed"
	@echo "  Logs:      make logs        Stop: make down"

# One-command demo. Seeds the "Sunrise Labs" showcase org (live mailboxes on
# mailpit + dovecot, active/paused/completed/draft campaigns, a warmup pool, and
# a full history + analytics dataset) and brings up the WHOLE platform plus a
# simulator that plays the internet: delivering captured mail into inboxes,
# opening pixels, clicking tracked links, and replying as the seeded contacts.
# Everything the dashboard shows is real product code; only the humans are faked.
#
#   make sandbox                  # the works; dashboard on :5173
#   make sandbox SEED=false       # keep existing data, just run the stack
#
# Ctrl-C stops the app; infra (incl. mailpit + dovecot) stays up. Log in with
# sandbox@warmbly.test / password123. Docs: /development/sandbox/.
SANDBOX_SVCS := postgres redis nats mailpit dovecot
sandbox:
	@command -v docker >/dev/null || { echo "docker is required: https://docs.docker.com/get-docker/"; exit 1; }
	@command -v go >/dev/null || { echo "go 1.25+ is required: https://go.dev/dl/"; exit 1; }
	@command -v pnpm >/dev/null || { echo "pnpm is required: https://pnpm.io/installation"; exit 1; }
	$(COMPOSE) up -d $(SANDBOX_SVCS)
	@echo "Waiting for infra (postgres, redis, nats, mailpit, dovecot)..."
	@until $(COMPOSE) exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; do sleep 1; done
	$(GO_DEV_ENV) go run ./cmd/migrate
	@if [ "$(SEED)" = "true" ]; then $(GO_DEV_ENV) go run ./cmd/sandbox -seed-only; fi
	@if [ ! -d web/node_modules ]; then echo "Installing web dependencies (first run)..."; cd web && pnpm install; fi
	@if [ ! -d admin/node_modules ]; then echo "Installing admin dependencies (first run)..."; cd admin && pnpm install; fi
	@echo "Starting realtime + tracking as containers (no host Elixir/cargo needed)..."
	@BACKEND_INTERNAL_URL=http://host.docker.internal:8080 $(COMPOSE) up -d --build realtime tracking
	@echo ""
	@echo "Sandbox up. Dashboard http://localhost:5173  Admin http://localhost:5174  Mailpit http://localhost:18025"
	@echo "Login: sandbox@warmbly.test / password123 (org: Sunrise Labs). Ctrl-C stops the app; infra stays up."
	@echo ""
	@trap 'kill 0' INT TERM; \
	$(MAKE) --no-print-directory backend & \
	$(MAKE) --no-print-directory consumer & \
	$(MAKE) --no-print-directory worker & \
	$(MAKE) --no-print-directory web & \
	$(MAKE) --no-print-directory admin & \
	$(MAKE) --no-print-directory sandbox-simulate & \
	wait

# The simulator on its own (started as part of `make sandbox`). Plays the
# internet against whatever the running stack has already sent into mailpit.
sandbox-simulate:
	$(GO_DEV_ENV) go run ./cmd/sandbox -simulate-only

# Seed (or reset) the sandbox org and exit - no simulator, no app services.
sandbox-seed:
	$(COMPOSE) up -d $(SANDBOX_SVCS)
	@until $(COMPOSE) exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; do sleep 1; done
	$(GO_DEV_ENV) go run ./cmd/migrate
	$(GO_DEV_ENV) go run ./cmd/sandbox -seed-only

# Load rich fixture data. Runs natively like the other dev services — the
# seeder only needs Postgres, so it does not depend on a (re)built docker
# backend image, just `make infra` plus migrations applied (`make migrate`,
# `make backend`, or `make run`). SEED_RICH/SEED_FULL match the old docker
# seed profile: baseline + 3 orgs/workers/mailboxes + plans, team users
# (incl. the admin@warmbly.local super-admin), CRM, and an API key.
seed:
	$(GO_DEV_ENV) SEED_RICH=true SEED_FULL=true go run ./cmd/seed

# Switch the seeded dev workspace between trial/paid plans without going
# through Stripe. Run after `make seed`.
#
#   make seed-plan PLAN=trial    # 14-day free trial
#   make seed-plan PLAN=starter
#   make seed-plan PLAN=pro
#   make seed-plan PLAN=enterprise
PLAN ?= trial
seed-plan:
	@case "$(PLAN)" in \
		trial) plan_id="00000000-0000-0000-0000-000000000001"; status="trialing"; stripe_sub=""; price="";; \
		starter) plan_id="00000000-0000-0000-0000-000000000110"; status="active"; stripe_sub="sub_seed_dev_starter"; price="price_starter_seed";; \
		pro) plan_id="00000000-0000-0000-0000-000000000120"; status="active"; stripe_sub="sub_seed_dev_pro"; price="price_pro_monthly_seed";; \
		enterprise) plan_id="00000000-0000-0000-0000-000000000130"; status="active"; stripe_sub="sub_seed_dev_enterprise"; price="price_enterprise_seed";; \
		*) echo "Usage: make seed-plan PLAN=trial|starter|pro|enterprise"; exit 1;; \
	esac; \
	$(COMPOSE) exec -T postgres psql -U warmbly -d warmbly_dev \
		-v plan_id="$$plan_id" -v status="$$status" -v stripe_sub="$$stripe_sub" -v price="$$price" \
		-c "INSERT INTO subscriptions (id, user_id, organization_id, plan_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, status, current_period_start, current_period_end, free_trial_started_at, free_trial_ends_at, is_enterprise, created_at, updated_at) VALUES ('88888888-0000-0000-0000-000000000001', '11111111-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001', :'plan_id', 'cus_seed_dev', NULLIF(:'stripe_sub', ''), NULLIF(:'price', ''), :'status', NOW(), NOW() + INTERVAL '30 days', CASE WHEN :'status' = 'trialing' THEN NOW() ELSE NULL END, CASE WHEN :'status' = 'trialing' THEN NOW() + INTERVAL '14 days' ELSE NULL END, :'plan_id' = '00000000-0000-0000-0000-000000000130', NOW(), NOW()) ON CONFLICT (organization_id) DO UPDATE SET plan_id = EXCLUDED.plan_id, stripe_subscription_id = EXCLUDED.stripe_subscription_id, stripe_price_id = EXCLUDED.stripe_price_id, status = EXCLUDED.status, current_period_start = EXCLUDED.current_period_start, current_period_end = EXCLUDED.current_period_end, free_trial_started_at = EXCLUDED.free_trial_started_at, free_trial_ends_at = EXCLUDED.free_trial_ends_at, is_enterprise = EXCLUDED.is_enterprise, updated_at = NOW();"
	@echo "Seeded dev organization switched to $(PLAN). Log in as dev@warmbly.com / password123."

# Stop services, keep volumes.
stop:
	$(COMPOSE) --profile seed --profile sandbox stop

# Stop + remove containers, keep volumes (postgres, redis, web node_modules).
down:
	$(COMPOSE) --profile seed --profile sandbox down

# Nuke everything including volumes. Useful for "start over".
reset:
	$(COMPOSE) --profile seed --profile sandbox down -v

# Wipe ONLY the Postgres data and bring a fresh, empty database back up.
# Migrations are embedded and re-apply on the next `make backend` boot, so
# the usual flow is `make db-reset && make backend`. Redis/Kafka/etc. volumes
# are left untouched (use `make reset` to nuke every volume).
db-reset:
	$(DEV_COMPOSE) rm -sf postgres
	-docker volume rm warmbly_postgres_data
	$(DEV_COMPOSE) up -d postgres
	@echo ""
	@echo "Fresh Postgres up. Run 'make backend' to apply migrations (then 'make seed' for fixtures)."

# Drop every table/type/sequence in-place by recreating the public schema.
# Keeps the running container + volume (no recreate), so it's faster than
# db-reset and works while the rest of the stack stays up. golang-migrate's
# schema_migrations table is dropped too, so `make backend` re-applies every
# migration from scratch. Postgres must already be running (`make infra`).
db-wipe:
	$(COMPOSE) exec -T postgres psql -U warmbly -d warmbly_dev -v ON_ERROR_STOP=1 \
		-c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public; GRANT ALL ON SCHEMA public TO warmbly; GRANT ALL ON SCHEMA public TO public;'
	@echo ""
	@echo "Schema wiped. Run 'make migrate' (or 'make backend') to re-apply migrations (then 'make seed' for fixtures)."

# Apply all pending migrations and exit — no API server. Same embedded
# migrations the backend runs on boot, against the dev Postgres. Pair with
# db-wipe/db-reset, e.g. `make db-wipe && make migrate && make seed`.
migrate:
	$(GO_DEV_ENV) go run ./cmd/migrate

# Stream container logs.
#   make logs              # all services, last 200 lines + follow
#   make logs backend      # one service
#   make logs backend consumer    # multiple
logs:
	$(COMPOSE) logs -f --tail=200 $(RUN_ARGS)

status:
	$(COMPOSE) ps

# Rebuild + restart a single service, picking up code changes.
# Usage: `make restart backend` (positional) or `make restart SVC=backend`.
#
# In Docker, a service's binary is compiled into its image at build time —
# `docker compose restart` alone keeps the old binary. This target rebuilds
# the image first and then brings the container up against it, so saving a
# Go file and running `make restart backend` is the only step you need.
#
# The positional form works via the trick at the bottom of the file that
# captures extra goals as $(RUN_ARGS) and makes them no-op targets.
restart:
	@svc="$(RUN_ARGS)"; \
	if [ -z "$$svc" ]; then svc="$(SVC)"; fi; \
	if [ -z "$$svc" ]; then echo "Usage: make restart <service>"; exit 1; fi; \
	$(COMPOSE) build $$svc && $(COMPOSE) up -d $$svc

# Rebuild + restart every Go service in one shot. Use when you've touched
# something in internal/ and don't want to think about which service uses
# it. `--parallel` runs the three Go builds concurrently.
restart-go:
	$(COMPOSE) build --parallel backend consumer worker
	$(COMPOSE) up -d backend consumer worker

# Same but including Rust (tracking) and Elixir (realtime). Slower; the
# safe choice when you've touched things across stacks.
restart-all:
	$(COMPOSE) build --parallel backend consumer worker tracking realtime
	$(COMPOSE) up -d backend consumer worker tracking realtime

# ─── infra + app (hot-reload dev) ───────────────────────────────────────
#
# Split into two pieces so multiple worktrees can share the stateful
# stuff and only the language services churn per branch:
#
#   1. From any worktree (usually root):  make infra
#      Brings up postgres, redis, nats, and mailpit under the pinned
#      `warmbly` project. These stay up across worktree switches.
#
#   2. From the worktree you're iterating on:  make app
#      Brings up the language services in hot-reload mode against the
#      already-running infra. Bind-mounted source means saves trigger
#      in-container rebuilds with no image churn.
#
# `make up` is the separate prod-image flow for smoke tests.

DEV_COMPOSE := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml

# Stateful infrastructure for the no-cloud stack: Postgres, Redis, NATS
# (JetStream event bus), and Mailpit as a local SMTP sink. Brought up once and
# left running. No Kafka/Zookeeper/Schema-Registry, no LocalStack, no Stripe
# mock, no Cloud Tasks emulator — the app runs on the local providers.
INFRA_SVCS  := postgres redis nats mailpit

# Language services. The things you iterate on; recreated per worktree.
APP_SVCS    := backend consumer worker tracking realtime web admin

infra:
	$(COMPOSE) up -d $(INFRA_SVCS)
	@echo ""
	@echo "Infra up under project 'warmbly' (postgres, redis, nats, mailpit)."
	@echo "Run 'make run' (backend+consumer+worker native) + 'make web' to iterate."

infra-down:
	$(COMPOSE) stop $(INFRA_SVCS)
	@echo "Infra stopped. Volumes preserved; 'make infra' brings them back."

app:
	$(DEV_COMPOSE) up -d --build $(APP_SVCS)
	@echo ""
	@echo "App services up against infra (hot reload enabled)."
	@echo "Logs:  make app-logs    Stop:  make app-down"

app-down:
	$(DEV_COMPOSE) stop $(APP_SVCS)
	@echo "App services stopped. Infra still up (use 'make infra-down' to stop it too)."

app-logs:
	$(DEV_COMPOSE) logs -f --tail=200 $(APP_SVCS)

# ─── native dev (host-run Go, no docker rebuilds) ───────────────────────
#
# The fastest loop: infra stays in docker (`make infra`); the Go services
# run directly on the host with `go run`. Save a file, re-run the target,
# and it recompiles against the warm Go build cache in a second or two —
# no docker image build, no container recreate. This is the answer to
# "docker takes too long to restart".
#
#   make infra        # once: postgres, redis, nats, mailpit
#   make backend      # API on :8080 (own terminal; applies migrations on boot)
#   make consumer     # event consumer (own terminal)
#   make worker       # send/sync worker (own terminal)
#   make run          # all three at once in one terminal (Ctrl-C stops all)
#   make web          # dashboard dev server, pointed at the native backend
#
# Env mirrors the docker-compose service definitions but targets the
# host-published ports (postgres 15432, redis 16379, nats 4222, mailpit smtp
# 11025, dovecot imaps 10993) instead of the in-network names.
#
# Remote infra: by default the native services connect to infra on this same
# machine (INFRA_HOST=localhost). To run the Go services against infra hosted on
# a different computer, point them at it:
#
#   make run INFRA_HOST=192.168.1.50
#
# That rewrites every infra endpoint (postgres, redis, nats) to the remote host.
# The infra machine just has to publish those ports on an interface the dev box
# can reach (the compose `ports:` already bind 0.0.0.0).
INFRA_HOST ?= localhost

# ─── expose the dev servers off-box (Tailscale / LAN) ───────────────────
#
# By default every dev server binds localhost and the frontends call the
# backend at localhost, so only this machine can use them. To reach them from
# another computer (e.g. over Tailscale), set PUBLIC_HOST to the address OTHER
# machines use to reach THIS one — your Tailscale IPv4 (`tailscale ip -4`, a
# 100.x.y.z) or MagicDNS name (`<host>.<tailnet>.ts.net`) — and pass it to
# every target you start:
#
#   make backend PUBLIC_HOST=100.83.12.7
#   make web     PUBLIC_HOST=100.83.12.7
#   make admin   PUBLIC_HOST=100.83.12.7
#   make site    PUBLIC_HOST=100.83.12.7
#
# When set: the Vite/Astro servers bind 0.0.0.0 (reachable off-box), the
# dashboard + admin point their API/app URLs at PUBLIC_HOST, and the backend
# widens CORS to those origins. Unset → unchanged localhost behavior.
#
# The Go backend already listens on 0.0.0.0:8080, so it's reachable on the
# Tailscale IP without PUBLIC_HOST — but you still need PUBLIC_HOST so the
# browser app calls the backend at that address instead of its own localhost.
# (The Vite configs allow *.ts.net hosts, so MagicDNS names work too; raw IPs
# are always allowed.)
PUBLIC_HOST ?=

comma := ,

# localhost when PUBLIC_HOST is unset, else PUBLIC_HOST — used to build the
# browser-facing URLs handed to the frontends and the backend.
WEB_HOST := $(if $(PUBLIC_HOST),$(PUBLIC_HOST),localhost)

# `--host 0.0.0.0` only when exposing; empty (default localhost bind) otherwise.
VITE_HOST_FLAG := $(if $(PUBLIC_HOST),--host 0.0.0.0,)

# Backend CORS allowlist: web + admin origins at PUBLIC_HOST plus localhost.
# Empty when not exposing, so the backend keeps its APP_URL-derived default.
CORS_ORIGINS := $(if $(PUBLIC_HOST),http://$(PUBLIC_HOST):5173$(comma)http://$(PUBLIC_HOST):5174$(comma)http://localhost:5173$(comma)http://localhost:5174,)

# Shared by the control-plane services (backend, consumer). Flattened to
# one line by make so it can prefix a command as inline env.
# Fixed dev key sealing SMTP/IMAP credentials at rest (64 hex chars). The
# backend/consumer decrypt with it and cmd/sandbox seeds with it, so all
# three must share the value. Never reuse in production.
CREDENTIALS_KEY_DEV := 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

# CODEC_PROVIDER=json: the worker command/result envelopes carry `any`
# bodies that Avro cannot serialize, so worker messaging only works on the
# JSON codec. tracking-events stays Avro (dedicated Avrov2 path).
# AI provider for dev. Off by default (no provider => the assistant returns a
# clean 503). Pick a backend with AI_PROVIDER and supply a key + model; the preset
# fills in the base URL. AI_PROVIDER=ollama runs a free local model with no key.
#   make backend AI_PROVIDER=ollama                                    # free, local, no key
#   make backend AI_PROVIDER=openrouter AI_KEY=sk-or-... AI_MODEL=deepseek/deepseek-chat
#   make backend AI_PROVIDER=groq AI_KEY=gsk_... AI_MODEL=openai/gpt-oss-20b
#   make backend AI_PROVIDER=openai AI_KEY=sk-...
# Switch models by changing AI_MODEL (OpenRouter fronts every vendor). AI_FREE=true
# marks a free model so credits are not charged; ollama sets it automatically.
AI_PROVIDER ?=
AI_KEY ?=
AI_MODEL ?=
AI_BASE_URL ?=
AI_FREE ?=
ifeq ($(AI_PROVIDER),)
AI_DEV_ENV :=
else
AI_DEV_ENV := AI_PROVIDER=$(AI_PROVIDER) \
	$(if $(AI_KEY),AI_API_KEY=$(AI_KEY),) \
	$(if $(AI_MODEL),AI_MODEL=$(AI_MODEL),) \
	$(if $(AI_BASE_URL),AI_BASE_URL=$(AI_BASE_URL),) \
	$(if $(AI_FREE),AI_FREE=$(AI_FREE),)
endif

# Local no-cloud dev key (base64 32 bytes). Dev-only; a real deployment sets its
# own KMS_LOCAL_MASTER_KEY (see `make gen-key`). Losing it makes sealed mailbox
# credentials unrecoverable.
KMS_KEY_DEV := L0K8Q0mQ2wq6b1n8H3xO5r7t9v2y4A6D8F1J3M5P7A=
# Shared local blob dir for the filesystem storage provider (backend + worker
# run natively on the same host, so they share this path).
BLOB_FS_ROOT_DEV := /tmp/warmbly-blobs

GO_DEV_ENV := \
	APP_ENV=dev \
	AWS_CONFIG_ENABLED=false \
	EVENTBUS_PROVIDER=nats \
	NATS_URL=nats://$(INFRA_HOST):4222 \
	CODEC_PROVIDER=json \
	KMS_PROVIDER=local \
	KMS_LOCAL_MASTER_KEY=$(KMS_KEY_DEV) \
	CREDENTIALS_ENCRYPTION_KEY=$(CREDENTIALS_KEY_DEV) \
	BLOB_PROVIDER=filesystem \
	BLOB_FS_ROOT=$(BLOB_FS_ROOT_DEV) \
	BLOB_PUBLIC_BASE_URL=http://localhost:8080/public \
	TASKS_PROVIDER=local \
	BILLING_PROVIDER=none \
	CAPTCHA_PROVIDER=none \
	PUBSUB_ENABLED=false \
	ENCRYPTED_KEYS_PROVIDER=postgres \
	PRIMARY_DB=postgres://warmbly:warmbly@$(INFRA_HOST):15432/warmbly_dev?sslmode=disable \
	REDIS=redis://$(INFRA_HOST):16379

# Worker: NATS + local KMS + shared filesystem blobs; no Postgres by design
# (relational access is via the backend internal API).
WORKER_DEV_ENV := \
	APP_ENV=dev \
	AWS_CONFIG_ENABLED=false \
	EVENTBUS_PROVIDER=nats \
	NATS_URL=nats://$(INFRA_HOST):4222 \
	CODEC_PROVIDER=json \
	KMS_PROVIDER=local \
	KMS_LOCAL_MASTER_KEY=$(KMS_KEY_DEV) \
	CREDENTIALS_ENCRYPTION_KEY=$(CREDENTIALS_KEY_DEV) \
	BLOB_PROVIDER=filesystem \
	BLOB_FS_ROOT=$(BLOB_FS_ROOT_DEV) \
	MAIL_TLS_INSECURE=true \
	REDIS=redis://$(INFRA_HOST):16379

# API server on :8080. Applies the embedded migrations on boot against
# the docker postgres.
backend:
	$(GO_DEV_ENV) \
	$(AI_DEV_ENV) \
	API_HOST=0.0.0.0:8080 \
	GIN_MODE=debug \
	APP_URL=http://$(WEB_HOST):5173 \
	CORS_ALLOW_ORIGINS=$(CORS_ORIGINS) \
	WEBSOCKET_URL=ws://$(WEB_HOST):4000/socket/websocket \
	AUTH_SECRET=local-dev-auth-secret-minimum-32-characters-long \
	EMAIL_NAME='Warmbly Dev' \
	EMAIL_ADDRESS=dev@warmbly.local \
	TRACKING_DOMAIN=t.warmbly.com \
	SMTP_HOST=$(INFRA_HOST) \
	SMTP_PORT=11025 \
	GEODB_PATH=data/GeoLite2-City.mmdb \
	INTERNAL_API_TOKEN=local-dev-internal-token \
	go run ./cmd/backend

# Event consumer (NATS by default; Kafka with -tags kafka) -> postgres.
consumer:
	$(GO_DEV_ENV) \
	$(AI_DEV_ENV) \
	go run ./cmd/consumer

# Send/sync worker. No Postgres by design. WORKER_ID is an explicit UUID
# (the worker resolves identity from WORKER_ID first, then bind IP, then
# hostname), so it boots cleanly off-box.
#
# The worker reads encrypted DEKs through the backend's /internal/dek
# endpoint (the prod `http` provider, no worker DB), so `make backend`
# must be running and INTERNAL_API_TOKEN must match.
worker:
	$(WORKER_DEV_ENV) \
	WORKER_ID=10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a01 \
	WORKER_TIER=shared \
	ENCRYPTED_KEYS_PROVIDER=http \
	ENCRYPTED_KEYS_BACKEND_URL=http://localhost:8080 \
	ENCRYPTED_KEYS_WORKER_TOKEN=local-dev-internal-token \
	go run ./cmd/worker

# backend + consumer + worker together in one terminal. Ctrl-C stops all
# (kill 0 takes down go run and its child binaries). Run `make infra` first.
# Workers are interchangeable now; run a second `make worker WORKER_ID=<uuid>`
# in another terminal to add parallelism.
run:
	@echo "backend + consumer + worker (native). Ctrl-C stops all. Run 'make infra' first if infra is down."
	@trap 'kill 0' INT TERM; \
	$(MAKE) --no-print-directory backend & \
	$(MAKE) --no-print-directory consumer & \
	$(MAKE) --no-print-directory worker & \
	wait

# Generate a fresh base64 KMS master key for a real self-host deployment. Put
# the output in your .env as KMS_LOCAL_MASTER_KEY (and back it up: losing it
# makes every stored mailbox credential unrecoverable).
gen-key:
	@openssl rand -base64 32

# ─── one-command dev stack ───────────────────────────────────────────────
#
# `make dev` is the "just make it work" target for a fresh clone or a fresh
# morning: brings up the docker infra (postgres, redis, nats, mailpit), waits
# until postgres is accepting connections, applies migrations, loads the seed
# fixtures (idempotent), installs web + admin deps on first run, starts realtime
# and tracking as containers (no host elixir/cargo needed), then runs backend +
# consumer + worker + dashboard + admin together in this terminal. Ctrl-C stops
# the app; infra stays up for next time (`make infra-down` stops it too).
#
#   make dev                      # everything; dashboard on :5173
#   make dev SEED=false           # skip fixture seeding
#   make dev AI_PROVIDER=ollama   # with the AI assistant on (see AI env above)
#
# Log in with dev@warmbly.com / password123 (from the seed fixtures). For the
# fully populated demo org instead, use `make sandbox`.
SEED ?= true
dev:
	@command -v docker >/dev/null || { echo "docker is required: https://docs.docker.com/get-docker/"; exit 1; }
	@command -v go >/dev/null || { echo "go 1.25+ is required: https://go.dev/dl/"; exit 1; }
	@command -v pnpm >/dev/null || { echo "pnpm is required: https://pnpm.io/installation"; exit 1; }
	$(COMPOSE) up -d $(INFRA_SVCS)
	@echo "Waiting for infra to be ready (postgres, redis, nats)..."
	@until $(COMPOSE) exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; do sleep 1; done
	$(GO_DEV_ENV) go run ./cmd/migrate
	@if [ "$(SEED)" = "true" ]; then $(GO_DEV_ENV) SEED_RICH=true SEED_FULL=true go run ./cmd/seed; fi
	@if [ ! -d web/node_modules ]; then echo "Installing web dependencies (first run)..."; cd web && pnpm install; fi
	@if [ ! -d admin/node_modules ]; then echo "Installing admin dependencies (first run)..."; cd admin && pnpm install; fi
	@echo "Starting realtime + tracking as containers (no host Elixir/cargo needed)..."
	@BACKEND_INTERNAL_URL=http://host.docker.internal:8080 $(COMPOSE) up -d --build realtime tracking
	@echo ""
	@echo "Starting backend + consumer + worker + dashboard + admin. Ctrl-C stops them (infra stays up)."
	@echo "Dashboard: http://localhost:5173    Admin: http://localhost:5174    Login: dev@warmbly.com / password123"
	@echo ""
	@trap 'kill 0' INT TERM; \
	$(MAKE) --no-print-directory backend & \
	$(MAKE) --no-print-directory consumer & \
	$(MAKE) --no-print-directory worker & \
	$(MAKE) --no-print-directory web & \
	$(MAKE) --no-print-directory admin & \
	wait

# ─── other native services (Rust tracking, Elixir realtime) ──────────────
#
# `make dev` runs these as containers, so you don't need cargo/elixir on the
# host. Use these native targets only if you want to iterate on the Rust or
# Elixir source directly.

# Open/click tracking service (Rust) on :3000 — NATS by default (no Kafka).
tracking:
	cd tracking && \
	APP_ENV=dev \
	AWS_CONFIG_ENABLED=false \
	TRACKING_HOST=0.0.0.0 \
	TRACKING_PORT=3000 \
	EVENTBUS_PROVIDER=nats \
	NATS_URL=nats://localhost:4222 \
	KAFKA_TRACKING_TOPIC=tracking-events \
	BACKEND_INTERNAL_URL=http://localhost:8080 \
	INTERNAL_API_TOKEN=local-dev-internal-token \
	cargo run

# Websocket fanout service (Elixir/Phoenix) on :4000. MIX_ENV=dev skips
# the prod-only env guards in runtime.exs; reads discrete DATABASE_* and
# REDIS_URL.
realtime:
	cd realtime && \
	export MIX_ENV=dev \
	       JWT_SECRET=local-dev-auth-secret-minimum-32-characters-long \
	       PORT=4000 \
	       PHX_HOST=$(WEB_HOST) \
	       DATABASE_HOST=localhost \
	       DATABASE_PORT=15432 \
	       DATABASE_NAME=warmbly_dev \
	       DATABASE_USER=warmbly \
	       DATABASE_PASSWORD=warmbly \
	       REDIS_URL=redis://localhost:16379 && \
	mix deps.get && mix phx.server

# ─── standalone frontends (web + admin + marketing site) ─────────────────
#
# Run each in its own terminal; all foreground the dev server (Ctrl-C to
# stop) and assume `pnpm install` has already run in the directory.
#
#   make web      # Vite dev server on http://localhost:5173 (dashboard)
#   make admin    # Vite dev server on http://localhost:5174
#   make site     # Astro dev server on http://localhost:4321
#
# `make web` points the dashboard at the natively-run backend on :8080,
# so you don't need the dockerized `web` service from `make app`.
#
# To reach these from another computer (Tailscale / LAN), add PUBLIC_HOST to
# every target (see the PUBLIC_HOST section above), e.g.
#   make backend PUBLIC_HOST=$$(tailscale ip -4 | head -1)
#   make web     PUBLIC_HOST=$$(tailscale ip -4 | head -1)

web:
	cd web && \
	VITE_APP_URL=http://$(WEB_HOST):5173 \
	VITE_API_URL=http://$(WEB_HOST):8080 \
	VITE_TRACKING_DOMAIN=t.warmbly.com \
	VITE_TURNSTILE_KEY=1x00000000000000000000AA \
	VITE_TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass \
	pnpm dev $(VITE_HOST_FLAG)

admin:
	cd admin && \
	VITE_API_URL=http://$(WEB_HOST):8080 \
	VITE_DASHBOARD_URL=http://$(WEB_HOST):5173 \
	VITE_TURNSTILE_KEY=1x00000000000000000000AA \
	VITE_TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass \
	pnpm dev $(VITE_HOST_FLAG)

site:
	cd site && pnpm dev $(VITE_HOST_FLAG)

# Engineering docs (Fumadocs / Next.js). Port 4322 — :3000 is the tracking
# service and :4321 is the marketing site. http://localhost:4322
# Next.js reads PORT from the env (passing -p through pnpm gets mangled).
docs:
	cd docs && PORT=4322 pnpm dev

# ─── admin bootstrap (local/test only) ──────────────────────────────────
#
# Grant a registered user platform admin access by flipping
# users.admin_permissions. There is no other way to seed the first admin —
# the in-app GrantAdminPermissions endpoint requires you to already be one.
#
#   make grant-admin EMAIL=you@example.com               # super (all perms)
#   make grant-admin EMAIL=you@example.com ROLE=support
#   make grant-admin EMAIL=you@example.com BITMASK=1     # raw bitmask
#   make revoke-admin EMAIL=you@example.com              # back to 0
#
# Role bitmasks mirror AdminRolePermissions in
# internal/models/admin_permission.go.
ROLE ?= super
grant-admin:
	@if [ -z "$(EMAIL)" ]; then \
		echo "Usage: make grant-admin EMAIL=<email> [ROLE=super|support|ops|analyst] [BITMASK=N]"; \
		exit 1; \
	fi
	@bits="$(BITMASK)"; \
	if [ -z "$$bits" ]; then \
		case "$(ROLE)" in \
			super)   bits=4194303 ;; \
			support) bits=1086401 ;; \
			ops)     bits=1062960 ;; \
			analyst) bits=1055233 ;; \
			*) echo "Unknown ROLE='$(ROLE)'. Use super|support|ops|analyst or pass BITMASK=N."; exit 1 ;; \
		esac; \
	fi; \
	echo "Granting admin_permissions=$$bits to $(EMAIL)..."; \
	out=$$($(COMPOSE) exec -T postgres psql -U warmbly -d warmbly_dev -tA \
		-v ON_ERROR_STOP=1 \
		-c "UPDATE users SET admin_permissions = $$bits, admin_granted_at = NOW() WHERE email = '$(EMAIL)' RETURNING id;"); \
	if [ -z "$$out" ]; then \
		echo "No user with email $(EMAIL). Sign up at http://localhost:5173 first."; \
		exit 1; \
	fi; \
	echo "OK. user_id=$$out — open http://localhost:5174 and sign in."

revoke-admin:
	@if [ -z "$(EMAIL)" ]; then echo "Usage: make revoke-admin EMAIL=<email>"; exit 1; fi
	@$(COMPOSE) exec -T postgres psql -U warmbly -d warmbly_dev -v ON_ERROR_STOP=1 \
		-c "UPDATE users SET admin_permissions = 0 WHERE email = '$(EMAIL)';"

# Positional-args plumbing. When the first goal is `restart` or `logs`,
# capture every following word as RUN_ARGS and declare those words as
# no-op rules so make doesn't error with "no rule for target".
ifneq (,$(filter restart logs,$(firstword $(MAKECMDGOALS))))
  RUN_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
  $(eval $(RUN_ARGS):;@:)
endif

# Run seeder tests against the docker-compose Postgres. Brings up the db
# if it isn't running. Requires `docker compose up -d postgres` to have
# happened at least once so the volume exists.
test-seed:
	$(COMPOSE) up -d postgres
	@until $(COMPOSE) exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; do echo "waiting for postgres..."; sleep 1; done
	SEED_TEST_DB="postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable" \
		go test -count=1 -v ./cmd/seed/
