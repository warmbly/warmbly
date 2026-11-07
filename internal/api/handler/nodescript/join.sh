#!/bin/sh
# Join this machine to a Warmbly fleet.
#
#   curl -fsSL https://<your-instance>/join.sh | sh -s -- \
#     --url https://<your-instance> --token <join-token> --role worker
#
# It asks the control plane to enrol the machine, writes the config the control
# plane hands back, installs a systemd service for the role and a timer that
# keeps it on the version the control plane wants, and starts it.
#
# Nothing is pushed to this machine, before or after. No inbound port is
# opened, no SSH key is installed, and the only credential involved is the join
# token, which is used once and never stored.
#
# POSIX sh: this runs under whatever /bin/sh the host has, which on Debian and
# Ubuntu is dash.
set -eu

WARMBLY_URL=""
WARMBLY_TOKEN=""
WARMBLY_ROLE=""
WARMBLY_REGION=""
WARMBLY_NAME=""
WARMBLY_IMAGE_REPO="ghcr.io/warmbly/warmbly"
CONFIG_DIR="/etc/warmbly"
STATE_DIR="/var/lib/warmbly"
# What the container may write. Kept separate from STATE_DIR because STATE_DIR
# also holds image-ref, which systemd feeds to a root `docker run`: anything
# the node can rewrite there would choose the image root then executes.
AGENT_DIR="/var/lib/warmbly/node"
DRY_RUN="false"
PRINT_UNIT="false"
PRINT_UPDATER="false"
ADOPT="false"
ADOPT_ACCEPT_TARGET="false"
ADOPT_ENV_REFRESH="false"
ADOPT_KEEP_LOCAL=""

log()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
Join a machine to a Warmbly fleet.

  --url <url>        Your Warmbly instance, e.g. https://app.example.com  (required)
  --token <token>    Fleet join token. Issue one in Fleet settings, or with
                     `warmblyctl fleet join-token`.                        (required)
  --role <role>      worker | consumer                                     (required)
  --region <label>   Where this machine egresses from, e.g. eu-central.
                     Placement prefers a worker near where a mailbox's
                     provider expects sign-ins. Optional.
  --name <name>      Display name in the dashboard. Defaults to the hostname.
  --image-repo <r>   Container image repository. Defaults to
                     ghcr.io/warmbly/warmbly.
  --config-dir <d>   Where to write the env file. Default /etc/warmbly.
  --dry-run          Enrol and print what would be written, change nothing.
  --print-updater    Print the updater that would be installed and exit.
  --print-unit       Print the systemd unit that would be installed and exit.
                     Contacts nothing and writes nothing; used by
                     `make join-check`.

Adopting a machine that already runs a node (no enrolment, node.env kept):
  --adopt            Install the standard unit and updater for the node
                     described by the existing node.env.
  --keep-local <k,k> Copy these node.env keys into node.local.env, for values
                     only this machine knows.
  --env-refresh      Let the updater refresh node.env from the control plane,
                     enabled only when a refresh would keep every key.
  --accept-target    Adopt even though the control plane asks for a different
                     version than this node runs.

  -h, --help         This text.

A second run re-enrols the same machine: it keeps the existing node id, so the
node keeps its identity, history and mailbox placements. It rewrites node.env
from the control plane's answer; put anything of your own in node.local.env
next to it, which is created once and never written again.
Normal first joins persist a nonsecret pending UUID before contacting the
control plane, so a failed or lost reply can be retried with the same identity.
USAGE
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --url)         WARMBLY_URL="${2:-}"; shift 2 ;;
      --token)       WARMBLY_TOKEN="${2:-}"; shift 2 ;;
      --role)        WARMBLY_ROLE="${2:-}"; shift 2 ;;
      --region)      WARMBLY_REGION="${2:-}"; shift 2 ;;
      --name)        WARMBLY_NAME="${2:-}"; shift 2 ;;
      --image-repo)  WARMBLY_IMAGE_REPO="${2:-}"; shift 2 ;;
      --config-dir)  CONFIG_DIR="${2:-}"; shift 2 ;;
      --dry-run)     DRY_RUN="true"; shift ;;
      --print-unit)  PRINT_UNIT="true"; shift ;;
      --print-updater) PRINT_UPDATER="true"; shift ;;
      --adopt)       ADOPT="true"; shift ;;
      --accept-target) ADOPT_ACCEPT_TARGET="true"; shift ;;
      --env-refresh) ADOPT_ENV_REFRESH="true"; shift ;;
      --keep-local)  ADOPT_KEEP_LOCAL="${2:-}"; shift 2 ;;
      -h|--help)     usage; exit 0 ;;
      *)             die "unknown option: $1 (try --help)" ;;
    esac
  done
}

require_args() {
  [ -n "$WARMBLY_URL" ]   || die "--url is required"
  [ -n "$WARMBLY_TOKEN" ] || die "--token is required"
  [ -n "$WARMBLY_ROLE" ]  || die "--role is required (worker or consumer)"
  case "$WARMBLY_ROLE" in
    worker|consumer) ;;
    *) die "--role must be worker or consumer, got '$WARMBLY_ROLE'" ;;
  esac
  [ -n "$WARMBLY_NAME" ] || WARMBLY_NAME="$(hostname 2>/dev/null || echo warmbly-node)"
  # Trim a trailing slash so the URLs we build never double up.
  WARMBLY_URL="${WARMBLY_URL%/}"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"
}

check_deps() {
  need_cmd curl
  if [ "$DRY_RUN" = "false" ]; then
    need_cmd flock
    command -v docker >/dev/null 2>&1 || die "docker is required but not installed. Install it, then re-run."
    command -v systemctl >/dev/null 2>&1 || die "systemd is required (this script installs a service and a timer)"
    [ "$(id -u)" = "0" ] || die "run as root: this writes to $CONFIG_DIR and installs a systemd unit"
  fi
}

