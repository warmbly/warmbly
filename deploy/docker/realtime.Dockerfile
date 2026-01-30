# Build stage
FROM elixir:1.18-alpine AS builder

RUN apk add --no-cache git build-base

WORKDIR /app

# Install hex + rebar
RUN mix local.hex --force && mix local.rebar --force

ENV MIX_ENV=prod

# Copy dependency files
COPY realtime/mix.exs realtime/mix.lock ./
RUN mix deps.get --only prod
RUN mix deps.compile

# Copy application code
COPY realtime/config config/
COPY realtime/lib lib/
COPY realtime/priv priv/

# Compile and build release
RUN mix compile
RUN mix release

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache libstdc++ openssl ncurses-libs
RUN adduser -D -u 1000 warmbly

WORKDIR /app

COPY --from=builder --chown=warmbly:warmbly /app/_build/prod/rel/realtime ./

USER warmbly
EXPOSE 4000

ENV PHX_SERVER=true

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:4000/health || exit 1

ENTRYPOINT ["/app/bin/realtime", "start"]
