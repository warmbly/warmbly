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
        dev sim seed reset logs status stop down tools test-seed \
        restart rebuild rebuild-go rebuild-all

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

# Start infra + app services (one worker). Foreground.
dev:
	docker compose up

# Full simulation: infra + app + premium and dedicated workers.
sim:
	docker compose --profile sim up

# Load rich fixture data. Backend must already be healthy.
seed:
	docker compose --profile seed run --rm seed

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

logs:
	docker compose logs -f --tail=200

status:
	docker compose ps

# Restart one service without rebuilding. Useful when config or env
# changed but the binary is still current.
#   make restart SVC=backend
restart:
	@if [ -z "$(SVC)" ]; then echo "Usage: make restart SVC=<service>"; exit 1; fi
	docker compose restart $(SVC)

# Rebuild + restart one service. Use after a code change.
#   make rebuild SVC=backend
rebuild:
	@if [ -z "$(SVC)" ]; then echo "Usage: make rebuild SVC=<service>"; exit 1; fi
	docker compose build $(SVC)
	docker compose up -d $(SVC)

# Rebuild + restart every Go service in one shot. Quick "I changed
# internal/ and don't want to think about which service uses it".
rebuild-go:
	docker compose build backend consumer worker-shared-1
	docker compose up -d backend consumer worker-shared-1

# Rebuild + restart everything that has source. Includes Rust (tracking)
# and Elixir (realtime) so cross-stack changes are picked up too. Slower
# than rebuild-go but the safe choice when you don't remember what you
# touched.
rebuild-all:
	docker compose build backend consumer worker-shared-1 tracking realtime
	docker compose up -d backend consumer worker-shared-1 tracking realtime

# Run seeder tests against the docker-compose Postgres. Brings up the db
# if it isn't running. Requires `docker compose up -d postgres` to have
# happened at least once so the volume exists.
test-seed:
	docker compose up -d postgres
	@until docker compose exec -T postgres pg_isready -U warmbly >/dev/null 2>&1; do echo "waiting for postgres..."; sleep 1; done
	SEED_TEST_DB="postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable" \
		go test -count=1 -v ./cmd/seed/
