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

.PHONY: setup-tools lint proto check-proto \
        up sim seed reset logs status stop down tools test-seed \
        restart restart-go restart-all infra infra-down app app-down app-logs

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
	$(COMPOSE) up

# Full simulation: infra + app + premium and dedicated workers.
sim:
	$(COMPOSE) --profile sim up

# Load rich fixture data. Requires backend up (via `make app` or
# `make up`) so migrations have run. `-T` disables TTY allocation
# (Make's shell isn't a tty; without -T compose can silently swallow
# the seed's stdout).
seed:
	$(COMPOSE) --profile seed run --rm -T seed

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
#      in-container rebuilds with no image churn:
#        - backend / consumer / worker-shared-1  → air rebuilds the
#          binary into ./tmp/main and restarts in place
#        - tracking                              → cargo-watch reruns
#          `cargo run` on changes under tracking/src
#        - realtime                              → Phoenix reloads
#          modules in-process; no external watcher
#
#   3. Switching worktrees:  cd <other-worktree> && make app
#      Because every worktree pins `-p warmbly`, this recreates the
#      app containers in place against the new worktree's source.
#      Infra is never touched. Caches (Go mod + build, Cargo registry
#      + target, Mix deps + _build) live on named volumes whose
#      `name:` skips the per-project prefix, so the first switch into
#      a worktree is a warm compile (seconds), subsequent switches are
#      near-instant.
#
# `make up` is the separate prod-image flow for smoke tests.

DEV_COMPOSE := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml

# Stateful infrastructure. Brought up once and left running.
INFRA_SVCS  := postgres redis zookeeper kafka schema-registry \
               mailpit localstack stripe-mock cloud-tasks-emulator

# Language services. The things you iterate on; recreated per worktree.
APP_SVCS    := backend consumer worker-shared-1 tracking realtime web

infra:
	$(DEV_COMPOSE) up -d $(INFRA_SVCS)
	@echo ""
	@echo "Infra up under project 'warmbly'. Switch worktrees freely;"
	@echo "run 'make app' in whichever worktree you want to iterate on."

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
