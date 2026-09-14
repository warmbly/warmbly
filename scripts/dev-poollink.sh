#!/usr/bin/env bash
# Two-instance dev environment for the Warmbly Cloud pool link.
#
#   cloud  : plays the hosted product. DEPLOYMENT_MODE=cloud, Stripe billing
#            gates ON (placeholder keys, so checkout fails but plans, trials
#            and the 10-mailbox free allowance behave like production), email
#            verification through Mailpit, the Sunrise Labs sandbox org seeded
#            so the pool has real mailboxes.
#   self   : plays a brand-new self-hosted install. Nothing seeded, no
#            accounts, so the first visit is the /setup claim flow, exactly as
#            a self-hoster sees it. WARMBLY_CLOUD_URL points at the cloud.
#
# Both share the docker infra from `make infra` (postgres :15432, redis
# :16379, nats :4222, mailpit :18025, dovecot :10993) but use their own
# databases, redis db indexes and blob roots, so `make dev` keeps working
# next to them.
#
#   scripts/dev-poollink.sh up        boot both stacks (idempotent)
#   scripts/dev-poollink.sh status    ports, pids, health
#   scripts/dev-poollink.sh setup-link  print a fresh claim link for the self-hosted instance
#   scripts/dev-poollink.sh down      stop everything, keep the databases
#   scripts/dev-poollink.sh reset     stop everything and drop the databases
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
RUN=/tmp/warmbly-poollink
mkdir -p "$RUN"

HOST=${PUBLIC_HOST:-localhost}
CLOUD_API_PORT=${CLOUD_API_PORT:-18401}
CLOUD_WEB_PORT=${CLOUD_WEB_PORT:-18301}
SELF_API_PORT=${SELF_API_PORT:-18402}
SELF_WEB_PORT=${SELF_WEB_PORT:-18302}
CLOUD_DB=warmbly_cloud_dev
SELF_DB=warmbly_selfhost_dev
PG="postgres://warmbly:warmbly@localhost:15432"

# Same dev secrets the Makefile uses, so sealed values are readable by both.
common_env() {
  export APP_ENV=dev AWS_CONFIG_ENABLED=false EVENTBUS_PROVIDER=nats NATS_URL=nats://localhost:4222 CODEC_PROVIDER=json
  export KMS_PROVIDER=local KMS_LOCAL_MASTER_KEY=Xr0JA7gqF2POy29a7MRByyqddivTNt8WOyKsOXklazk=
  export CREDENTIALS_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  export BLOB_PROVIDER=filesystem TASKS_PROVIDER=local CAPTCHA_PROVIDER=none PUBSUB_ENABLED=false ENCRYPTED_KEYS_PROVIDER=postgres
  export MAIL_TRANSPORT=smtp SMTP_HOST=localhost SMTP_PORT=11025 SMTP_SECURITY=none EMAIL_NAME='Warmbly Dev' EMAIL_ADDRESS=dev@warmbly.local
  export GIN_MODE=release AUTH_SECRET=local-dev-auth-secret-minimum-32-characters-long
  export GEODB_PATH=data/GeoLite2-City.mmdb INTERNAL_API_TOKEN=local-dev-internal-token TRACKING_DOMAIN=localhost:3000
  export WEBSOCKET_URL=ws://localhost:4000/socket/websocket
}

cloud_env() {
  common_env
  export DEPLOYMENT_MODE=cloud BILLING_PROVIDER=stripe
  export STRIPE_SECRET_KEY=${STRIPE_SECRET_KEY:-sk_test_placeholder} STRIPE_WEBHOOK_SECRET=${STRIPE_WEBHOOK_SECRET:-whsec_placeholder} STRIPE_PUBLISHABLE_KEY=${STRIPE_PUBLISHABLE_KEY:-pk_test_placeholder}
  export PRIMARY_DB="$PG/$CLOUD_DB?sslmode=disable" REDIS=redis://localhost:16379/5 BLOB_FS_ROOT=/tmp/warmbly-poollink-blobs-cloud
  export API_HOST=0.0.0.0:$CLOUD_API_PORT APP_URL=http://$HOST:$CLOUD_WEB_PORT BLOB_PUBLIC_BASE_URL=http://$HOST:$CLOUD_API_PORT/public
  export CORS_ALLOW_ORIGINS=http://$HOST:$CLOUD_WEB_PORT,http://localhost:$CLOUD_WEB_PORT
  export ENV_LABEL=cloud
}

