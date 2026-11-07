#!/usr/bin/env bash
# This worktree's own Warmbly stack, isolated from every other stack on the machine:
# its own database, Redis, NATS and Mailpit, so no cache, event or login code crosses over.
#
#   stack.sh up [lite|full|sandbox]  start (the mode is remembered; lite on first run)
#   stack.sh down                    stop everything; the data stays
#   stack.sh restart                 rebuild the Go binaries and start again
#   stack.sh status                  what runs, where, and how much memory it holds
#   stack.sh logs [name]             follow the logs (backend, web, consumer, worker, simulator)
#   stack.sh reset-data [mode]       drop the database and start again with fresh seed data
#   stack.sh ls                      every QA stack on this machine
#
# Modes:
#   lite     backend + dashboard on the rich seed (SEED_RICH + SEED_FULL), dev@warmbly.com
#   full     lite + consumer, worker and realtime: sends, syncs and live updates work
#   sandbox  full + tracking, Dovecot and the simulator on the Sunrise Labs sandbox seed
#            (sandbox@warmbly.test): live mailboxes, opens, clicks and replies
#
# The stack stops itself after QA_STACK_IDLE_MIN minutes (default 60) with no recording; 0 keeps it up.
set -euo pipefail

QA=$(cd "$(dirname "$0")/.." && pwd)
REPO=$(cd "$QA/.." && pwd)
RUN=$QA/.artifacts/stack
BIN=$RUN/bin
mkdir -p "$RUN"

slug=$(basename "$REPO" | tr -c '[:alnum:]\n' '_' | tr '[:upper:]' '[:lower:]')
DB=${QA_DB:-warmbly_qa_$slug}
LABEL=warmbly-qa=$slug
IDLE_MIN=${QA_STACK_IDLE_MIN:-60}
REALTIME_IMAGE=${QA_REALTIME_IMAGE:-ghcr.io/warmbly/warmbly/realtime:prod}
TRACKING_IMAGE=${QA_TRACKING_IMAGE:-ghcr.io/warmbly/warmbly/tracking:prod}
AUTH_SECRET=local-dev-auth-secret-minimum-32-characters-long
INTERNAL_TOKEN=local-dev-internal-token
MODE=lite

say() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
alive() { [ -s "$RUN/$1.pid" ] && kill -0 "$(cat "$RUN/$1.pid")" 2>/dev/null; }
psql_() { docker exec warmbly-postgres-1 psql -U warmbly "$@"; }
cname() { echo "wqa-$slug-$1"; }
NET=wqa-$slug
running() { [ "$(docker inspect -f '{{.State.Running}}' "$(cname "$1")" 2>/dev/null)" = true ]; }

listening() {
  if command -v ss >/dev/null; then ss -ltn | awk '{print $4}' | grep -qE ":$1\$"
  else lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; fi
}

has() { # does the current mode include this tier
  case "$1" in
    full) [ "$MODE" = full ] || [ "$MODE" = sandbox ] ;;
    sandbox) [ "$MODE" = sandbox ] ;;
  esac
}

# ── ports ────────────────────────────────────────────────────────────────
PORT_KEYS="API WEB REDIS NATS SMTP MAILPIT RT TRACK IMAPS VENDOR"
base_of() {
  case "$1" in
    API) echo 18180 ;; WEB) echo 15280 ;; REDIS) echo 16480 ;; NATS) echo 14322 ;;
    SMTP) echo 11125 ;; MAILPIT) echo 18125 ;; RT) echo 14100 ;; TRACK) echo 13100 ;;
    IMAPS) echo 10994 ;; VENDOR) echo 18199 ;;
  esac
}

assigned() { # port already given to another key of this stack
  local k
  for k in $PORT_KEYS; do [ "${!k:-}" = "$1" ] && return 0; done
  return 1
}

pick() { # first port at or above $1 that is free and not assigned here
  local p=$1
  while listening "$p" || assigned "$p"; do p=$((p + 1)); done
  echo "$p"
}

save_ports() {
  local k
  : >"$RUN/ports"
  for k in $PORT_KEYS; do echo "$k=${!k}" >>"$RUN/ports"; done
}

load_ports() {
  local k changed=""
  # shellcheck disable=SC1091
  [ -s "$RUN/ports" ] && . "$RUN/ports"
  for k in $PORT_KEYS; do
    if [ -z "${!k:-}" ]; then printf -v "$k" '%s' "$(pick "$(base_of "$k")")"; changed=1; fi
  done
  [ -n "$changed" ] && save_ports
  return 0
}

