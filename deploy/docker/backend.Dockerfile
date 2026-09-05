# syntax=docker/dockerfile:1.7
#
# Default build is CGO-free (NATS + JSON) — no librdkafka, gcc, or musl-dev, so
# it builds in a fraction of the time and cross-compiles to arm64/amd64 cleanly.
# BuildKit cache mounts keep the module + compile caches warm across builds.
#
# The builder always runs on the build host ($BUILDPLATFORM) and cross-compiles
# to $TARGETARCH, so multi-arch CI builds never run the Go compiler under QEMU.
#
# To include the optional Kafka backend, build with --build-arg GO_TAGS=kafka
# (adds librdkafka + CGO; slower, and CGO cannot cross-compile — build each arch
# on a native runner). Runtime selection is still by env
# (EVENTBUS_PROVIDER / CODEC_PROVIDER).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG GO_TAGS=""
ARG TARGETOS TARGETARCH
# Build identity shown in the admin panel; see internal/version.
ARG VERSION="" COMMIT="" BUILT_AT=""
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
    CGO_ENABLED=$CGO GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags "$TAGS" -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/backend ./cmd/backend; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/seed ./cmd/seed; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/migrate ./cmd/migrate; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/warmblyctl ./cmd/warmblyctl; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/warmbly ./cmd/cli

# Runtime stage
FROM alpine:3.23

ARG GO_TAGS=""
# postgresql-client is here for `warmblyctl backup` and `warmblyctl restore`:
# the instance bundle is a pg_dump and the restore replays it with psql, and the
# backend container is where the CLI already has PRIMARY_DB and the blob root.
RUN apk add --no-cache ca-certificates tzdata postgresql-client && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache librdkafka; fi && \
    adduser -D -u 1000 warmbly

# BLOB_FS_ROOT's default mount point, owned by the user the process runs as.
# Docker seeds a fresh named volume from the image, so the directory has to
# exist here with the right owner; otherwise Docker creates the mount point
# root-owned, the non-root process cannot write to it, and the first send fails
# with "mkdir /data/blobs/emails: permission denied".
RUN mkdir -p /data/blobs && chown -R warmbly:warmbly /data

COPY --from=builder /out/backend /app/backend
COPY --from=builder /out/seed /app/seed
COPY --from=builder /out/migrate /app/migrate

# The operator CLI goes on PATH, not /app, so the documented recovery command is
# `docker compose exec backend warmblyctl status` and not a path.
COPY --from=builder /out/warmblyctl /usr/local/bin/warmblyctl

# The customer CLI ships alongside it, so an operator who has exec on the box
# can drive the product as well as recover it without installing anything.
COPY --from=builder /out/warmbly /usr/local/bin/warmbly

# Installer script the worker orchestrator uploads + runs over SSH, and serves
# at GET /worker-install.sh. The mode is explicit because COPY otherwise keeps
# the checkout's: on a filesystem without POSIX permissions that is 0700, and
# the backend runs as uid 1000, so serving the installer fails with a 500.
COPY --chmod=755 scripts/install-worker.sh /app/scripts/install-worker.sh

USER warmbly
EXPOSE 8080

# 127.0.0.1, not localhost: busybox wget tries ::1 first but the server binds IPv4.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/backend"]
