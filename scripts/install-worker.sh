#!/usr/bin/env bash
#
# Warmbly worker installer.
#
# Drops a Warmbly worker on any Linux VPS in one command. The worker runs as a
# Docker container under systemd, with config in /etc/warmbly/worker.env.
#
# Worker identity is derived deterministically from the VPS's public IPv4
# address (UUIDv5, URL namespace), so:
#
#   - same IP   -> same worker  (reputation persists across reinstalls)
#   - new IP    -> new worker   (fresh identity, no inherited reputation)
#
# Quick start:
#
#   curl -fsSL https://get.warmbly.com/worker | sudo bash -s -- \
#     --kafka kafka.warmbly.com:9092 \
#     --schema-registry https://schema.warmbly.com \
#     --redis redis://cache.warmbly.com:6379 \
#     --aws-key AKIA... --aws-secret ... --aws-region us-east-1
#
# Or with a pre-built env file:
#
#   curl -fsSL https://get.warmbly.com/worker | sudo bash -s -- \
#     --env-file /root/worker.env
#
# Re-running is safe: existing env values are preserved unless overridden,
# and the worker ID will resolve to the same value as long as the IP is stable.

set -euo pipefail

# ---------- defaults ----------

IMAGE="${WARMBLY_WORKER_IMAGE:-ghcr.io/warmbly/worker:latest}"
CONFIG_DIR="/etc/warmbly"
ENV_FILE="${CONFIG_DIR}/worker.env"
ID_FILE="${CONFIG_DIR}/worker.id"
UNIT_FILE="/etc/systemd/system/warmbly-worker.service"
CONTAINER_NAME="warmbly-worker"

ACTION="install"
INTERACTIVE=1
SUPPLIED_ENV_FILE=""

declare -A CFG=(
  [APP_ENV]="prod"
  [AWS_CONFIG_ENABLED]="true"
  [AWS_REGION]=""
  [AWS_ACCESS_KEY_ID]=""
  [AWS_SECRET_ACCESS_KEY]=""
  [KAFKA_BOOTSTRAP_SERVERS]=""
  [KAFKA_SASL_USERNAME]=""
  [KAFKA_SASL_PASSWORD]=""
  [SCHEMA_REGISTRY_URL]=""
  [SCHEMA_REGISTRY_KEY]=""
  [SCHEMA_REGISTRY_SECRET]=""
  [REDIS]=""
  [WORKER_TIER]="shared"
)

WORKER_ID_OVERRIDE=""
IP_OVERRIDE=""

# UUIDv5 URL namespace (RFC 4122)
UUID_NS_URL="6ba7b811-9dad-11d1-80b4-00c04fd430c8"

# ---------- pretty output ----------

if [[ -t 1 ]]; then
  C_RED=$'\033[0;31m'; C_GRN=$'\033[0;32m'; C_YLW=$'\033[0;33m'
  C_BLU=$'\033[0;34m'; C_BLD=$'\033[1m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_YLW=""; C_BLU=""; C_BLD=""; C_RST=""
fi

log()   { printf "%s==>%s %s\n" "$C_BLU" "$C_RST" "$*"; }
ok()    { printf "%s ok %s %s\n" "$C_GRN" "$C_RST" "$*"; }
warn()  { printf "%s !! %s %s\n" "$C_YLW" "$C_RST" "$*" >&2; }
die()   { printf "%s xx %s %s\n" "$C_RED" "$C_RST" "$*" >&2; exit 1; }

# ---------- usage ----------

