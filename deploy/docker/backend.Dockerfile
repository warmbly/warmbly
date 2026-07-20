# syntax=docker/dockerfile:1.7
#
# Default build is CGO-free (NATS + JSON) — no librdkafka, gcc, or musl-dev, so
# it builds in a fraction of the time and cross-compiles to arm64/amd64 cleanly.
# BuildKit cache mounts keep the module + compile caches warm across builds.
#
# To include the optional Kafka backend, build with --build-arg GO_TAGS=kafka
# (adds librdkafka + CGO; slower). Runtime selection is still by env
# (EVENTBUS_PROVIDER / CODEC_PROVIDER).
FROM golang:1.25-alpine AS builder

ARG GO_TAGS=""
RUN apk add --no-cache git ca-certificates && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache gcc musl-dev librdkafka-dev; fi

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    if echo "$GO_TAGS" | grep -qw kafka; then CGO=1; TAGS="musl kafka"; else CGO=0; TAGS=""; fi; \
    CGO_ENABLED=$CGO GOOS=linux go build -tags "$TAGS" -ldflags="-s -w" -o /out/backend ./cmd/backend; \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/seed ./cmd/seed; \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# Runtime stage
FROM alpine:3.23

ARG GO_TAGS=""
RUN apk add --no-cache ca-certificates tzdata && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache librdkafka; fi && \
    adduser -D -u 1000 warmbly

COPY --from=builder /out/backend /app/backend
COPY --from=builder /out/seed /app/seed
COPY --from=builder /out/migrate /app/migrate

# Installer script the worker orchestrator uploads + runs over SSH.
COPY scripts/install-worker.sh /app/scripts/install-worker.sh

USER warmbly
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/backend"]