# A port another process took while the stack was down is reassigned rather than fought over.
heal_ports() {
  local k new
  for k in $PORT_KEYS; do
    if listening "${!k}"; then
      printf -v "$k" '%s' ""
      new=$(pick "$(base_of "$k")")
      warn "a port for $k was taken by another process; using $new"
      printf -v "$k" '%s' "$new"
    fi
  done
  save_ports
}

db_exists() { psql_ -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$DB'" 2>/dev/null | grep -q 1; }
db_seeded() { db_exists && [ "$(psql_ -d "$DB" -tAc "SELECT count(*) FROM users" 2>/dev/null || echo 0)" != 0 ]; }

load_mode() {
  local saved
  saved=$(cat "$RUN/mode" 2>/dev/null || true)
  MODE=${1:-${saved:-lite}}
  case "$MODE" in lite | full | sandbox) ;; *) die "unknown mode $MODE (lite, full, sandbox)" ;; esac
  # lite and full share a seed; the sandbox seed is a different organization and account.
  if [ -n "$saved" ] && [ "$saved" != "$MODE" ] && { [ "$saved" = sandbox ] || [ "$MODE" = sandbox ]; } && db_seeded; then
    die "$DB holds the $saved seed; $MODE needs a fresh one: stack.sh reset-data $MODE"
  fi
}

# ── commands derived from the Makefile, so the env stays in step with `make dev` ─
rewrite() {
  local mock=""
  has sandbox && mock="http://127.0.0.1:$VENDOR"
  sed \
    -e "s#/warmbly_dev?#/$DB?#g" \
    -e "s#API_HOST=0.0.0.0:8080#API_HOST=0.0.0.0:$API#" \
    -e "s#localhost:8080#localhost:$API#g" \
    -e "s#APP_URL=http://localhost:5173#APP_URL=http://localhost:$WEB#" \
    -e "s#CORS_ALLOW_ORIGINS= #CORS_ALLOW_ORIGINS=http://localhost:$WEB #" \
    -e "s#redis://localhost:16379#redis://localhost:$REDIS#g" \
    -e "s#nats://localhost:4222#nats://localhost:$NATS#g" \
    -e "s#SMTP_PORT=11025#SMTP_PORT=$SMTP#g" \
    -e "s#ws://localhost:4000#ws://localhost:$RT#g" \
    -e "s#TRACKING_DOMAIN=localhost:3000#TRACKING_DOMAIN=localhost:$TRACK#g" \
    -e "s#BLOB_FS_ROOT=/tmp/warmbly-blobs#BLOB_FS_ROOT=$RUN/blobs#g" \
    -e "s#MAILVENDOR_SANDBOX_URL=[^ ]*#MAILVENDOR_SANDBOX_URL=$mock#" \
    -e "s#go run ./cmd/\\([a-z]*\\)#$BIN/\\1#g"
}

make_cmd() { (cd "$REPO" && make -s -n "$1") | rewrite; }

# QA_STRIPE_SECRET_KEY, a Stripe test-mode key, bills through Stripe instead of nothing; the keys
# reach the backend through the environment and are never written to the run directory.
billing() {
  if [ -z "${QA_STRIPE_SECRET_KEY:-}" ]; then cat; return; fi
  sed 's#BILLING_PROVIDER=none#BILLING_PROVIDER=stripe#'
}
export_stripe() {
  [ -n "${QA_STRIPE_SECRET_KEY:-}" ] || return 0
  case "$QA_STRIPE_SECRET_KEY" in sk_test_* | rk_test_* | rkcs_test_*) ;; *) die "QA_STRIPE_SECRET_KEY must be a Stripe test-mode key" ;; esac
  export STRIPE_SECRET_KEY="$QA_STRIPE_SECRET_KEY"
  export STRIPE_WEBHOOK_SECRET="${QA_STRIPE_WEBHOOK_SECRET:-whsec_qa_unused}"
  export STRIPE_PUBLISHABLE_KEY="${QA_STRIPE_PUBLISHABLE_KEY:-pk_test_qa_unused}"
}