# json_field extracts a top-level string field. The join response is generated
# by our own backend and is a flat object, so this stays honest without pulling
# in a JSON parser the host may not have.
json_field() {
  # shellcheck disable=SC2016
  sed -n 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

# b64decode reads base64 on stdin. coreutils is the norm; openssl is the
# fallback for the images that ship without it.
b64decode() {
  if command -v base64 >/dev/null 2>&1; then
    base64 -d
  else
    openssl base64 -d -A
  fi
}

existing_node_id() {
  if [ -f "$CONFIG_DIR/node.env" ]; then
    sed -n 's/^WARMBLY_NODE_ID=//p' "$CONFIG_DIR/node.env" | head -n 1
  fi
}

valid_node_id() {
  printf '%s\n' "$1" | LC_ALL=C grep -Eq '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' &&
    [ "$1" != "00000000-0000-0000-0000-000000000000" ]
}

prepare_node_id() {
  PRIOR_NODE_ID="$(existing_node_id)"
  [ "$DRY_RUN" = "false" ] || return 0
  mkdir -p "$CONFIG_DIR"
  [ ! -L "$CONFIG_DIR/.join.lock" ] || die "join lock must not be a symlink"
  # Hold the lock through configuration writes, not just UUID initialization.
  exec 9>"$CONFIG_DIR/.join.lock"
  flock -w 30 9 || die "another join is in progress; retry after it finishes"
  PRIOR_NODE_ID="$(existing_node_id)"
  if [ -z "$PRIOR_NODE_ID" ]; then
    [ ! -L "$CONFIG_DIR/node.pending-id" ] || die "pending node identity must not be a symlink"
    if [ -f "$CONFIG_DIR/node.pending-id" ]; then
      PRIOR_NODE_ID="$(cat "$CONFIG_DIR/node.pending-id")"
    else
      PRIOR_NODE_ID="$(cat /proc/sys/kernel/random/uuid)" || die "secure UUID generation unavailable"
      valid_node_id "$PRIOR_NODE_ID" || die "secure UUID generation returned an invalid identity"
      pending=$(mktemp "$CONFIG_DIR/.node-id.XXXXXX")
      printf '%s\n' "$PRIOR_NODE_ID" > "$pending"
      chmod 600 "$pending"
      mv "$pending" "$CONFIG_DIR/node.pending-id"
    fi
  fi
  valid_node_id "$PRIOR_NODE_ID" || die "stored node identity is invalid; no enrollment requested"
  PRIOR_NODE_ID=$(printf '%s' "$PRIOR_NODE_ID" | tr 'A-F' 'a-f')
}

enrol() {
  prepare_node_id
  prior="$PRIOR_NODE_ID"
  if [ -n "$prior" ]; then
    log "Re-joining as existing node $prior"
  fi

  body=$(printf '{"token":"%s","role":"%s","region":"%s","name":"%s","node_id":"%s"}' \
    "$WARMBLY_TOKEN" "$WARMBLY_ROLE" "$WARMBLY_REGION" "$WARMBLY_NAME" "$prior")

  tmp="$(mktemp)"
  code=$(curl -sS -o "$tmp" -w '%{http_code}' \
    -X POST "$WARMBLY_URL/api/v1/fleet/join" \
    -H 'Content-Type: application/json' \
    -d "$body" || echo 000)

  if [ "$code" = "000" ]; then
    rm -f "$tmp"
    die "could not reach $WARMBLY_URL. Check the URL and that this machine can reach it."
  fi
  if [ "$code" != "200" ]; then
    detail=$(json_field message < "$tmp")
    [ -n "$detail" ] || detail=$(cat "$tmp")
    rm -f "$tmp"
    case "$code" in
      401) die "the join token was rejected: $detail" ;;
      *)   die "enrolment failed (HTTP $code): $detail" ;;
    esac
  fi

  NODE_ID=$(json_field node_id < "$tmp")
  DESIRED_VERSION=$(json_field desired_version < "$tmp")
  # The env file arrives base64 encoded, so a shell with no JSON parser can
  # recover it exactly. Decoding is one command; picking a multi-line,
  # quote-bearing value back out of JSON with sed is guesswork.
  NODE_ENV=$(json_field env_b64 < "$tmp" | b64decode)
  rm -f "$tmp"

  [ -n "$NODE_ID" ] || die "the control plane did not return a node id"
  [ -n "$NODE_ENV" ] || die "the control plane returned no configuration for this node"
  if [ "$DRY_RUN" = "false" ]; then
    valid_node_id "$NODE_ID" || die "the control plane returned an invalid node identity"
    NODE_ID=$(printf '%s' "$NODE_ID" | tr 'A-F' 'a-f')
    [ "$NODE_ID" = "$prior" ] || die "the control plane returned a different node identity; configuration not written"
    env_id=$(printf '%s\n' "$NODE_ENV" | sed -n 's/^WARMBLY_NODE_ID=//p')
    [ "$env_id" = "$NODE_ID" ] || die "node configuration does not match the enrolled identity"
  fi
  # The control plane names a tag, including when it has no release resolved.
  # This only catches a backend too old to do that, and it names the tag the
  # project publishes: there is no `latest`.
  [ -n "$DESIRED_VERSION" ] || DESIRED_VERSION="prod"
  log "Enrolled as $WARMBLY_ROLE node $NODE_ID"
}

