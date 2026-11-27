# syntax=docker/dockerfile:1.7
#
# The updater is the host-side agent behind "Update and restart" in the admin
# panel: it pulls the checkout, rebuilds the images and recreates the
# containers. It needs git and the docker CLI with the compose plugin, and the
# docker socket mounted at runtime (see the updater service in
# docker-compose.yml). Builder runs on $BUILDPLATFORM and cross-compiles.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS TARGETARCH
ARG VERSION="" COMMIT="" BUILT_AT=""
RUN apk add --no-cache git ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/updater ./cmd/updater

# Runtime: the official docker CLI image already carries the compose plugin.
FROM docker:29-cli
# VERSION keys the build cache, so every release re-runs this against Alpine's current packages.
ARG VERSION
RUN apk upgrade --no-cache

RUN apk add --no-cache git ca-certificates tzdata && mkdir -p /var/lib/warmbly-updater

COPY --from=builder /out/updater /app/updater

# Runs as root on purpose: it holds the docker socket, and chowns what git
# wrote back to the checkout's owner after every update.
EXPOSE 8095

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8095/health || exit 1

ENTRYPOINT ["/app/updater"]