self_env() {
  common_env
  export DEPLOYMENT_MODE=self_hosted BILLING_PROVIDER=none
  export PRIMARY_DB="$PG/$SELF_DB?sslmode=disable" REDIS=redis://localhost:16379/6 BLOB_FS_ROOT=/tmp/warmbly-poollink-blobs-self
  export API_HOST=0.0.0.0:$SELF_API_PORT APP_URL=http://$HOST:$SELF_WEB_PORT BLOB_PUBLIC_BASE_URL=http://$HOST:$SELF_API_PORT/public
  export CORS_ALLOW_ORIGINS=http://$HOST:$SELF_WEB_PORT,http://localhost:$SELF_WEB_PORT
  export WARMBLY_CLOUD_URL=http://localhost:$CLOUD_API_PORT INSTANCE_NAME=${INSTANCE_NAME:-my-selfhost} WARMBLY_VERSION=dev
  export ENV_LABEL=self-hosted
}

psql_admin() { docker exec -i warmbly-postgres-1 psql -U warmbly -d postgres -v ON_ERROR_STOP=1 "$@"; }
psql_db() { docker exec -i warmbly-postgres-1 psql -U warmbly -d "$1" -v ON_ERROR_STOP=1 -tA "${@:2}"; }

need() { command -v "$1" >/dev/null || { echo "$1 is required"; exit 1; }; }

ensure_infra() {
  need docker; need go; need pnpm
  if ! docker exec warmbly-postgres-1 pg_isready -U warmbly >/dev/null 2>&1; then
    echo "The dev infra is not running. Start it first:  make infra"
    exit 1
  fi
}

ensure_db() {
  local db=$1
  if [ "$(psql_admin -tAc "select 1 from pg_database where datname='$db'")" != "1" ]; then
    psql_admin -c "CREATE DATABASE $db" >/dev/null
    echo "created database $db"
  fi
}

start_bg() { # name, logfile, command...
  local name=$1 log=$2; shift 2
  if [ -f "$RUN/$name.pid" ] && kill -0 "$(cat "$RUN/$name.pid")" 2>/dev/null; then
    echo "$name already running (pid $(cat "$RUN/$name.pid"))"; return
  fi
  setsid nohup "$@" >"$log" 2>&1 < /dev/null &
  echo $! > "$RUN/$name.pid"
  echo "$name started (pid $!), log $log"
}

wait_health() { # url, label
  local i
  for i in $(seq 1 60); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' "$1/health")" = 200 ]; then return 0; fi
    sleep 1
  done
  echo "$2 did not become healthy; see the log"; return 1
}

build_backend() {
  ( cd "$ROOT" && go build -o "$RUN/backend" ./cmd/backend )
}

