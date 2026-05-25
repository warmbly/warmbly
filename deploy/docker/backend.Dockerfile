# syntax=docker/dockerfile:1.7
#
# Cache mounts below are keyed by a stable `id=warmbly-go*` so every
# build on this Docker daemon shares them — across `make restart`,
# across services (backend/worker/consumer/seed), and across worktrees.
# Switching to a fresh worktree no longer re-downloads modules.

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates gcc musl-dev librdkafka-dev

ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/root/.cache/go-build
ENV GOTMPDIR=/tmp

WORKDIR /app

# Modules layer. Only re-runs when go.mod / go.sum change; even then
# the cache mount makes the download incremental.
COPY go.mod go.sum ./
RUN --mount=type=cache,id=warmbly-gomodcache,target=/go/pkg/mod \
    go mod download

# Source layer.
COPY . .

# Build both binaries. Build cache survives between runs via the
# GOCACHE mount, so an unchanged subtree compiles in seconds.
RUN --mount=type=cache,id=warmbly-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=warmbly-gocache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -tags musl -ldflags="-s -w" -o /backend ./cmd/backend
RUN --mount=type=cache,id=warmbly-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=warmbly-gocache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /seed ./cmd/seed

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata librdkafka
RUN adduser -D -u 1000 warmbly

COPY --from=builder /backend /app/backend
COPY --from=builder /seed /app/seed

# Installer script the worker orchestrator uploads + runs over SSH.
COPY scripts/install-worker.sh /app/scripts/install-worker.sh

USER warmbly
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/backend"]