write_config() {
  if [ "$DRY_RUN" = "true" ]; then
    log ""
    log "--dry-run: would write $CONFIG_DIR/node.env with:"
    printf '%s\n' "$NODE_ENV" | redact | sed 's/^/    /'
    log ""
    log "--dry-run: would create $CONFIG_DIR/node.local.env if absent, and leave it"
    log "           alone if present. Both files are passed to the container."
    log ""
    log "--dry-run: would run image $WARMBLY_IMAGE_REPO/$WARMBLY_ROLE:$DESIRED_VERSION"
    return 0
  fi

  mkdir -p "$CONFIG_DIR" "$STATE_DIR" "$AGENT_DIR"
  # The node container runs as uid 1000 (deploy/docker/worker.Dockerfile), so
  # the directory it writes into has to be owned by that uid, or its
  # target-version write fails with EACCES, which it only logs, and auto-update
  # silently never happens.
  #
  # Only this subdirectory, never STATE_DIR itself: a recursive chown there
  # would re-own a bare-metal install's BLOB_FS_ROOT, and making STATE_DIR
  # container-writable would let the node rewrite the image reference that
  # systemd hands to a root `docker run --network host`.
  chown 1000:1000 "$AGENT_DIR" 2>/dev/null || true
  chmod 0700 "$AGENT_DIR"
  # uid 1000 needs traverse on the parent to reach it. Readable and executable,
  # never writable: image-ref lives here and systemd feeds it to a root
  # `docker run`, so the node must not be able to replace it.
  chmod 0755 "$STATE_DIR"
  # Converge a machine joined by an earlier version of this script, which
  # chowned the whole tree to uid 1000 and so left image-ref rewritable by the
  # node. Re-owning is idempotent and cheap.
  chown root:root "$STATE_DIR" 2>/dev/null || true
  for f in image image-ref target-version; do
    [ -e "$STATE_DIR/$f" ] || continue
    chown root:root "$STATE_DIR/$f" 2>/dev/null || true
    chmod 0644 "$STATE_DIR/$f"
  done
  umask 077
  {
    printf '%s\n' "$NODE_ENV"
    printf 'WARMBLY_VERSION=%s\n' "$DESIRED_VERSION"
    printf 'WARMBLY_TARGET_VERSION_PATH=%s/target-version\n' "$AGENT_DIR"
    printf 'WARMBLY_NODE_NAME=%s\n' "$WARMBLY_NAME"
    # node.env is exactly the render plus the lines above, so a refresh loses nothing.
    printf 'WARMBLY_ENV_REFRESH=true\n'
  } > "$CONFIG_DIR/node.env"
  chmod 600 "$CONFIG_DIR/node.env"

  ensure_local_env

  printf '%s\n' "$DESIRED_VERSION" > "$AGENT_DIR/target-version"
  chown 1000:1000 "$AGENT_DIR/target-version" 2>/dev/null || true
  printf '%s\n' "$WARMBLY_IMAGE_REPO/$WARMBLY_ROLE" > "$STATE_DIR/image"
  # systemd performs no command substitution, so the image reference has to
  # reach the unit as an environment variable it can expand itself.
  printf 'WARMBLY_IMAGE_REF=%s/%s:%s\n' \
    "$WARMBLY_IMAGE_REPO" "$WARMBLY_ROLE" "$DESIRED_VERSION" > "$STATE_DIR/image-ref"
  log "Wrote $CONFIG_DIR/node.env"
}

# redact masks every value in the env that carries a credential, for the
# --dry-run listing. Two shapes, because they leak differently:
#
#   NAME_TOKEN=secret             the whole value goes
#   NAME=scheme://user:pass@host  only the userinfo goes, so the address the
#                                 node will actually use stays readable, which
#                                 is the thing --dry-run exists to show
#
# The second shape is why a name list is not enough on its own: PRIMARY_DB,
# NATS_URL, REDIS and SENTRY_DSN all carry their credential inside a URL and
# none of them is called TOKEN, KEY, SECRET or PASSWORD.
#
# One -e per word rather than a `\|` alternation: alternation in a BRE is a GNU
# extension, and on a sed without it the expression matches nothing and every
# secret prints in clear, which is the failure mode this function exists to
# prevent. Matched as a SUFFIX so ENCRYPTED_KEYS_BACKEND_URL, an address worth
# reading, is not masked for containing "KEY".
redact() {
  sed -e 's/^\([A-Z0-9_]*TOKEN\)=.*/\1=***/' \
      -e 's/^\([A-Z0-9_]*KEY\)=.*/\1=***/' \
      -e 's/^\([A-Z0-9_]*SECRET\)=.*/\1=***/' \
      -e 's/^\([A-Z0-9_]*PASSWORD\)=.*/\1=***/' \
      -e 's/^\([A-Z0-9_]*DSN\)=.*/\1=***/' \
      -e 's|^\([A-Z0-9_]*\)=\([a-z][a-z0-9+.-]*://\)[^:/@]*:[^@]*@|\1=\2***:***@|' \
      -e 's|^\([A-Z0-9_]*\)=\([a-z][a-z0-9+.-]*://\)[^:/@]*@|\1=\2***@|'
}

# ensure_local_env creates the operator's own env file, once. node.env is
# rewritten wholesale on every join, so anything added there is lost the next
# time this runs; this file is the place that survives. The container reads
# both, this one second, so a value here wins.
#
# Never truncates an existing file: re-running a join must not discard the
# credential someone put here.
ensure_local_env() {
  if [ -f "$CONFIG_DIR/node.local.env" ]; then
    return 0
  fi
  cat > "$CONFIG_DIR/node.local.env" <<'LOCALENV'
# Local overrides for this machine, read after node.env, so a name repeated
# here wins.
#
# `warmbly join` creates this file once and never writes it again, which makes
# it the place for anything the control plane cannot know: a DSN it does not
# hold, credentials for infrastructure of your own, a per-machine tuning knob.
#
# KEY=value, one per line, no export, no quotes needed.
LOCALENV
  chmod 600 "$CONFIG_DIR/node.local.env"
  log "Created $CONFIG_DIR/node.local.env (yours; re-joining leaves it alone)"
  return 0
}

