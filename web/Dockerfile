# syntax=docker/dockerfile:1

# Stage 1: build the static dashboard once on the build platform. The output is
# plain JS/CSS/HTML (arch-independent), so both target arches reuse it and the
# heavy pnpm build runs only once.
FROM --platform=$BUILDPLATFORM node:22-alpine AS build
WORKDIR /app
# No TTY in a build: CI=true makes pnpm reinstall instead of prompting.
ENV CI=true
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
# The build identity, the same VERSION/COMMIT the Go images take. This is the
# one VITE_* that must be baked: it tags every error event with the build, and
# it has to match the release the source maps were uploaded under, which a
# container variable set after the bundle was built could not. Every other
# setting comes from the runtime config below, so one image still serves any
# deployment.
ARG VERSION=""
ARG COMMIT=""
# Source-map upload is optional and off unless CI passes a whole backend's
# worth of credentials. A fork or a self-host build sets none of them, needs no
# account anywhere, and ships no source maps.
#
# PostHog's upload runs after the build because it is a CLI rather than a Vite
# plugin: it rewrites the built files with a chunk id, sends them, and deletes
# the .map files it sent. The `if` is what keeps a fork's build from reaching
# for a CLI it has no credentials for.
ARG SENTRY_ORG=""
ARG SENTRY_PROJECT=""
ARG POSTHOG_CLI_PROJECT_ID=""
ARG POSTHOG_CLI_HOST=""
RUN --mount=type=secret,id=sentry_auth_token,required=false \
    --mount=type=secret,id=posthog_cli_api_key,required=false \
    export VITE_SENTRY_RELEASE="${VERSION:-$COMMIT}" && \
    export SENTRY_ORG="$SENTRY_ORG" && \
    export SENTRY_PROJECT="$SENTRY_PROJECT" && \
    export SENTRY_AUTH_TOKEN="$(cat /run/secrets/sentry_auth_token 2>/dev/null || true)" && \
    export POSTHOG_CLI_PROJECT_ID="$POSTHOG_CLI_PROJECT_ID" && \
    export POSTHOG_CLI_HOST="$POSTHOG_CLI_HOST" && \
    export POSTHOG_CLI_API_KEY="$(cat /run/secrets/posthog_cli_api_key 2>/dev/null || true)" && \
    pnpm build && \
    if [ -n "$POSTHOG_CLI_PROJECT_ID" ] && [ -n "$POSTHOG_CLI_API_KEY" ]; then \
        pnpm sourcemaps:posthog; \
    fi

# Stage 2: serve the built SPA from nginx with a history fallback. The
# entrypoint renders /config.js from container env at startup.
FROM nginx:1.30-alpine
# VERSION keys the build cache, so every release re-runs this against Alpine's current packages.
ARG VERSION
RUN apk upgrade --no-cache
RUN apk add --no-cache curl jq
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx-security-headers.conf /etc/nginx/warmbly-security-headers.conf
COPY nginx-mail-preview-headers.conf /etc/nginx/warmbly-mail-preview-headers.conf
RUN nginx -t
COPY --from=build /app/dist /usr/share/nginx/html
# public/ files keep their checkout mode through the build; on a filesystem
# without POSIX permissions (exFAT/NTFS mounts) that is 0700 and nginx 403s.
RUN chmod -R a+rX /usr/share/nginx/html
COPY docker-entrypoint.sh /docker-entrypoint.d/40-warmbly-config.sh
RUN chmod +x /docker-entrypoint.d/40-warmbly-config.sh
EXPOSE 80