# The sandbox reads its own endpoints from the environment (internal/sandbox/config.go).
sandbox_env() {
  echo "MAILPIT_URL=http://localhost:$MAILPIT TRACKING_URL=http://localhost:$TRACK DOVECOT_IMAP_ADDR=localhost:$IMAPS" \
    "SANDBOX_SMTP_PORT=$SMTP SANDBOX_IMAP_PORT=$IMAPS SANDBOX_VENDOR_ADDR=127.0.0.1:$VENDOR"
}

start() { # name, command
  rm -f "$RUN/$1.pid"
  (cd "$REPO" && setsid -f bash -c "echo \$\$ >'$RUN/$1.pid'; $2" >"$RUN/$1.log" 2>&1 </dev/null)
  for _ in $(seq 1 50); do [ -s "$RUN/$1.pid" ] && return 0; sleep 0.1; done
  die "$1 did not start; see $RUN/$1.log"
}

wait_for() { # name, check, seconds
  for _ in $(seq 1 "$3"); do
    if [ -s "$RUN/$1.pid" ] && ! alive "$1"; then echo "$1 exited:"; tail -30 "$RUN/$1.log"; exit 1; fi
    eval "$2" >/dev/null 2>&1 && return 0
    sleep 1
  done
  die "$1 was not ready within $3s (logs: stack.sh logs $1, or docker logs $(cname "$1"))"
}

stop_proc() {
  local pid
  if alive "$1"; then
    pid=$(cat "$RUN/$1.pid")
    kill -TERM -- "-$pid" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
    kill -KILL -- "-$pid" 2>/dev/null || true
  fi
  rm -f "$RUN/$1.pid"
}

container() { # name, docker run args...
  local name=$1
  shift
  running "$name" && return 0
  docker rm -f "$(cname "$name")" >/dev/null 2>&1 || true
  docker run -d --name "$(cname "$name")" --network "$NET" --label "$LABEL" \
    --add-host host.docker.internal:host-gateway "$@" >/dev/null
}

# Warns when the image was built before the last commit to its source, so a recording never shows stale code silently.
check_image() { # image, source dir
  local built changed
  built=$(docker inspect -f '{{.Created}}' "$1" 2>/dev/null) || die "image $1 is missing: cd $REPO && docker compose -p warmbly build ${2%/}"
  changed=$(cd "$REPO" && git log -1 --format=%cI -- "$2")
  if [ -n "$changed" ] && [ "$(date -d "$changed" +%s)" -gt "$(date -d "$built" +%s)" ]; then
    warn "$1 was built before the last change to $2; rebuild: cd $REPO && docker compose -p warmbly build ${2%/}"
  fi
}

build_bins() {
  local cmds=(./cmd/backend ./cmd/seed)
  has full && cmds+=(./cmd/consumer ./cmd/worker)
  has sandbox && cmds+=(./cmd/sandbox)
  say "building ${cmds[*]##*/} (one go build, cached between runs)"
  mkdir -p "$BIN"
  (cd "$REPO" && go build -o "$BIN/" "${cmds[@]}") || die "go build failed"
}

account() { if has sandbox; then echo sandbox@warmbly.test; else echo dev@warmbly.com; fi; }

write_env() {
  cat >"$RUN/env.json" <<JSON
{
  "mode": "$MODE",
  "webURL": "http://localhost:$WEB",
  "apiURL": "http://localhost:$API",
  "mailpitURL": "http://localhost:$MAILPIT",
  "email": "$(account)",
  "password": "password123",
  "database": "$DB"
}
JSON
}

# Stops the stack once nothing has used it for IDLE_MIN minutes, so a forgotten stack frees its memory.
start_watchdog() {
  [ "$IDLE_MIN" = 0 ] && return 0
  date -Is >"$RUN/last-used"
  start watchdog "while sleep 60; do
    last=\$(stat -c %Y '$RUN/last-used' 2>/dev/null || echo 0)
    if [ \$(( \$(date +%s) - last )) -ge $((IDLE_MIN * 60)) ]; then
      echo \"unused for $IDLE_MIN min, stopping\"; rm -f '$RUN/watchdog.pid'; exec bash '$QA/scripts/stack.sh' down
    fi
  done"
}

seed() {
  if has sandbox; then
    (cd "$REPO" && eval "$(sandbox_env) $(make_cmd sandbox-seed | grep "$BIN/sandbox")")
  else
    (cd "$REPO" && eval "$(make_cmd seed)")
  fi
}