# Blob handling. All of it reads the env in memory rather than the file on
# disk, so --dry-run reports the same thing a real join would do instead of
# reading a previous join's leftovers or failing on a file that is not there.
blob_provider() {
  printf '%s\n' "$NODE_ENV" | sed -n 's/^BLOB_PROVIDER=//p' | head -n 1
}

# blobs_are_local matches what storage.NewFromEnv accepts, which is both
# "filesystem" and the "fs" alias. Testing only the long form left an fs
# instance with no mount and no warning.
blobs_are_local() {
  case "$(blob_provider)" in
    filesystem|fs) return 0 ;;
    *) return 1 ;;
  esac
}

blob_root() {
  printf '%s\n' "$NODE_ENV" | sed -n 's/^BLOB_FS_ROOT=//p' | head -n 1
}

# ensure_blob_root prepares the directory the node's storage layer will open.
# It has to exist and be writable by uid 1000 before the container starts: the
# storage layer does MkdirAll and the process exits if that fails, and an
# unmounted path is created root-owned by docker, so the node restart-loops.
#
# Failures are reported, never swallowed: a silent skip here produces exactly
# that restart loop after the script has printed "Done".
ensure_blob_root() {
  root="$1"
  if [ ! -d "$root" ]; then
    # 0022 for the duration: write_config sets umask 077, which would create
    # every missing PARENT 0700 and leave a co-located backend unable to
    # traverse in. The explicit chmod below only covers the leaf.
    ( umask 022 && mkdir -p "$root" ) || die "could not create BLOB_FS_ROOT '$root'"
    chmod 0755 "$root" || die "could not set permissions on '$root'"
    chown 1000:1000 "$root" 2>/dev/null || true
    return 0
  fi

  # It already existed, so it belongs to something else - most likely a
  # co-located backend. Re-owning it would break that backend, so this only
  # reports: guessing at writability from the mode bits got both directions
  # wrong (a 0666 root has no search bit, a root:1000 0775 root is fine), and a
  # wrong guess is worse than saying plainly what to check.
  owner=$(stat -c '%u' "$root" 2>/dev/null || echo "")
  if [ -n "$owner" ] && [ "$owner" != "1000" ]; then
    warn ""
    warn "NOTE: $root already exists and is owned by uid $owner."
    warn "      The node runs as uid 1000. If it cannot write there, sends fail"
    warn "      when the worker tries to store a message body. Check with:"
    warn ""
    warn "        sudo -u '#1000' test -w $root && echo writable || echo NOT writable"
    warn ""
    warn "      Give uid 1000 access, or switch the instance to BLOB_PROVIDER=s3."
    warn ""
  fi
  return 0
}

