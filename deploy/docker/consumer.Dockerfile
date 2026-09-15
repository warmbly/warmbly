# syntax=docker/dockerfile:1.7
#
# CGO-free by default (NATS + JSON). Build with --build-arg GO_TAGS=kafka to
# include the Kafka backend (adds librdkafka + CGO). See backend.Dockerfile.
# Builder runs on $BUILDPLATFORM and cross-compiles to $TARGETARCH (no QEMU).
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG GO_TAGS=""
ARG TARGETOS TARGETARCH
# Build identity shown in the admin panel; see internal/version.
ARG VERSION="" COMMIT="" BUILT_AT=""
RUN apk add --no-cache git ca-certificates && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache gcc musl-dev librdkafka-dev; fi

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    set -eux; \
    if echo "$GO_TAGS" | grep -qw kafka; then CGO=1; TAGS="musl kafka"; else CGO=0; TAGS=""; fi; \
    CGO_ENABLED=$CGO GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags "$TAGS" -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/consumer ./cmd/consumer

# Runtime stage
FROM alpine:3.23

ARG GO_TAGS=""
RUN apk add --no-cache ca-certificates tzdata && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache librdkafka; fi && \
    adduser -D -u 1000 warmbly

# BLOB_FS_ROOT's default mount point, owned by the user the process runs as.
# Docker seeds a fresh named volume from the image, so the directory has to
# exist here with the right owner; otherwise Docker creates the mount point
# root-owned, the non-root process cannot write to it, and the first send fails
# with "mkdir /data/blobs/emails: permission denied".
RUN mkdir -p /data/blobs && chown -R warmbly:warmbly /data

# GEODB_PATH's default directory, owned by the same user for the same reason:
# with GEODB_URL set the process writes the database here itself, and /app is
# root-owned, so without this the download fails on a directory it cannot make.
# A bind mount over it still wins, which is how an operator supplies their own.
RUN mkdir -p /app/data && chown -R warmbly:warmbly /app/data

# Amazon RDS presents a chain rooted in an RDS CA that is in no public trust
# store, so sslmode=verify-full cannot work against it from the system bundle
# alone. Shipping AWS's truststore makes verification possible for operators who
# opt in with sslrootcert=/etc/ssl/rds/global-bundle.pem; nothing here changes
# the default, because pointing every install at an RDS-only store would break
# a Postgres fronted by a public CA.
RUN mkdir -p /etc/ssl/rds && \
    wget -qO /etc/ssl/rds/global-bundle.pem \
      https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem && \
    chmod 0644 /etc/ssl/rds/global-bundle.pem

COPY --from=builder /out/consumer /app/consumer

USER warmbly

ENTRYPOINT ["/app/consumer"]