# ── lifecycle ────────────────────────────────────────────────────────────
up() {
  local c
  for c in docker go pnpm setsid curl; do command -v "$c" >/dev/null || die "$c is required"; done
  load_ports
  load_mode "${1:-}"
  if alive backend && alive web; then
    if [ "$(cat "$RUN/mode" 2>/dev/null)" = "$MODE" ]; then status; return 0; fi
    say "switching to $MODE"
    down >/dev/null
  fi
  heal_ports
  echo "$MODE" >"$RUN/mode"

  docker ps --format '{{.Names}}' | grep -q '^warmbly-postgres-1$' || (cd "$REPO" && make infra)
  until docker exec warmbly-postgres-1 pg_isready -U warmbly >/dev/null 2>&1; do sleep 1; done
  db_exists || { say "creating database $DB"; psql_ -d postgres -c "CREATE DATABASE $DB" >/dev/null; }
  [ -d "$REPO/web/node_modules" ] || { say "installing web deps"; (cd "$REPO/web" && pnpm install --frozen-lockfile); }
  mkdir -p "$RUN/blobs"
  build_bins

  # Containers reach each other by name on the stack's network; the host reaches them on loopback ports.
  docker network inspect "$NET" >/dev/null 2>&1 || docker network create --label "$LABEL" "$NET" >/dev/null
  say "starting redis, nats and mailpit (private to this stack)"
  container redis -p "127.0.0.1:$REDIS:6379" redis:7-alpine redis-server --save "" --appendonly no
  container nats -p "127.0.0.1:$NATS:4222" nats:2.10-alpine -js
  container mailpit -p "127.0.0.1:$MAILPIT:8025" -p "127.0.0.1:$SMTP:1025" \
    -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit:latest
  has sandbox && container dovecot -p "127.0.0.1:$IMAPS:31993" -e 'USER_PASSWORD={PLAIN}sandbox' dovecot/dovecot:latest
  wait_for redis "docker exec $(cname redis) redis-cli ping" 30
  wait_for mailpit "curl -fs localhost:$MAILPIT/api/v1/info" 30

  say "starting backend on :$API (applies migrations on boot)"
  export_stripe
  make_cmd backend | billing >"$RUN/backend.sh"
  start backend "exec bash '$RUN/backend.sh'"
  wait_for backend "curl -fs localhost:$API/health" 300

  # Seed on an empty users table, not on a missing database: a failed first run leaves it empty.
  if ! db_seeded; then
    if has sandbox; then say "seeding the Sunrise Labs sandbox into $DB"; else say "seeding $DB (rich + full fixtures)"; fi
    seed >"$RUN/seed.log" 2>&1 || { echo "seed failed:"; tail -30 "$RUN/seed.log"; exit 1; }
  fi

  if has full; then
    say "starting consumer, worker and realtime"
    make_cmd consumer >"$RUN/consumer.sh"
    start consumer "exec bash '$RUN/consumer.sh'"
    make_cmd worker >"$RUN/worker.sh"
    start worker "exec bash '$RUN/worker.sh'"
    check_image "$REALTIME_IMAGE" realtime/
    container realtime -p "127.0.0.1:$RT:4000" -e PORT=4000 -e PHX_HOST=localhost -e APP_ENV=dev \
      -e "DATABASE_URL=postgres://warmbly:warmbly@host.docker.internal:15432/$DB?sslmode=disable" -e DATABASE_SSL=false \
      -e "REDIS_URL=redis://$(cname redis):6379" -e "JWT_SECRET=$AUTH_SECRET" -e PUBSUB_ENABLED=false \
      -e SECRET_KEY_BASE=local-development-secret-key-base-minimum-64-characters-for-phoenix -e CHECK_ORIGIN=false \
      "$REALTIME_IMAGE"
  fi
  if has sandbox; then
    check_image "$TRACKING_IMAGE" tracking/
    container tracking -p "127.0.0.1:$TRACK:3000" -e APP_ENV=dev -e AWS_CONFIG_ENABLED=false -e TRACKING_HOST=0.0.0.0 \
      -e TRACKING_PORT=3000 -e EVENTBUS_PROVIDER=nats -e "NATS_URL=nats://$(cname nats):4222" -e CODEC_PROVIDER=json \
      -e "BACKEND_INTERNAL_URL=http://host.docker.internal:$API" -e "INTERNAL_API_TOKEN=$INTERNAL_TOKEN" "$TRACKING_IMAGE"
    say "starting the simulator (delivers, opens, clicks and replies like the internet)"
    make_cmd sandbox-simulate | sed "s#$BIN/sandbox#$(sandbox_env) $BIN/sandbox#" >"$RUN/simulator.sh"
    start simulator "exec bash '$RUN/simulator.sh'"
  fi

  say "starting dashboard on :$WEB"
  start web "cd web && export VITE_APP_URL=http://localhost:$WEB VITE_API_URL=http://localhost:$API VITE_TURNSTILE_KEY=1x00000000000000000000AA VITE_TURNSTILE_BYPASS_TOKEN=warmbly-local-turnstile-bypass; exec pnpm dev --port $WEB --strictPort"
  wait_for web "curl -fs localhost:$WEB" 120
  if has full; then wait_for realtime "curl -fs localhost:$RT/health" 90; fi
  if has sandbox; then wait_for tracking "curl -fs localhost:$TRACK/health" 60; fi

  write_env
  start_watchdog
  status
}