# validate_blob_root rejects a value docker could never mount, right after
# enrolment and before anything is written.
validate_blob_root() {
  blobs_are_local || return 0
  root=$(blob_root)
  [ -n "$root" ] || return 0
  case "$root" in
    /*) ;;
    *) die "BLOB_FS_ROOT is '$root', which is not an absolute path. Docker cannot mount a relative path; fix it on the backend and re-run." ;;
  esac
  if [ -e "$root" ] && [ ! -d "$root" ]; then
    die "BLOB_FS_ROOT '$root' exists but is not a directory."
  fi
  return 0
}

# docker_mounts is every -v argument, on ONE line. Command substitution strips
# trailing newlines, so a multi-line value would collapse the unit's
# continuations and hand docker a stray token as the image name.
#
# Pure: it computes the list and creates nothing. Preparing the directories is
# ensure_blob_root's job, called from install_units, so the unit can be
# rendered and asserted on without touching the filesystem.
docker_mounts() {
  mounts="-v $AGENT_DIR:$AGENT_DIR"
  if blobs_are_local; then
    root=$(blob_root)
    if [ -n "$root" ]; then
      mounts="$mounts -v $root:$root"
    fi
  fi
  printf '%s' "$mounts"
}

# warn_shared_blobs is loud on purpose. A node on filesystem blobs either has
# its own copy, and cannot read the bodies the backend asked it to send, or
# shares a directory it may have no permission on. Both fail at send time, long
# after this script has printed "Done".
warn_shared_blobs() {
  blobs_are_local || return 0
  warn ""
  warn "WARNING: this instance stores blobs on local disk (BLOB_PROVIDER=$(blob_provider),"
  warn "         BLOB_FS_ROOT=$(blob_root))."
  warn ""
  warn "         A node needs the SAME storage the backend writes to, with"
  warn "         permissions it can read. That only holds when the node shares a"
  warn "         filesystem with the backend and the ids line up. Otherwise sends"
  warn "         fail when the worker cannot read the message body."
  warn ""
  warn "         Set BLOB_PROVIDER=s3 on the backend before running nodes off-host,"
  warn "         then re-run this command."
  warn ""
}

# warn_missing_db covers the one config a consumer cannot start without and the
# control plane cannot always supply: an instance holding its DSN in SSM rather
# than its environment has nothing to send. Silence here is a node that enrols,
# writes its files, and then restart-loops on a config error.
warn_missing_db() {
  [ "$WARMBLY_ROLE" = "consumer" ] || return 0
  if printf '%s\n' "$NODE_ENV" | grep -q '^PRIMARY_DB='; then
    return 0
  fi
  warn ""
  warn "WARNING: this consumer has no PRIMARY_DB."
  warn ""
  warn "         A consumer is control plane: it updates relational state"
  warn "         directly, so it needs the database DSN. The control plane sent"
  warn "         none, which means the backend reads its own DSN from somewhere"
  warn "         other than its environment (AWS SSM or Secrets Manager)."
  warn ""
  warn "         Add it to $CONFIG_DIR/node.local.env, which re-joining will"
  warn "         not overwrite, then: systemctl restart warmbly-consumer"
  warn ""
  warn "           PRIMARY_DB=postgres://user:pass@host:5432/warmbly?sslmode=verify-full"
  warn ""
}

# render_unit prints the systemd service exactly as install_units writes it.
# Separate so it can be asserted on without root, Docker, or a real join:
# `make join-check` renders this and checks the result, because every defect
# this file has had parsed cleanly and only showed up in what it produced.
render_unit() {
  service="warmbly-$WARMBLY_ROLE"
  MOUNTS=$(docker_mounts)
  cat <<UNIT
[Unit]
Description=Warmbly $WARMBLY_ROLE
After=docker.service network-online.target
Requires=docker.service

[Service]
Restart=always
RestartSec=5
# systemd does not run a shell, so the image reference comes from a file it
# reads as environment rather than from a command substitution. \${VAR} expands
# to exactly one argument, which is what an image:tag needs.
EnvironmentFile=$STATE_DIR/image-ref
# The container is replaced rather than reconfigured, so start always removes
# any previous one first: a name collision after an unclean stop would
# otherwise wedge the service in a restart loop.
ExecStartPre=-/usr/bin/docker rm -f $service
ExecStart=/usr/bin/docker run --rm --name $service --env-file $CONFIG_DIR/node.env --env-file $CONFIG_DIR/node.local.env --network host $MOUNTS \${WARMBLY_IMAGE_REF}
# A send or import in flight gets time to finish before the container is killed.
ExecStop=/usr/bin/docker stop --time 45 $service
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
UNIT
}

# render_updater prints the root updater exactly as install_updater writes it.
# The node only writes requests into $AGENT_DIR; this decides whether to act
# on them, so the process being replaced is never the one doing the replacing.
render_updater() {
  cat <<'UPDATER'
#!/bin/sh
# Applies what the control plane asked this node to run: a version, and with
# WARMBLY_ENV_REFRESH=true in node.env, a refreshed env. Never reaches out
# except to pull the image and, for a refresh, to fetch this node's own env.
set -eu
# ROOT and the timings exist for make join-check; the timer sets none of them.
ROOT="${WARMBLY_UPDATER_ROOT:-}"
STATE_DIR="$ROOT/var/lib/warmbly"
AGENT_DIR="$ROOT/var/lib/warmbly/node"
CONFIG_DIR="$ROOT/etc/warmbly"
HEALTH_SECONDS="${WARMBLY_UPDATE_HEALTH_SECONDS:-120}"
SETTLE_SECONDS="${WARMBLY_UPDATE_SETTLE_SECONDS:-15}"
POLL_SECONDS="${WARMBLY_UPDATE_POLL_SECONDS:-10}"
LOCK_SECONDS="${WARMBLY_UPDATE_LOCK_SECONDS:-300}"
# Kept across an env refresh: written by join or by this updater, never rendered.
PRESERVED_KEYS="WARMBLY_VERSION WARMBLY_TARGET_VERSION_PATH WARMBLY_NODE_NAME WARMBLY_ENV_REFRESH NODE_LOG_TOKEN NODE_CREDENTIAL"

log() { printf 'warmbly-node-update: %s\n' "$*"; }

env_value() {
  sed -n "s/^$1=//p" "$CONFIG_DIR/node.env" | head -n 1
}

# read_token reads a file the node wrote. The node is untrusted here, so a
# symlink or anything but a plain token reads as empty.
read_token() {
  [ -f "$1" ] || return 0
  [ ! -L "$1" ] || return 0
  v=$(head -c 256 "$1" | head -n 1)
  case "$v" in
    ""|*[!A-Za-z0-9._:+-]*) return 0 ;;
  esac
  printf '%s\n' "$v"
}

# write_agent replaces a file in the node's directory by rename, so a symlink
# the node planted is replaced, never followed.
write_agent() {
  tmp=$(mktemp "$AGENT_DIR/.update.XXXXXX")
  printf '%s\n' "$2" > "$tmp"
  chmod 0644 "$tmp"
  chown 1000:1000 "$tmp" 2>/dev/null || true
  mv -f "$tmp" "$AGENT_DIR/$1"
}

set_version() {
  if grep -q '^WARMBLY_VERSION=' "$CONFIG_DIR/node.env"; then
    sed -i "s|^WARMBLY_VERSION=.*|WARMBLY_VERSION=$1|" "$CONFIG_DIR/node.env"
  else
    printf 'WARMBLY_VERSION=%s\n' "$1" >> "$CONFIG_DIR/node.env"
  fi
}

# digest_for prints the digest the control plane pinned for tag, if any.
digest_for() {
  [ -f "$AGENT_DIR/target-digest" ] || return 0
  [ ! -L "$AGENT_DIR/target-digest" ] || return 0
  line=$(head -c 256 "$AGENT_DIR/target-digest" | head -n 1)
  printf '%s\n' "$line" | grep -Eq '^[A-Za-z0-9._-]+ sha256:[0-9a-f]{64}$' || return 0
  [ "${line%% *}" = "$1" ] || return 0
  printf '%s\n' "${line#* }"
}

started() {
  docker inspect -f '{{.State.Running}} {{.State.StartedAt}}' "$service" 2>/dev/null || echo "false -"
}

# healthy passes when the restarted container comes up and stays on one start
# for HEALTH_SECONDS. A crash loop restarts it, which changes StartedAt.
healthy() {
  sleep "$SETTLE_SECONDS"
  first=$(started)
  case "$first" in
    "true "*) ;;
    *) return 1 ;;
  esac
  waited=0
  while [ "$waited" -lt "$HEALTH_SECONDS" ]; do
    sleep "$POLL_SECONDS"
    waited=$((waited + POLL_SECONDS))
    [ "$(started)" = "$first" ] || return 1
  done
  return 0
}

update_version() {
  target=$(read_token "$AGENT_DIR/target-version")
  [ -n "$target" ] || return 0
  case "$target" in
    *[!A-Za-z0-9._-]*) log "refusing malformed target '$target'"; return 0 ;;
  esac
  current=$(env_value WARMBLY_VERSION)
  [ "$target" != "$current" ] || return 0
  if [ "$(read_token "$AGENT_DIR/update-failed")" = "$target" ]; then
    return 0
  fi
  image=$(head -n 1 "$STATE_DIR/image")
  case "$image" in
    ""|*[!A-Za-z0-9./_-]*) log "refusing malformed image '$image'"; return 0 ;;
  esac
  ref="$image:$target"
  digest=$(digest_for "$target")
  if [ -n "$digest" ]; then
    ref="$image@$digest"
  fi
  # Pull first, so a missing image leaves the node on what it runs.
  if ! docker pull "$ref" >/dev/null 2>&1; then
    log "$ref is not pullable yet; staying on $current"
    return 0
  fi
  previous=$(cat "$STATE_DIR/image-ref")
  set_version "$target"
  printf 'WARMBLY_IMAGE_REF=%s\n' "$ref" > "$STATE_DIR/image-ref"
  log "$current -> $target ($ref)"
  systemctl restart "$service"
  if healthy; then
    rm -f "$AGENT_DIR/update-failed"
    return 0
  fi
  log "$target did not stay up; returning to $current"
  set_version "$current"
  printf '%s\n' "$previous" > "$STATE_DIR/image-ref"
  systemctl restart "$service"
  write_agent update-failed "$target"
}

# fetch_env writes this node's env as the control plane renders it now into $1.
fetch_env() {
  url=$(env_value WARMBLY_BACKEND_URL)
  id=$(env_value WARMBLY_NODE_ID)
  token=$(env_value NODE_BROKER_TOKEN)
  [ -n "$token" ] || token=$(env_value INTERNAL_API_TOKEN)
  if [ -z "$url" ] || [ -z "$id" ] || [ -z "$token" ]; then
    log "node.env lacks WARMBLY_BACKEND_URL, WARMBLY_NODE_ID or a node token; cannot fetch env"
    return 1
  fi
  # Both credentials go through curl's stdin config, never argv, so ps never shows them.
  cred=$(env_value NODE_CREDENTIAL)
  code=$( {
    printf 'header = "Authorization: Bearer %s"\n' "$token"
    if [ -n "$cred" ]; then
      printf 'header = "X-Warmbly-Node-Id: %s"\n' "$id"
      printf 'header = "X-Warmbly-Node-Credential: %s"\n' "$cred"
    fi
  } | curl -sS -K - --max-time 30 -o "$1" -w '%{http_code}' \
      "${url%/}/api/v1/internal/fleet/node-env?node_id=$id&role=$role") || code=000
  if [ "$code" != "200" ]; then
    case "$code" in
      401|403) log "fetching env failed (HTTP $code): the control plane hands an env only to the node it was issued to; run the join command again on this machine, which keeps its id and node.local.env" ;;
      *) log "fetching env failed (HTTP $code)" ;;
    esac
    return 1
  fi
  if ! grep -qx "WARMBLY_NODE_ID=$id" "$1" || ! grep -qx "WARMBLY_NODE_ROLE=$role" "$1"; then
    log "fetched env names another node; ignored"
    return 1
  fi
  return 0
}

refresh_env() {
  [ "$(env_value WARMBLY_ENV_REFRESH)" = "true" ] || return 0
  want=$(read_token "$AGENT_DIR/target-env-revision")
  [ -n "$want" ] || return 0
  have=$(read_token "$AGENT_DIR/env-revision")
  # A revision that failed its health check waits for the next one.
  if [ "$want" = "$have" ] || [ "$want-failed" = "$have" ]; then
    return 0
  fi
  umask 077
  fresh=$(mktemp "$CONFIG_DIR/.node.env.XXXXXX")
  if ! fetch_env "$fresh"; then
    rm -f "$fresh"
    return 0
  fi
  revision=$(sha256sum "$fresh" | cut -c1-32)
  for key in $PRESERVED_KEYS; do
    grep "^$key=" "$CONFIG_DIR/node.env" | head -n 1 >> "$fresh" || true
  done
  chmod 600 "$fresh"
  cp -p "$CONFIG_DIR/node.env" "$CONFIG_DIR/.node.env.previous"
  mv -f "$fresh" "$CONFIG_DIR/node.env"
  log "applied env revision $revision"
  systemctl restart "$service"
  if healthy; then
    rm -f "$CONFIG_DIR/.node.env.previous"
    write_agent env-revision "$revision"
    return 0
  fi
  log "env revision $revision did not stay up; restoring the previous env"
  mv -f "$CONFIG_DIR/.node.env.previous" "$CONFIG_DIR/node.env"
  systemctl restart "$service"
  write_agent env-revision "$want-failed"
}

# check_env lists, by name only, keys in node.env a refresh would drop.
check_env() {
  umask 077
  fresh=$(mktemp "$CONFIG_DIR/.node.env.XXXXXX")
  if ! fetch_env "$fresh"; then
    rm -f "$fresh"
    return 1
  fi
  dropped=""
  withdrawn=""
  if [ "$role" = "worker" ]; then
    withdrawn="CREDENTIALS_ENCRYPTION_KEY CREDENTIALS_ENCRYPTION_READ_KEYS KMS_LOCAL_MASTER_KEY"
  fi
  keys=$(sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' "$CONFIG_DIR/node.env" | tr '\n' ' ')
  for key in $keys; do
    case " $PRESERVED_KEYS " in *" $key "*) continue ;; esac
    # Keys the control plane no longer sends a worker are meant to go.
    case " $withdrawn " in *" $key "*) continue ;; esac
    grep -q "^$key=" "$fresh" && continue
    if [ -f "$CONFIG_DIR/node.local.env" ] && grep -q "^$key=" "$CONFIG_DIR/node.local.env"; then
      continue
    fi
    dropped="$dropped $key"
  done
  rm -f "$fresh"
  if [ -n "$dropped" ]; then
    log "a refresh would drop:$dropped (move them to node.local.env first)"
    return 1
  fi
  log "a refresh keeps every key node.env has"
  return 0
}

main() {
  [ -f "$CONFIG_DIR/node.env" ] || exit 0
  [ -f "$STATE_DIR/image" ] || exit 0
  role=$(env_value WARMBLY_NODE_ROLE)
  case "$role" in
    worker|consumer) ;;
    *) exit 0 ;;
  esac
  service="warmbly-$role"
  exec 8>"$ROOT/run/warmbly-node-update.lock"
  if [ "${1:-}" = "--check-env" ]; then
    # adopt reads success as a passed preflight, so a held lock has to wait or fail, never pass.
    flock -w "$LOCK_SECONDS" 8 || { log "another update is running; retry --check-env"; exit 1; }
    check_env
    return
  fi
  flock -n 8 || exit 0
  refresh_env
  update_version
  return 0
}

main "$@"
UPDATER
}

install_updater() {
  render_updater > /usr/local/bin/warmbly-node-update
  chmod 755 /usr/local/bin/warmbly-node-update

  cat > /etc/systemd/system/warmbly-node-update.service <<'UNIT'
[Unit]
Description=Apply the Warmbly version the control plane asked for

[Service]
Type=oneshot
ExecStart=/usr/local/bin/warmbly-node-update
UNIT

  cat > /etc/systemd/system/warmbly-node-update.timer <<'UNIT'
[Unit]
Description=Check for a new Warmbly version

[Timer]
OnBootSec=2min
OnUnitActiveSec=2min

[Install]
WantedBy=timers.target
UNIT
}

# adopt puts an already-running node under the standard unit and updater
# without enrolling it again: node.env stays as it is, apart from opting in to
# env refresh when asked, and node.local.env gains only the keys named.
adopt() {
  [ "$(id -u)" = "0" ] || die "run as root: --adopt installs a systemd unit"
  need_cmd flock
  need_cmd sha256sum
  command -v docker >/dev/null 2>&1 || die "docker is required but not installed"
  command -v systemctl >/dev/null 2>&1 || die "systemd is required"
  [ -f "$CONFIG_DIR/node.env" ] || die "--adopt needs an existing $CONFIG_DIR/node.env"
  NODE_ENV=$(cat "$CONFIG_DIR/node.env")
  NODE_ID=$(printf '%s\n' "$NODE_ENV" | sed -n 's/^WARMBLY_NODE_ID=//p' | head -n 1)
  valid_node_id "$NODE_ID" || die "node.env has no valid WARMBLY_NODE_ID; join this machine instead"
  role=$(printf '%s\n' "$NODE_ENV" | sed -n 's/^WARMBLY_NODE_ROLE=//p' | head -n 1)
  [ -n "$WARMBLY_ROLE" ] || WARMBLY_ROLE="$role"
  [ "$WARMBLY_ROLE" = "$role" ] || die "node.env says role '$role', not '$WARMBLY_ROLE'"
  case "$WARMBLY_ROLE" in
    worker|consumer) ;;
    *) die "node.env has no usable WARMBLY_NODE_ROLE" ;;
  esac
  current=$(printf '%s\n' "$NODE_ENV" | sed -n 's/^WARMBLY_VERSION=//p' | head -n 1)
  target=""
  if [ -f "$AGENT_DIR/target-version" ] && [ ! -L "$AGENT_DIR/target-version" ]; then
    target=$(head -n 1 "$AGENT_DIR/target-version")
  fi
  if [ -n "$target" ] && [ "$target" != "$current" ] && [ "$ADOPT_ACCEPT_TARGET" != "true" ]; then
    die "the control plane asks for $target and this node runs ${current:-an unknown version}; the updater would move it within two minutes. Set the fleet version first, or pass --accept-target."
  fi
  # The unit starts from image-ref and the updater pulls from image, so keep what the node already runs.
  [ -f "$STATE_DIR/image-ref" ] || die "--adopt needs $STATE_DIR/image-ref, the image this node starts from; join this machine instead"
  image=""
  if [ -f "$STATE_DIR/image" ]; then image=$(head -n 1 "$STATE_DIR/image"); fi
  if [ -z "$image" ]; then
    image=$(sed -n 's/^WARMBLY_IMAGE_REF=//p' "$STATE_DIR/image-ref" | head -n 1)
    image=${image%%@*}
    case "${image##*/}" in *:*) image=${image%:*} ;; esac
  fi
  [ -n "$image" ] || die "$STATE_DIR/image-ref names no image; join this machine instead"

  mkdir -p "$STATE_DIR" "$AGENT_DIR"
  chown 1000:1000 "$AGENT_DIR" 2>/dev/null || true
  chmod 0700 "$AGENT_DIR"
  chmod 0755 "$STATE_DIR"
  chown root:root "$STATE_DIR" 2>/dev/null || true
  printf '%s\n' "$image" > "$STATE_DIR/image"
  for f in image image-ref; do
    chown root:root "$STATE_DIR/$f" 2>/dev/null || true
    chmod 0644 "$STATE_DIR/$f"
  done
  if ! grep -q '^WARMBLY_TARGET_VERSION_PATH=' "$CONFIG_DIR/node.env"; then
    # Without it the node never writes the version the control plane asks for.
    printf 'WARMBLY_TARGET_VERSION_PATH=%s/target-version\n' "$AGENT_DIR" >> "$CONFIG_DIR/node.env"
  fi
  ensure_local_env
  keep_local
  render_unit > "/etc/systemd/system/warmbly-$WARMBLY_ROLE.service"
  # Keep this a standalone statement: make join-check asserts on it by first field.
  install_updater
  systemctl daemon-reload
  if [ "$ADOPT_ENV_REFRESH" = "true" ] && ! grep -qx 'WARMBLY_ENV_REFRESH=true' "$CONFIG_DIR/node.env"; then
    # Opt in only when a refresh would keep every key this node runs with.
    /usr/local/bin/warmbly-node-update --check-env || die "env refresh left off: name the keys listed above with --keep-local and run --adopt again"
    printf 'WARMBLY_ENV_REFRESH=true\n' >> "$CONFIG_DIR/node.env"
    log "Enabled env refresh"
  fi
  systemctl enable --now warmbly-node-update.timer >/dev/null 2>&1 || true
  log "Adopted $WARMBLY_ROLE node $NODE_ID. The updater runs every two minutes;"
  log "run \`systemctl restart warmbly-$WARMBLY_ROLE\` to start it under the new unit."
  return 0
}