cmd_up() {
  ensure_infra
  cd "$ROOT"
  ensure_db "$CLOUD_DB"; ensure_db "$SELF_DB"
  [ -d web/node_modules ] || (cd web && pnpm install)

  echo "== migrating"
  ( cloud_env; go run ./cmd/migrate >/dev/null ); ( self_env; go run ./cmd/migrate >/dev/null )

  if [ "$(psql_db "$CLOUD_DB" -c "select count(*) from users")" = "0" ]; then
    echo "== seeding the cloud with the Sunrise Labs sandbox org (pool mailboxes)"
    ( cloud_env; go run ./cmd/sandbox -seed-only >/dev/null )
  fi

  echo "== building backend"
  build_backend

  ( cloud_env; start_bg cloud-api "$RUN/cloud-api.log" "$RUN/backend" )
  ( self_env;  start_bg self-api  "$RUN/self-api.log"  "$RUN/backend" )
  wait_health "http://localhost:$CLOUD_API_PORT" "cloud backend"
  wait_health "http://localhost:$SELF_API_PORT" "self-hosted backend"

  ( cd web && start_bg cloud-web "$RUN/cloud-web.log" env VITE_APP_URL=http://$HOST:$CLOUD_WEB_PORT VITE_API_URL=http://$HOST:$CLOUD_API_PORT VITE_TURNSTILE_KEY=1x00000000000000000000AA VITE_TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass pnpm dev --port "$CLOUD_WEB_PORT" --strictPort ${PUBLIC_HOST:+--host 0.0.0.0} )
  ( cd web && start_bg self-web  "$RUN/self-web.log"  env VITE_APP_URL=http://$HOST:$SELF_WEB_PORT  VITE_API_URL=http://$HOST:$SELF_API_PORT  VITE_TURNSTILE_KEY=1x00000000000000000000AA VITE_TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass pnpm dev --port "$SELF_WEB_PORT"  --strictPort ${PUBLIC_HOST:+--host 0.0.0.0} )

  echo
  cmd_status
  echo
  echo "Walkthrough (the self-hoster's first minute):"
  echo "  1. Claim the self-hosted instance with the setup link below (or: scripts/dev-poollink.sh setup-link)."
  echo "  2. Connect a mailbox there: SMTP localhost:11025 (no auth, Mailpit), IMAP localhost:10993 TLS,"
  echo "     any user like you@sunrise.test with password 'sandbox' (Dovecot accepts every user)."
  echo "  3. Settings > Warmbly Cloud > Connect. Open the link it shows: that is the 'prod cloud' at http://$HOST:$CLOUD_WEB_PORT,"
  echo "     where you register a new account (verification mail lands in Mailpit http://localhost:18025), then approve the code."
  echo "  4. Back on the instance, enroll the mailbox. The cloud workspace is on a free trial with Stripe gates on,"
  echo "     so the 10-mailbox allowance and the upgrade card behave like production (checkout itself needs real keys)."
  echo "     Existing cloud login with a full pool: sandbox@warmbly.test / password123 (Sunrise Labs)."
  echo
  cmd_setup_link
}

cmd_setup_link() {
  cd "$ROOT"
  ( self_env; go run ./cmd/warmblyctl setup-link 2>&1 ) | sed 's/^/  /'
}

cmd_status() {
  local n
  for n in cloud-api self-api cloud-web self-web; do
    if [ -f "$RUN/$n.pid" ] && kill -0 "$(cat "$RUN/$n.pid")" 2>/dev/null; then
      printf '  %-10s running (pid %s)\n' "$n" "$(cat "$RUN/$n.pid")"
    else
      printf '  %-10s stopped\n' "$n"
    fi
  done
  echo
  echo "  cloud (prod-like):  dashboard http://$HOST:$CLOUD_WEB_PORT   api http://localhost:$CLOUD_API_PORT   db $CLOUD_DB"
  echo "  self-hosted:        dashboard http://$HOST:$SELF_WEB_PORT   api http://localhost:$SELF_API_PORT   db $SELF_DB"
  echo "  mailpit:            http://localhost:18025      logs: $RUN/*.log"
}

cmd_down() {
  local n
  for n in cloud-web self-web cloud-api self-api; do
    if [ -f "$RUN/$n.pid" ]; then
      local pid; pid=$(cat "$RUN/$n.pid")
      kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
      rm -f "$RUN/$n.pid"
      echo "$n stopped"
    fi
  done
}

cmd_reset() {
  cmd_down
  psql_admin -c "DROP DATABASE IF EXISTS $CLOUD_DB" -c "DROP DATABASE IF EXISTS $SELF_DB" >/dev/null
  rm -rf /tmp/warmbly-poollink-blobs-cloud /tmp/warmbly-poollink-blobs-self
  docker exec warmbly-redis-1 redis-cli -n 5 flushdb >/dev/null; docker exec warmbly-redis-1 redis-cli -n 6 flushdb >/dev/null
  echo "databases dropped"
}

case "${1:-up}" in
  up) cmd_up ;;
  status) cmd_status ;;
  setup-link) cmd_setup_link ;;
  down) cmd_down ;;
  reset) cmd_reset ;;
  *) echo "usage: $0 up|status|setup-link|down|reset"; exit 1 ;;
esac
