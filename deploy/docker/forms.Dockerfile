# syntax=docker/dockerfile:1.7
#
# The forms service: the public face of hosted lead-capture forms. The React
# (TanStack) app in forms/ builds to static files; the Go binary serves them,
# the per-form page shells and the same-origin submit API. Always CGO-free —
# no event bus, no database, no cache; the backend's internal API is its only
# dependency.
# Builders run on $BUILDPLATFORM and cross-compile / emit static assets for
# $TARGETARCH (no QEMU).
FROM --platform=$BUILDPLATFORM node:22-alpine AS appbuilder
WORKDIR /app
# No TTY in a build: CI=true makes pnpm reinstall instead of prompting.
ENV CI=true
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY forms/package.json forms/pnpm-lock.yaml forms/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY forms/ .
# Source-map upload to PostHog is optional and off unless CI passes the CLI
# credentials, exactly as the dashboard image does it: the CLI injects a chunk
# id into the built files, uploads the maps and deletes them. A fork or a
# self-host build sets none of this and ships no source maps.
ARG POSTHOG_CLI_PROJECT_ID=""
ARG POSTHOG_CLI_HOST=""
RUN --mount=type=secret,id=posthog_cli_api_key,required=false \
    export POSTHOG_CLI_PROJECT_ID="$POSTHOG_CLI_PROJECT_ID" && \
    export POSTHOG_CLI_HOST="$POSTHOG_CLI_HOST" && \
    export POSTHOG_CLI_API_KEY="$(cat /run/secrets/posthog_cli_api_key 2>/dev/null || true)" && \
    pnpm build && \
    if [ -n "$POSTHOG_CLI_PROJECT_ID" ] && [ -n "$POSTHOG_CLI_API_KEY" ]; then \
        pnpm sourcemaps:posthog; \
    fi

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS TARGETARCH
# Build identity shown in the admin panel; see internal/version.
ARG VERSION="" COMMIT="" BUILT_AT=""
RUN apk add --no-cache git ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/forms ./cmd/forms

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 warmbly

COPY --from=builder /out/forms /app/forms
COPY --from=appbuilder /app/dist /app/static
ENV FORMS_STATIC_DIR=/app/static

USER warmbly
EXPOSE 8090

# 127.0.0.1, not localhost: busybox wget tries ::1 first but the server binds IPv4.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8090/health || exit 1

ENTRYPOINT ["/app/forms"]