# keep_local copies the named node.env keys into node.local.env, so a refresh
# that renders node.env afresh cannot drop a value only this machine knows.
keep_local() {
  [ -n "$ADOPT_KEEP_LOCAL" ] || return 0
  for key in $(printf '%s' "$ADOPT_KEEP_LOCAL" | tr ',' ' '); do
    case "$key" in
      *[!A-Za-z0-9_]*|"") die "--keep-local takes variable names, got '$key'" ;;
    esac
    grep -q "^$key=" "$CONFIG_DIR/node.local.env" && continue
    line=$(grep "^$key=" "$CONFIG_DIR/node.env" | head -n 1) || true
    if [ -z "$line" ]; then
      warn "$key is not in node.env; nothing to keep"
      continue
    fi
    printf '%s\n' "$line" >> "$CONFIG_DIR/node.local.env"
    log "Kept $key in node.local.env"
  done
  return 0
}

install_units() {
  [ "$DRY_RUN" = "false" ] || return 0

  service="warmbly-$WARMBLY_ROLE"
  if blobs_are_local; then
    root=$(blob_root)
    if [ -n "$root" ]; then
      # Keep this a standalone statement: make join-check asserts on it by
      # first field, because a looser match was satisfied by the name
      # appearing inside a warn string.
      ensure_blob_root "$root"
    fi
  fi
  render_unit > "/etc/systemd/system/$service.service"

  # Keep this a standalone statement: make join-check asserts on it by first field.
  install_updater
  systemctl daemon-reload
  log "Installed $service.service and warmbly-node-update.timer"
}