usage() {
  cat <<EOF
${C_BLD}warmbly worker installer${C_RST}

Usage:
  install-worker.sh [flags]

Actions:
  --install             Install or reconfigure the worker (default)
  --update              Pull the latest image and restart
  --uninstall           Stop and remove the worker (keeps config)
  --purge               Stop, remove, and delete /etc/warmbly entirely
  --status              Show worker status and recent logs

Configuration flags:
  --ip <ipv4>                  Public IP to derive worker identity from
                               (default: auto-detected via ifconfig.me/etc.)
  --worker-id <uuid>           Pin a specific worker UUID, bypassing IP derivation
  --tier <shared|dedicated>    Worker tier label (default: shared)
  --image <ref>                Docker image (default: ${IMAGE})
  --env-file <path>            Use this env file verbatim, skip prompts

  --kafka <bootstrap>          Kafka bootstrap servers (host:port[,host:port])
  --kafka-user <user>
  --kafka-pass <pass>

  --schema-registry <url>
  --schema-key <key>
  --schema-secret <secret>

  --redis <url>                e.g. redis://default:pass@host:6379

  --aws-region <region>
  --aws-key <key>
  --aws-secret <secret>

  --env KEY=value              Set an arbitrary env var (repeatable)

  --non-interactive            Fail instead of prompting for missing values
  -h, --help                   This help
EOF
}

# ---------- arg parsing ----------

EXTRA_ENVS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install)         ACTION="install"; shift ;;
    --update)          ACTION="update"; shift ;;
    --uninstall)       ACTION="uninstall"; shift ;;
    --purge)           ACTION="purge"; shift ;;
    --status)          ACTION="status"; shift ;;

    --ip)              IP_OVERRIDE="$2"; shift 2 ;;
    --worker-id)       WORKER_ID_OVERRIDE="$2"; shift 2 ;;
    --tier)            CFG[WORKER_TIER]="$2"; shift 2 ;;
    --image)           IMAGE="$2"; shift 2 ;;
    --env-file)        SUPPLIED_ENV_FILE="$2"; shift 2 ;;

    --kafka)           CFG[KAFKA_BOOTSTRAP_SERVERS]="$2"; shift 2 ;;
    --kafka-user)      CFG[KAFKA_SASL_USERNAME]="$2"; shift 2 ;;
    --kafka-pass)      CFG[KAFKA_SASL_PASSWORD]="$2"; shift 2 ;;

    --schema-registry) CFG[SCHEMA_REGISTRY_URL]="$2"; shift 2 ;;
    --schema-key)      CFG[SCHEMA_REGISTRY_KEY]="$2"; shift 2 ;;
    --schema-secret)   CFG[SCHEMA_REGISTRY_SECRET]="$2"; shift 2 ;;

    --redis)           CFG[REDIS]="$2"; shift 2 ;;

    --aws-region)      CFG[AWS_REGION]="$2"; shift 2 ;;
    --aws-key)         CFG[AWS_ACCESS_KEY_ID]="$2"; shift 2 ;;
    --aws-secret)      CFG[AWS_SECRET_ACCESS_KEY]="$2"; shift 2 ;;

    --env)             EXTRA_ENVS+=("$2"); shift 2 ;;

    --non-interactive) INTERACTIVE=0; shift ;;
    -h|--help)         usage; exit 0 ;;
    *)                 die "Unknown flag: $1 (use --help)" ;;
  esac
done

# ---------- preflight ----------

require_root() {
  if [[ $EUID -ne 0 ]]; then
    die "Run as root (try: sudo $0 ...)"
  fi
}

detect_pkg_manager() {
  if command -v apt-get >/dev/null 2>&1; then echo "apt"
  elif command -v dnf >/dev/null 2>&1; then echo "dnf"
  elif command -v yum >/dev/null 2>&1; then echo "yum"
  elif command -v pacman >/dev/null 2>&1; then echo "pacman"
  elif command -v apk >/dev/null 2>&1; then echo "apk"
  else echo "unknown"
  fi
}

ensure_docker() {
  if command -v docker >/dev/null 2>&1; then
    ok "docker present"
    return
  fi
  log "installing docker via get.docker.com"
  if ! command -v curl >/dev/null 2>&1; then
    case "$(detect_pkg_manager)" in
      apt)    apt-get update -qq && apt-get install -y curl ca-certificates ;;
      dnf)    dnf install -y curl ca-certificates ;;
      yum)    yum install -y curl ca-certificates ;;
      pacman) pacman -Sy --noconfirm curl ca-certificates ;;
      apk)    apk add --no-cache curl ca-certificates ;;
      *)      die "curl missing and package manager unknown" ;;
    esac
  fi
  curl -fsSL https://get.docker.com | sh
  systemctl enable --now docker || true
  command -v docker >/dev/null 2>&1 || die "docker install failed"
  ok "docker installed"
}

