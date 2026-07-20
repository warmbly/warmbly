# syntax=docker/dockerfile:1.7
#
# Default build is NATS-only (pure Rust, rustls TLS): no rdkafka/librdkafka, so
# it skips the slow librdkafka source compile and needs none of the cmake /
# openssl / curl / sasl build deps. Cargo cache mounts keep the registry +
# target caches warm across builds.
#
# Build with --build-arg CARGO_FEATURES=kafka to include the Kafka + Avro
# publisher (re-adds the librdkafka build chain; slower).
FROM rust:1.93-alpine AS builder

ARG CARGO_FEATURES=""
RUN apk add --no-cache musl-dev && \
    if echo "$CARGO_FEATURES" | grep -qw kafka; then \
      apk add --no-cache cmake make gcc g++ pkgconfig openssl-dev openssl-libs-static curl-dev zlib-static; \
    fi

WORKDIR /app
COPY Cargo.toml Cargo.lock* ./
COPY src ./src

# The binary is copied out of the (cache-mounted) target dir before the mount is
# released, so the runtime stage can COPY it.
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/app/target \
    set -eux; \
    if [ -n "$CARGO_FEATURES" ]; then FEAT="--features $CARGO_FEATURES"; else FEAT=""; fi; \
    cargo build --release $FEAT; \
    cp target/release/tracking /tracking

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates libgcc && \
    addgroup -g 1000 tracking && \
    adduser -u 1000 -G tracking -s /bin/sh -D tracking

WORKDIR /app
COPY --from=builder /tracking /app/tracking

USER tracking
EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/health || exit 1

CMD ["/app/tracking"]