down() {
  local n
  for n in watchdog simulator web worker consumer backend; do stop_proc "$n"; done
  docker ps -aq --filter "label=$LABEL" | xargs -r docker rm -f >/dev/null
  docker network rm "$NET" >/dev/null 2>&1 || true
  rm -f "$RUN/env.json"
  say "stopped (data kept in $DB)"
}

rss_mb() { ps -o rss= -g "$(cat "$RUN/$1.pid")" 2>/dev/null | awk '{s+=$1} END {printf "%d", s/1024}'; }

status() {
  local n mb total=0 name mem ids idle=off
  load_ports
  MODE=$(cat "$RUN/mode" 2>/dev/null || echo lite)
  printf '  mode %s\n\n' "$MODE"
  for n in backend web consumer worker simulator; do
    case $n in consumer | worker) has full || continue ;; simulator) has sandbox || continue ;; esac
    if alive "$n"; then
      mb=$(rss_mb "$n"); total=$((total + mb))
      printf '  %-10s up    %5s MB\n' "$n" "$mb"
    else
      printf '  %-10s down\n' "$n"
    fi
  done
  ids=$(docker ps -q --filter "label=$LABEL")
  if [ -n "$ids" ]; then
    # shellcheck disable=SC2086
    while read -r name mem; do
      mb=$(awk -v m="$mem" 'BEGIN { v = m + 0; if (m ~ /GiB/) v *= 1024; else if (m ~ /KiB/) v /= 1024; printf "%d", v }')
      total=$((total + mb))
      printf '  %-10s up    %5s MB  container\n' "${name#"wqa-$slug-"}" "$mb"
    done < <(docker stats --no-stream --format '{{.Name}} {{.MemUsage}}' $ids | awk '{print $1, $2}')
  fi
  [ "$IDLE_MIN" != 0 ] && idle="after $IDLE_MIN min unused"
  cat <<INFO

  total      ~$total MB   (auto-stop: $idle)

  Dashboard  http://localhost:$WEB/app/emails
  API        http://localhost:$API
  Mailpit    http://localhost:$MAILPIT   (this stack's mail and login codes)
  Login      $(account) / password123
  Database   $DB
INFO
}

ls_stacks() {
  local out
  out=$(docker ps --filter label=warmbly-qa --format '{{.Label "warmbly-qa"}}' | sort | uniq -c)
  if [ -z "$out" ]; then echo "  no QA stacks running"; else echo "$out" | awk '{printf "  %s  (%s containers)\n", $2, $1}'; fi
}

case "${1:-status}" in
  up) up "${2:-}" ;;
  down) down ;;
  restart)
    mode=$(cat "$RUN/mode" 2>/dev/null || echo lite)
    down
    up "$mode"
    ;;
  status) status ;;
  logs) if [ -n "${2:-}" ]; then tail -f "$RUN/$2.log"; else tail -f "$RUN"/*.log; fi ;;
  reset-data)
    target=${2:-$(cat "$RUN/mode" 2>/dev/null || echo lite)}
    down
    psql_ -d postgres -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null
    rm -rf "$QA/.artifacts/auth" "$RUN/blobs" "$RUN/mode"
    say "dropped $DB"
    up "$target"
    ;;
  ls) ls_stacks ;;
  *) sed -n '2,20p' "$0"; exit 1 ;;
esac
