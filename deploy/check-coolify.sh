#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
compose=(docker compose --env-file /dev/null -f deploy/docker-compose.coolify.yml)

# Disposable interpolation fixtures, never deployment credentials.
export AUTH_SECRET=coolify-validation-auth-secret-at-least-32-characters
export INTERNAL_API_TOKEN=coolify-validation-edge-token
export NODE_BROKER_TOKEN=coolify-validation-worker-token
export SECRET_KEY_BASE=coolify-validation-phoenix-secret-at-least-sixty-four-characters-long
export KMS_LOCAL_MASTER_KEY=Y29vbGlmeS12YWxpZGF0aW9uLW9ubHktMzItYnl0ZXM=
export CREDENTIALS_ENCRYPTION_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export POSTGRES_PASSWORD=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
export APP_URL=https://app.example.com
export ADMIN_URL=https://admin.example.com
export API_PUBLIC_URL=https://api.example.com
export REALTIME_HOST=realtime.example.com
export TRACKING_DOMAIN=track.example.com
export FORMS_DOMAIN=forms.example.com
export WARMBLY_TAG=coolify-validation

config=$("${compose[@]}" config --format json)
check() {
    if ! jq -e "$1" >/dev/null <<<"$config"; then
        printf 'Coolify check failed: %s\n' "$2" >&2
        exit 1
    fi
}

check '(.services | keys) == ["admin", "backend", "consumer", "forms", "nats", "portal", "postgres", "realtime", "redis", "tracking", "web", "worker"]' 'complete stack'
check 'all(.services[]; (has("build") or has("ports") or has("env_file") or has("container_name") or has("privileged")) | not)' 'no builds, published ports, external env files, fixed container names, or privileges'
check 'all(.services[].volumes[]?; .type == "volume")' 'named volumes only, no host or Docker socket mounts'
check '(.volumes | keys) == ["blobs", "nats_data", "postgres_data", "redis_data", "worker_state"]' 'all persistent stores'
check '. as $config | all(["backend", "consumer", "worker", "forms", "portal", "tracking", "realtime", "web", "admin"][]; $config.services[.].image == "ghcr.io/warmbly/warmbly/" + . + ":coolify-validation")' 'matching service images with one release tag'
check 'all([.services.backend, .services.consumer, .services.worker, .services.tracking, .services.forms, .services.realtime][]; .environment.APP_ENV == "prod")' 'production environment'
check '.services.backend.environment.AUTH_SECRET == .services.realtime.environment.JWT_SECRET' 'shared JWT secret'
check '.services.backend.environment.NODE_BROKER_TOKEN == .services.worker.environment.ENCRYPTED_KEYS_WORKER_TOKEN and .services.tracking.environment.INTERNAL_API_TOKEN == .services.backend.environment.INTERNAL_API_TOKEN and .services.forms.environment.INTERNAL_API_TOKEN == .services.backend.environment.INTERNAL_API_TOKEN' 'separate worker and edge tokens'
check '.services.portal.environment.PORTAL_EDGE_TOKEN == .services.backend.environment.PORTAL_EDGE_TOKEN and .services.portal.environment.PORTAL_DOMAIN == .services.backend.environment.PORTAL_DOMAIN and (.services.portal.environment | has("INTERNAL_API_TOKEN") or has("NODE_BROKER_TOKEN") | not)' 'portal holds only its own token'
check '.services.worker.environment.MAIL_TLS_INSECURE == "false" and .services.worker.environment.ENCRYPTED_KEYS_PROVIDER == "http" and (.services.worker.environment | has("PRIMARY_DB") | not)' 'worker TLS and database boundary'
check '.services.postgres.environment.POSTGRES_PASSWORD as $password | .services.backend.environment.PRIMARY_DB == .services.consumer.environment.PRIMARY_DB and .services.backend.environment.PRIMARY_DB == .services.realtime.environment.DATABASE_URL and (.services.backend.environment.PRIMARY_DB | contains($password))' 'database clients share credentials'
check '.services.backend.environment.UPDATER_URL == "none" and (.services | has("updater") | not)' 'Coolify owns updates'
check '.services.backend.environment.WEBSOCKET_URL == "wss://realtime.example.com/socket/websocket" and .services.web.environment.WEBSOCKET_URL == .services.backend.environment.WEBSOCKET_URL and .services.realtime.environment.CHECK_ORIGIN_HOSTS == "https://app.example.com,https://admin.example.com"' 'secure realtime URL and browser origins'
check '.services.web.environment.WARMBLY_API_URL == "https://api.example.com" and .services.admin.environment.WARMBLY_API_URL == "https://api.example.com"' 'runtime frontend API URLs'
check '.services.backend.environment.MAIL_TRANSPORT == .services.consumer.environment.MAIL_TRANSPORT' 'platform mail reaches notifications'
check 'all([.services.backend, .services.consumer][]; .environment.GEODB_PATH == "/data/blobs/geo/GeoLite2-City.mmdb")' 'GeoIP path configured without requiring an external database'
check 'all(["backend", "tracking", "realtime", "forms", "portal", "web", "admin"][] as $name | .services[$name].healthcheck.test; any(.[]; contains("http://127.0.0.1:")))' 'HTTP probes use the container IPv4 loopback'
check 'all(["consumer", "worker", "tracking", "realtime", "forms", "portal", "web", "admin"][] as $name | .services[$name].depends_on.backend.condition; . == "service_healthy")' 'wait for backend migrations'

for required in AUTH_SECRET INTERNAL_API_TOKEN NODE_BROKER_TOKEN SECRET_KEY_BASE KMS_LOCAL_MASTER_KEY CREDENTIALS_ENCRYPTION_KEY POSTGRES_PASSWORD APP_URL ADMIN_URL API_PUBLIC_URL REALTIME_HOST TRACKING_DOMAIN FORMS_DOMAIN; do
    if env -u "$required" "${compose[@]}" config --quiet >/dev/null 2>&1; then
        printf 'Coolify accepted missing required variable: %s\n' "$required" >&2
        exit 1
    fi
    if env "$required=" "${compose[@]}" config --quiet >/dev/null 2>&1; then
        printf 'Coolify accepted empty required variable: %s\n' "$required" >&2
        exit 1
    fi
done

printf 'Coolify Compose configuration checks passed.\n'
