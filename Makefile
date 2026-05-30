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
        up sim seed seed-plan reset logs status stop down tools test-seed \
        restart restart-go restart-all infra infra-down app app-down app-logs \
        backend consumer worker run tracking realtime web \
        admin site grant-admin revoke-admin

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

# ─── dev / simulation stack ─────────────────────────────────────────────

# Bring up the production-style stack (one worker, foreground).
# Uses the unchanged docker-compose.yml — every service runs the same
# image it would run in prod, just wired against local infra. Good
# for "does the release binary boot?" smoke tests.
up:
	$(COMPOSE) up

# Full simulation: infra + app + premium and dedicated workers.
sim:
	$(COMPOSE) --profile sim up

# Load rich fixture data. Requires backend up (via `make backend`,
# `make app`, or `make up`) so migrations have run. `-T` disables TTY
# allocation (Make's shell isn't a tty; without -T compose can silently
# swallow the seed's stdout).
seed:
	$(COMPOSE) --profile seed run --rm -T seed

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

# Spin up debugging UIs (kafka-ui).
tools:
	$(COMPOSE) --profile tools up -d kafka-ui
	@echo "kafka-ui: http://localhost:18090"

# Stop services, keep volumes.
stop:
	$(COMPOSE) --profile sim --profile seed --profile tools stop

# Stop + remove containers, keep volumes (postgres, redis, web node_modules).
down:
	$(COMPOSE) --profile sim --profile seed --profile tools down

# Nuke everything including volumes. Useful for "start over".
reset:
	$(COMPOSE) --profile sim --profile seed --profile tools down -v

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
	$(COMPOSE) build --parallel backend consumer worker-shared-1
	$(COMPOSE) up -d backend consumer worker-shared-1

# Same but including Rust (tracking) and Elixir (realtime). Slower; the
# safe choice when you've touched things across stacks.
restart-all:
	$(COMPOSE) build --parallel backend consumer worker-shared-1 tracking realtime
	$(COMPOSE) up -d backend consumer worker-shared-1 tracking realtime

# ─── infra + app (hot-reload dev) ───────────────────────────────────────
#
# Split into two pieces so multiple worktrees can share the stateful
# stuff and only the language services churn per branch:
#
#   1. From any worktree (usually root):  make infra
#      Brings up postgres, redis, kafka, etc. under the pinned
#      `warmbly` project. These stay up across worktree switches.
#
#   2. From the worktree you're iterating on:  make app
#      Brings up the language services in hot-reload mode against the
#      already-running infra. Bind-mounted source means saves trigger
#      in-container rebuilds with no image churn.
#
# `make up` is the separate prod-image flow for smoke tests.

DEV_COMPOSE := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml

# Stateful infrastructure. Brought up once and left running.
# localstack-init is a one-shot that (re)creates the KMS alias, DynamoDB
# tables, and S3 bucket. localstack runs with PERSISTENCE=0, so those are
# wiped on every restart and must be recreated before any service (incl.
# the natively-run backend) touches KMS/Dynamo/S3.
INFRA_SVCS  := postgres redis zookeeper kafka kafka-init schema-registry \
               mailpit localstack localstack-init stripe-mock cloud-tasks-emulator

# Language services. The things you iterate on; recreated per worktree.
APP_SVCS    := backend consumer worker-shared-1 tracking realtime web

infra:
	$(DEV_COMPOSE) up -d $(INFRA_SVCS)
	@echo ""
	@echo "Infra up under project 'warmbly'. Switch worktrees freely;"
	@echo "run 'make app' (docker) or 'make backend' (native) to iterate."

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
#   make infra        # once: postgres, redis, kafka, localstack (+init), ...
#   make backend      # API on :8080 (own terminal; applies migrations on boot)
#   make consumer     # kafka -> postgres consumer (own terminal)
#   make worker       # send/sync worker (own terminal)
#   make run          # all three at once in one terminal (Ctrl-C stops all)
#   make web          # dashboard dev server, pointed at the native backend
#
# Env mirrors the docker-compose service definitions but targets the
# host-published ports (postgres 15432, redis 16379, kafka 9092,
# schema-registry 8081, localstack 4566, mailpit smtp 11025,
# cloud-tasks 8123, stripe-mock 12111) instead of the in-network names.

# Shared by the control-plane services (backend, consumer). Flattened to
# one line by make so it can prefix a command as inline env.
GO_DEV_ENV := \
	APP_ENV=dev \
	AWS_CONFIG_ENABLED=false \
	AWS_ENDPOINT_URL=http://localhost:4566 \
	AWS_REGION=us-east-1 \
	AWS_ACCESS_KEY_ID=test \
	AWS_SECRET_ACCESS_KEY=test \
	PRIMARY_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
	REDIS=redis://localhost:16379 \
	KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
	KAFKA_CONSUMER_GROUP=consumer-group \
	SCHEMA_REGISTRY_URL=http://localhost:8081 \
	ASTRA_DB_ID=local-astra-db-id \
	ASTRA_DB_REGION=local-region \
	ASTRA_KEYSPACE_NAME=warmbly_dev \
	ASTRA_APPLICATION_TOKEN=local-astra-token \
	GCP_PROJECT_ID=

