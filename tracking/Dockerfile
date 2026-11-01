# syntax=docker/dockerfile:1.7
#
# Default build is NATS-only (pure Rust, rustls TLS): no rdkafka/librdkafka, so
# it skips the slow librdkafka source compile and needs none of the cmake /
# openssl / curl / sasl build deps. Cargo cache mounts keep the registry +
# target caches warm across builds.
#
# Build with --build-arg CARGO_FEATURES=kafka to include the Kafka + Avro
# publisher (re-adds the librdkafka build chain; slower).
# The floor is set by the dependency tree, not by anything this crate uses: the
# AWS SDK declares rust-version 1.94.1. CI builds with `stable`, so it cannot
# catch a builder that has fallen behind; only the image build does, and it does
# so as a release failure. Raise this when `cargo build` here says a package is
# not supported.
FROM rust:1.96-alpine AS builder

ARG CARGO_FEATURES=""
RUN apk add --no-cache musl-dev && \
    if echo "$CARGO_FEATURES" | grep -qw kafka; then \
      apk add --no-cache cmake make gcc g++ pkgconfig openssl-dev openssl-libs-static curl-dev zlib-static; \
    fi

WORKDIR /app
COPY Cargo.toml Cargo.lock* ./
COPY src ./src
# include_str! reads static/tracking.js and scanner-networks.txt at compile
# time, so both must be in the context.
COPY static ./static
COPY scanner-networks.txt ./scanner-networks.txt

# The binary is copied out of the (cache-mounted) target dir before the mount is
# released, so the runtime stage can COPY it.
RUN --mount=type=cache,id=cargo-registry,target=/usr/local/cargo/registry \
    --mount=type=cache,id=cargo-target,target=/app/target \
    set -eux; \
    if [ -n "$CARGO_FEATURES" ]; then FEAT="--features $CARGO_FEATURES"; else FEAT=""; fi; \
    export OPENSSL_STATIC=1; \
    cargo build --release $FEAT; \
    cp target/release/tracking /tracking

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates libgcc && \
    addgroup -g 1000 tracking && \
    adduser -u 1000 -G tracking -s /bin/sh -D tracking

WORKDIR /app
COPY --from=builder /tracking /app/tracking

# Build identity, the same VERSION/COMMIT the Go images take. Read at runtime
# and used only to tag error events, so it needs no rebuild of the binary.
ARG VERSION=""
ARG COMMIT=""
ENV WARMBLY_RELEASE=${VERSION:-${COMMIT}}

USER tracking
EXPOSE 3000

# 127.0.0.1, not localhost: busybox wget tries ::1 first but the server binds IPv4.
# SERVE_TLS adds a TLS listener and keeps this plain one, on loopback at least.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:3000/health || exit 1

CMD ["/app/tracking"]