start_node() {
  [ "$DRY_RUN" = "false" ] || return 0
  service="warmbly-$WARMBLY_ROLE"

  log "Pulling $WARMBLY_IMAGE_REPO/$WARMBLY_ROLE:$DESIRED_VERSION"
  docker pull "$WARMBLY_IMAGE_REPO/$WARMBLY_ROLE:$DESIRED_VERSION" >/dev/null

  systemctl enable --now warmbly-node-update.timer >/dev/null 2>&1 || true
  systemctl enable "$service" >/dev/null 2>&1 || true
  systemctl restart "$service"

  log ""
  log "Done. This machine is now a Warmbly $WARMBLY_ROLE."
  log ""
  log "  Node id      $NODE_ID"
  log "  Version      $DESIRED_VERSION"
  log "  Config       $CONFIG_DIR/node.env (rewritten on every join)"
  log "  Yours        $CONFIG_DIR/node.local.env (never rewritten; wins on conflict)"
  log "  Logs         journalctl -u $service -f"
  log "  Status       systemctl status $service"
  log ""
  log "It will appear in Fleet within a minute or two, and will keep itself on"
  log "whatever version you set there. Nothing else to do."
}

main() {
  # The calls below are asserted on by `make join-check`, matched on their
  # first field, so each stays a standalone statement. Reformatting one into
  # `if ! x; then` fails the build with a message about ordering.
  parse_args "$@"
  if [ "$PRINT_UPDATER" = "true" ]; then
    render_updater
    return 0
  fi
  if [ "$ADOPT" = "true" ]; then
    adopt
    return 0
  fi
  if [ "$PRINT_UNIT" = "true" ]; then
    # No enrolment, no network, no files. NODE_ENV is whatever the caller
    # supplied, so the blob branches can be exercised from a test.
    WARMBLY_ROLE="${WARMBLY_ROLE:-worker}"
    NODE_ENV="${NODE_ENV:-}"
    validate_blob_root
    render_unit
    return 0
  fi
  require_args
  check_deps
  enrol
  # Validate the config we were handed before writing any of it: failing later
  # leaves an enrolled node with files on disk and no service.
  validate_blob_root
  write_config
  install_units
  start_node
  warn_shared_blobs
  warn_missing_db
  return 0
}

main "$@"