# Worker keeps the infra/AWS env but never Postgres or the consumer group
# — it has no relational access by design.
WORKER_DEV_ENV := \
	APP_ENV=dev \
	AWS_CONFIG_ENABLED=false \
	AWS_ENDPOINT_URL=http://localhost:4566 \
	AWS_REGION=us-east-1 \
	AWS_ACCESS_KEY_ID=test \
	AWS_SECRET_ACCESS_KEY=test \
	REDIS=redis://localhost:16379 \
	KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
	SCHEMA_REGISTRY_URL=http://localhost:8081

# API server on :8080. Applies the embedded migrations on boot against
# the docker postgres.
backend:
	$(GO_DEV_ENV) \
	API_HOST=0.0.0.0:8080 \
	GIN_MODE=debug \
	APP_URL=http://localhost:5173 \
	WEBSOCKET_URL=ws://localhost:4000/socket/websocket \
	KAFKA_TRACKING_TOPIC=tracking-events \
	AUTH_SECRET=local-dev-auth-secret-minimum-32-characters-long \
	GOOGLE_CLIENT_ID=local-google-client-id \
	GOOGLE_CLIENT_SECRET=local-google-client-secret \
	GOOGLE_REDIRECT_URI=http://localhost:3000/auth/google/callback \
	APPLE_APP_ID=local-apple-app-id \
	APPLE_TEAM_ID=local-apple-team-id \
	APPLE_KEY_ID=local-apple-key-id \
	APPLE_KEY_SECRET=local-apple-key-secret-base64 \
	TURNSTILE_SECRET=1x0000000000000000000000000000000AA \
	TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass \
	STRIPE_API_BASE=http://localhost:12111 \
	STRIPE_SECRET_KEY=sk_test_local \
	STRIPE_WEBHOOK_SECRET=whsec_local \
	STRIPE_PUBLISHABLE_KEY=pk_test_local \
	EMAIL_NAME='Warmbly Dev' \
	EMAIL_ADDRESS=dev@warmbly.local \
	TRACKING_DOMAIN=t.warmbly.com \
	SMTP_HOST=localhost \
	SMTP_PORT=11025 \
	GEODB_PATH=data/GeoLite2-City.mmdb \
	INTERNAL_API_TOKEN=local-dev-internal-token \
	GOOGLE_APPLICATION_CREDENTIALS_JSON=dev@local.iam.gserviceaccount.com \
	CLOUD_TASKS_EMULATOR_HOST=localhost:8123 \
	CLOUD_TASKS_QUEUE_NAME=projects/local/locations/local/queues/default \
	CLOUD_TASKS_WEBHOOK_URL=http://localhost:8080/webhook/email \
	go run ./cmd/backend

# Kafka -> postgres consumer.
consumer:
	$(GO_DEV_ENV) \
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
run:
	@echo "backend + consumer + worker (native). Ctrl-C stops all. Run 'make infra' first if infra is down."
	@trap 'kill 0' INT TERM; \
	$(MAKE) --no-print-directory backend & \
	$(MAKE) --no-print-directory consumer & \
	$(MAKE) --no-print-directory worker & \
	wait

# ─── other native services (Rust tracking, Elixir realtime) ──────────────
#
# Deliberately NOT part of `make run` — run them on their own only when you
# need the open/click tracking pixel service or the websocket fanout. Each
# needs its language toolchain on the host (cargo / elixir+mix).

# Open/click tracking service (Rust) on :3000.
tracking:
	cd tracking && \
	APP_ENV=dev \
	AWS_CONFIG_ENABLED=false \
	TRACKING_HOST=0.0.0.0 \
	TRACKING_PORT=3000 \
	KAFKA_BOOTSTRAP_SERVERS=localhost:9092 \
	KAFKA_TRACKING_TOPIC=tracking-events \
	SCHEMA_REGISTRY_URL=http://localhost:8081 \
	cargo run

# Websocket fanout service (Elixir/Phoenix) on :4000. MIX_ENV=dev skips
# the prod-only env guards in runtime.exs; reads discrete DATABASE_* and
# REDIS_URL.
realtime:
	cd realtime && \
	export MIX_ENV=dev \
	       PORT=4000 \
	       PHX_HOST=localhost \
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

web:
	cd web && \
	VITE_APP_URL=http://localhost:5173 \
	VITE_API_URL=http://localhost:8080 \
	VITE_TRACKING_DOMAIN=t.warmbly.com \
	VITE_TURNSTILE_KEY=1x00000000000000000000AA \
	VITE_TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass \
	pnpm dev

admin:
	cd admin && pnpm dev

site:
	cd site && pnpm dev

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
