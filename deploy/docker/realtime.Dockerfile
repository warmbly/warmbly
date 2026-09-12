# Build stage
FROM elixir:1.18-otp-26-alpine AS builder

RUN apk add --no-cache git build-base

WORKDIR /app

# Install hex + rebar
RUN mix local.hex --force && mix local.rebar --force

ENV MIX_ENV=prod

# Copy dependency files
COPY realtime/mix.exs ./
COPY realtime/mix.lock ./
RUN mix deps.get --only prod
RUN mix deps.compile

# Copy application code
COPY realtime/config config/
COPY realtime/lib lib/

# Compile and build release
RUN mix compile
RUN mix release

# Runtime stage. The alpine version must be at least the builder image's, or
# the crypto NIF fails to load against an older libcrypto (missing symbols).
FROM alpine:3.23

RUN apk add --no-cache libstdc++ openssl ncurses-libs wget
RUN adduser -D -u 1000 warmbly

# Amazon RDS presents a chain rooted in an RDS CA that is in no public trust
# store, and Postgrex verifies the peer against the system store by default.
# Shipping AWS's truststore lets an operator opt in with
# DATABASE_SSL_CA_FILE=/etc/ssl/rds/global-bundle.pem; nothing here changes the
# default, because pointing every install at an RDS-only store would break a
# Postgres fronted by a public CA.
RUN mkdir -p /etc/ssl/rds && \
    wget -qO /etc/ssl/rds/global-bundle.pem \
      https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem && \
    chmod 0644 /etc/ssl/rds/global-bundle.pem

WORKDIR /app

COPY --from=builder --chown=warmbly:warmbly /app/_build/prod/rel/realtime ./

USER warmbly
EXPOSE 4000

ENV PHX_SERVER=true

# Build identity, the same VERSION/COMMIT the Go images take. Read at runtime
# by config/runtime.exs and used only to tag error events.
ARG VERSION=""
ARG COMMIT=""
ENV WARMBLY_RELEASE=${VERSION:-${COMMIT}}

# 127.0.0.1, not localhost: busybox wget tries ::1 first but the server binds IPv4.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:4000/health || exit 1

ENTRYPOINT ["/app/bin/realtime", "start"]