ensure_uuidgen() {
  if command -v uuidgen >/dev/null 2>&1; then return; fi
  case "$(detect_pkg_manager)" in
    apt)    apt-get update -qq && apt-get install -y uuid-runtime ;;
    dnf)    dnf install -y util-linux ;;
    yum)    yum install -y util-linux ;;
    pacman) pacman -Sy --noconfirm util-linux ;;
    apk)    apk add --no-cache util-linux ;;
    *)      ;;
  esac
}

valid_ipv4() {
  [[ "$1" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
  local IFS=.
  local -a octets=($1)
  for o in "${octets[@]}"; do
    (( o >= 0 && o <= 255 )) || return 1
  done
  return 0
}

detect_public_ip() {
  local candidates=(
    "https://api.ipify.org"
    "https://ifconfig.me"
    "https://checkip.amazonaws.com"
    "https://icanhazip.com"
  )
  local ip
  for url in "${candidates[@]}"; do
    ip="$(curl -fsS --max-time 5 "$url" 2>/dev/null | tr -d '[:space:]')" || continue
    if valid_ipv4 "$ip"; then
      echo "$ip"
      return 0
    fi
  done
  # last resort: first non-loopback v4 on the host
  if command -v hostname >/dev/null 2>&1; then
    ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
    if valid_ipv4 "$ip"; then
      warn "using local interface IP $ip (could not reach public IP services)"
      echo "$ip"
      return 0
    fi
  fi
  return 1
}

# UUIDv5(URL namespace, ip) — deterministic, so same IP always maps to same UUID.
derive_uuid_from_ip() {
  local ip="$1"
  if command -v uuidgen >/dev/null 2>&1 && uuidgen --help 2>&1 | grep -q -- '--sha1'; then
    uuidgen --sha1 --namespace "$UUID_NS_URL" --name "$ip" | tr '[:upper:]' '[:lower:]'
    return
  fi
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "import uuid;print(uuid.uuid5(uuid.UUID('$UUID_NS_URL'), '$ip'))"
    return
  fi
  if command -v python >/dev/null 2>&1; then
    python -c "import uuid;print(uuid.uuid5(uuid.UUID('$UUID_NS_URL'), '$ip'))"
    return
  fi
  die "cannot derive UUIDv5: need 'uuidgen --sha1' (util-linux >= 2.34) or python"
}

# ---------- config helpers ----------

prompt_if_missing() {
  local key="$1" label="$2" secret="${3:-0}" default="${4:-}"
  if [[ -n "${CFG[$key]:-}" ]]; then return; fi
  if [[ $INTERACTIVE -eq 0 ]]; then
    [[ -n "$default" ]] && CFG[$key]="$default" && return
    die "missing required value for $key (--$(echo "$key" | tr '[:upper:]_' '[:lower:]-'))"
  fi
  local prompt="$label"
  [[ -n "$default" ]] && prompt+=" [$default]"
  prompt+=": "
  local val
  if [[ $secret -eq 1 ]]; then
    read -rsp "$prompt" val; echo
  else
    read -rp "$prompt" val
  fi
  CFG[$key]="${val:-$default}"
}

merge_existing_env() {
  [[ -f "$ENV_FILE" ]] || return 0
  log "merging existing $ENV_FILE"
  while IFS='=' read -r k v; do
    [[ -z "$k" || "$k" =~ ^# ]] && continue
    # strip surrounding quotes if present
    v="${v%\"}"; v="${v#\"}"
    if [[ -z "${CFG[$k]:-}" ]]; then
      CFG[$k]="$v"
    fi
  done < "$ENV_FILE"
}

write_env_file() {
  install -d -m 0700 "$CONFIG_DIR"
  local tmp; tmp="$(mktemp)"
  {
    echo "# Warmbly worker config — managed by install-worker.sh"
    echo "# $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    for key in APP_ENV AWS_CONFIG_ENABLED AWS_REGION AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY \
               KAFKA_BOOTSTRAP_SERVERS KAFKA_SASL_USERNAME KAFKA_SASL_PASSWORD \
               SCHEMA_REGISTRY_URL SCHEMA_REGISTRY_KEY SCHEMA_REGISTRY_SECRET \
               REDIS WORKER_TIER; do
      printf '%s=%s\n' "$key" "${CFG[$key]}"
    done
    for kv in "${EXTRA_ENVS[@]}"; do
      printf '%s\n' "$kv"
    done
  } > "$tmp"
  install -m 0600 "$tmp" "$ENV_FILE"
  rm -f "$tmp"
  ok "wrote $ENV_FILE"
}

resolve_worker_id() {
  local ip="${IP_OVERRIDE}"
  local id

  if [[ -n "$WORKER_ID_OVERRIDE" ]]; then
    id="$WORKER_ID_OVERRIDE"
    ip="manual"
  else
    if [[ -z "$ip" ]]; then
      ip="$(detect_public_ip)" || die "could not detect public IP (pass --ip <ipv4>)"
    fi
    valid_ipv4 "$ip" || [[ "$ip" == "manual" ]] || die "invalid IPv4: $ip"
    id="$(derive_uuid_from_ip "$ip")"
  fi

  install -d -m 0700 "$CONFIG_DIR"
  printf 'id=%s\nip=%s\n' "$id" "$ip" > "$ID_FILE"
  chmod 0644 "$ID_FILE"

  WORKER_IP="$ip"
  echo "$id"
}

write_unit() {
  local worker_id="$1"
  cat > "$UNIT_FILE" <<EOF
[Unit]
Description=Warmbly worker
Documentation=https://docs.warmbly.com/workers
Wants=network-online.target docker.service
After=network-online.target docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=10
TimeoutStartSec=0

ExecStartPre=-/usr/bin/docker rm -f ${CONTAINER_NAME}
ExecStartPre=/usr/bin/docker pull ${IMAGE}
ExecStart=/usr/bin/docker run --rm \\
  --name ${CONTAINER_NAME} \\
  --hostname ${worker_id} \\
  --network host \\
  --env-file ${ENV_FILE} \\
  --log-driver=journald \\
  ${IMAGE}

ExecStop=/usr/bin/docker stop -t 30 ${CONTAINER_NAME}

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$UNIT_FILE"
  systemctl daemon-reload
  ok "wrote $UNIT_FILE"
}

# ---------- actions ----------

do_install() {
  require_root
  ensure_docker
  ensure_uuidgen

  if [[ -n "$SUPPLIED_ENV_FILE" ]]; then
    [[ -f "$SUPPLIED_ENV_FILE" ]] || die "env file not found: $SUPPLIED_ENV_FILE"
    install -d -m 0700 "$CONFIG_DIR"
    install -m 0600 "$SUPPLIED_ENV_FILE" "$ENV_FILE"
    ok "installed env file from $SUPPLIED_ENV_FILE"
  else
    merge_existing_env
    prompt_if_missing KAFKA_BOOTSTRAP_SERVERS "Kafka bootstrap servers"
    prompt_if_missing SCHEMA_REGISTRY_URL     "Schema Registry URL"
    prompt_if_missing REDIS                   "Redis URL"
    prompt_if_missing AWS_REGION              "AWS region" 0 "us-east-1"
    prompt_if_missing AWS_ACCESS_KEY_ID       "AWS access key ID"
    prompt_if_missing AWS_SECRET_ACCESS_KEY   "AWS secret access key" 1

    # SASL + schema auth are optional; only prompt if interactive and unset
    if [[ $INTERACTIVE -eq 1 ]]; then
      [[ -z "${CFG[KAFKA_SASL_USERNAME]}" ]] && read -rp "Kafka SASL user (leave blank if none): "   CFG[KAFKA_SASL_USERNAME] || true
      [[ -z "${CFG[KAFKA_SASL_PASSWORD]}" && -n "${CFG[KAFKA_SASL_USERNAME]}" ]] && { read -rsp "Kafka SASL pass: " CFG[KAFKA_SASL_PASSWORD]; echo; }
      [[ -z "${CFG[SCHEMA_REGISTRY_KEY]}"  ]] && read -rp "Schema Registry key (leave blank if none): " CFG[SCHEMA_REGISTRY_KEY]  || true
      [[ -z "${CFG[SCHEMA_REGISTRY_SECRET]}" && -n "${CFG[SCHEMA_REGISTRY_KEY]}" ]] && { read -rsp "Schema Registry secret: " CFG[SCHEMA_REGISTRY_SECRET]; echo; }
    fi

    write_env_file
  fi

  local worker_id; worker_id="$(resolve_worker_id)"
  log "public ip: ${C_BLD}${WORKER_IP}${C_RST}"
  log "worker id: ${C_BLD}${worker_id}${C_RST}  (derived from ip)"

  write_unit "$worker_id"

  systemctl enable warmbly-worker.service >/dev/null 2>&1 || true
  systemctl restart warmbly-worker.service
  ok "warmbly-worker started"

  sleep 2
  if systemctl is-active --quiet warmbly-worker.service; then
    ok "worker is running"
  else
    warn "worker not active; check: journalctl -u warmbly-worker -n 100 --no-pager"
  fi

  cat <<EOF

${C_BLD}Worker installed.${C_RST}

  public ip    ${WORKER_IP}
  worker id    ${worker_id}
  image        ${IMAGE}
  env file     ${ENV_FILE}
  unit         ${UNIT_FILE}

Same IP always resolves to the same worker id. If this VPS's public IP
changes, the worker id will change too — re-run this script to refresh.

Register this worker id in the Warmbly control plane to start receiving work.

Manage:
  systemctl status warmbly-worker
  journalctl -u warmbly-worker -f
  $0 --update
  $0 --uninstall

EOF
}

do_update() {
  require_root
  [[ -f "$UNIT_FILE" ]] || die "worker not installed (no $UNIT_FILE)"

  # Recover worker id from /etc/warmbly/worker.id so the regenerated unit
  # keeps the same hostname and therefore the same Kafka topic subscription.
  local worker_id=""
  if [[ -f "$ID_FILE" ]]; then
    # ID_FILE is either a bare UUID (legacy) or "id=<uuid>\nip=<ip>" (current).
    if grep -q '^id=' "$ID_FILE"; then
      worker_id="$(grep '^id=' "$ID_FILE" | head -1 | cut -d= -f2- | tr -d '[:space:]')"
    else
      worker_id="$(head -1 "$ID_FILE" | tr -d '[:space:]')"
    fi
  fi
  [[ -n "$worker_id" ]] || die "could not read worker id (reinstall required)"

  WORKER_IP="$(grep '^ip=' "$ID_FILE" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '[:space:]')"

  log "pulling $IMAGE"
  docker pull "$IMAGE"

  log "rewriting systemd unit"
  write_unit "$worker_id"

  systemctl restart warmbly-worker.service
  ok "worker updated to $IMAGE"
}

do_uninstall() {
  require_root
  log "stopping warmbly-worker"
  systemctl stop warmbly-worker.service 2>/dev/null || true
  systemctl disable warmbly-worker.service 2>/dev/null || true
  rm -f "$UNIT_FILE"
  systemctl daemon-reload
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  ok "worker removed (config preserved in $CONFIG_DIR)"
}

do_purge() {
  do_uninstall
  rm -rf "$CONFIG_DIR"
  ok "purged $CONFIG_DIR"
}

do_status() {
  if [[ ! -f "$UNIT_FILE" ]]; then
    warn "not installed"
    exit 1
  fi
  systemctl status warmbly-worker.service --no-pager || true
  echo
  log "recent logs:"
  journalctl -u warmbly-worker -n 30 --no-pager || true
}

# ---------- dispatch ----------

case "$ACTION" in
  install)   do_install ;;
  update)    do_update ;;
  uninstall) do_uninstall ;;
  purge)     do_purge ;;
  status)    do_status ;;
  *)         die "unknown action: $ACTION" ;;
esac
