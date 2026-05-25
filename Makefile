GO_BIN := $(shell go env GOBIN)
ifeq ($(strip $(GO_BIN)),)
GO_BIN := $(shell go env GOPATH)/bin
endif

export PATH := $(GO_BIN):$(PATH)

GOLANGCI_LINT_VERSION ?= v1.64.8
PROTOC_GEN_GO_VERSION ?= v1.36.11
PROTOC_GEN_GO_GRPC_VERSION ?= v1.6.1

PROTO_DIR := internal/tasks/proto
PROTO_GEN_FILES := $(PROTO_DIR)/tasks.pb.go

.PHONY: setup-tools lint proto check-proto \
        up dev dev-down dev-logs sim seed reset logs status stop down tools test-seed \
        restart restart-go restart-all cache-clean

setup-tools:
	@echo "Installing required Go tools into $(GO_BIN)"
	GOBIN=$(GO_BIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN=$(GO_BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(GO_BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

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
	docker compose up

# Full simulation: infra + app + premium and dedicated workers.
sim:
	docker compose --profile sim up

# Load rich fixture data.
#
# Two-step on purpose:
#
#   1. `up -d backend` forces compose to reconcile backend against the
#      current dev spec. If the running container was created from an
#      older spec (e.g. healthcheck definition changed), compose
#      recreates it; if the spec matches, this is a near-no-op. This
#      is the step that fixes the "dependency backend failed to start"
#      failure when an older backend container had no healthcheck and
#      seed's `service_healthy` could therefore never be satisfied.
#
#   2. `run --rm seed` then runs the one-shot seeder. With backend now
#      guaranteed to be the current spec (lenient dev healthcheck),
#      service_healthy resolves cleanly.
#
# Goes through DEV_COMPOSE so that the spec compose sees matches what
# `make dev` would start; running `make seed` after `make up` still
# works because `docker compose run` doesn't recreate already-running
# deps. DEV_COMPOSE is defined further down (Make evaluates lazily).
seed:
	$(DOCKER_ENV) $(DEV_COMPOSE) up -d backend
	$(DEV_COMPOSE) --profile seed run --rm seed

# Spin up debugging UIs (kafka-ui).
tools:
	docker compose --profile tools up -d kafka-ui
	@echo "kafka-ui: http://localhost:18090"

# Stop services, keep volumes.
stop:
	docker compose --profile sim --profile seed --profile tools stop

# Stop + remove containers, keep volumes (postgres, redis, web node_modules).
down:
	docker compose --profile sim --profile seed --profile tools down

# Nuke everything including volumes. Useful for "start over".
reset:
	docker compose --profile sim --profile seed --profile tools down -v

# Stream container logs.
#   make logs              # all services, last 200 lines + follow
#   make logs backend      # one service
#   make logs backend consumer    # multiple
logs:
	docker compose logs -f --tail=200 $(RUN_ARGS)

status:
	docker compose ps

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
#
# DOCKER_BUILDKIT=1 + COMPOSE_DOCKER_CLI_BUILD=1 ensures BuildKit is
# active so the `--mount=type=cache,id=warmbly-go*` directives in the
# Go Dockerfiles actually attach. Without BuildKit those mounts are
# silently ignored and every build re-downloads the module graph.
DOCKER_ENV := DOCKER_BUILDKIT=1 COMPOSE_DOCKER_CLI_BUILD=1

restart:
	@svc="$(RUN_ARGS)"; \
	if [ -z "$$svc" ]; then svc="$(SVC)"; fi; \
	if [ -z "$$svc" ]; then echo "Usage: make restart <service>"; exit 1; fi; \
	$(DOCKER_ENV) docker compose build $$svc && docker compose up -d $$svc

# Rebuild + restart every Go service in one shot. Use when you've touched
# something in internal/ and don't want to think about which service uses
# it.
#
# `--parallel` runs the three Go builds concurrently. Because the Go
# Dockerfiles share `id=warmbly-go*` cache mounts, the second and third
# builds hit a warm module + build cache and finish in a fraction of
# the first build's time.
restart-go:
	$(DOCKER_ENV) docker compose build --parallel backend consumer worker-shared-1
	docker compose up -d backend consumer worker-shared-1

# Same but including Rust (tracking) and Elixir (realtime). Slower; the
# safe choice when you've touched things across stacks.
restart-all:
	$(DOCKER_ENV) docker compose build --parallel backend consumer worker-shared-1 tracking realtime
	docker compose up -d backend consumer worker-shared-1 tracking realtime

# ─── hot-reload dev mode ────────────────────────────────────────────────
#
# Bind-mounts source into long-running containers so saves are picked
# up without a docker image rebuild:
#
#   - backend / consumer / worker-shared-1   → air watches .go and
#     rebuilds the binary into ./tmp/main, then restarts in place.
#   - tracking                                → cargo-watch reruns
#     `cargo run` on changes under tracking/src.
#   - realtime                                → mix phx.server — Phoenix
#     reloads modules in-process; no external watcher.
#
# Caches (Go mod + build, Cargo registry + target, Mix deps + _build)
# live on named volumes whose `name:` skips the per-project prefix, so
# the second worktree to bring up `make dev` starts in seconds.
#
# `make up` still gives you the production-style images. Dev is opt-in.

DEV_COMPOSE := docker compose -f docker-compose.yml -f docker-compose.dev.yml
# Services with a hot-reload override in docker-compose.dev.yml. Used
# by dev-down / dev-logs to scope to "the things dev mode replaced".
DEV_SVCS    := backend consumer worker-shared-1 tracking realtime

# Bring up the full stack with dev-mode overrides for the language
# services. Same set of services you'd get from `make up` (infra,
# mailpit, web, app, one worker), but the 5 language services in
# DEV_SVCS run from the *-dev Dockerfiles with bind-mounted source.
#
# We pass no service list on purpose: depends_on doesn't pull in
# mailpit (only used via SMTP env vars, no edge in the graph) or web,
# so naming the 5 dev services directly would silently drop them. The
# user expectation is "dev = up + hot reload", so default to the full
# default profile.
#
# Escape hatch: SVCS="backend consumer worker-shared-1" narrows the
# `up` to just those services (plus their depends_on chain) if you
# don't want to spin up Rust/Elixir.
SVCS ?=
dev:
	$(DOCKER_ENV) $(DEV_COMPOSE) up -d --build $(SVCS)
	@echo ""
	@echo "Dev mode running. Hot reload enabled for: $(DEV_SVCS)"
	@echo "  Go saves           → ~2-5s rebuild (air)"
	@echo "  Rust saves         → ~2-10s rebuild (cargo-watch, debug build)"
	@echo "  Elixir saves       → in-process reload (Phoenix)"
	@echo ""
	@echo "Stream logs:   make dev-logs"
	@echo "Stop dev:      make dev-down    (stops language services only)"
	@echo "Stop all:      make stop        (everything, incl. infra)"

dev-down:
	$(DEV_COMPOSE) stop $(DEV_SVCS)
	@echo "dev language services stopped (infra still up). Run 'make stop' to tear down everything."

dev-logs:
	$(DEV_COMPOSE) logs -f --tail=200 $(DEV_SVCS)

# Force-drop the BuildKit cache. Useful when something's gone weird
# (corrupted cache, debugging a "works on a clean build but not after
# a rebuild" issue). The shared Go module + build caches will rebuild
# on the next `make restart-go`.
#
# We prune everything because `docker builder prune` doesn't expose a
# filter for our `id=warmbly-*` cache mounts. The cost of an aggressive
# prune is one slow build afterwards — small price for an escape hatch.
cache-clean:
	docker builder prune -af
	@echo "BuildKit cache cleared. Next make restart-go will repopulate the warmbly Go caches."

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
	docker compose up -d postgres
	@until docker compose exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; do echo "waiting for postgres..."; sleep 1; done
	SEED_TEST_DB="postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable" \
		go test -count=1 -v ./cmd/seed/
