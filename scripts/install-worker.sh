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
TEMPLATE_UNIT_FILE="/etc/systemd/system/warmbly-worker@.service"
INSTANCES_DIR="${CONFIG_DIR}/instances"
CONTAINER_NAME="warmbly-worker"

ACTION="install"
INTERACTIVE=1
SUPPLIED_ENV_FILE=""

# Comma-separated list of IPv4 addresses for multi-IP install. When non-empty,
# the installer drops one templated systemd unit per IP, each bound to that IP
# and with a worker_id derived from it (UUIDv5). Empty == legacy single-IP.
IPS_LIST=""

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
  --ips <ipv4,ipv4,...>        Run one worker process per IP, each bound to its
                               own egress (multi-IP box). When set, --ip and
                               --worker-id are ignored.
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
    --ips)             IPS_LIST="$2"; shift 2 ;;
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

# ---------- multi-IP helpers ----------
#
# Multi-IP mode runs one worker process per IP, all on the same box. Each
# instance shares the common ${ENV_FILE} for Kafka/KMS/etc. and adds a
# per-instance drop-in env file at ${INSTANCES_DIR}/<dashed-ip>.env that
# carries WORKER_BIND_IP (and the derived WORKER_ID). systemd's template
# instancing (%i) picks the right env file at start.

# Turns 1.2.3.4 into 1-2-3-4 so it can be used as a systemd instance name.
ip_to_instance() {
  echo "${1//./-}"
}

# Inverse of ip_to_instance.
instance_to_ip() {
  echo "${1//-/.}"
}

parse_ips_list() {
  local raw="$1"
  raw="${raw// /}"
  [[ -n "$raw" ]] || die "--ips given but empty"
  local -a ips
  IFS=',' read -ra ips <<< "$raw"
  local seen=""
  for ip in "${ips[@]}"; do
    valid_ipv4 "$ip" || die "invalid IPv4 in --ips: $ip"
    if [[ ",$seen," == *",$ip,"* ]]; then
      die "duplicate IP in --ips: $ip"
    fi
    seen="${seen},${ip}"
  done
  echo "${ips[@]}"
}

write_instance_env() {
  local ip="$1"
  local worker_id; worker_id="$(derive_uuid_from_ip "$ip")"
  install -d -m 0700 "$INSTANCES_DIR"
  local inst; inst="$(ip_to_instance "$ip")"
  local path="${INSTANCES_DIR}/${inst}.env"
  local tmp; tmp="$(mktemp)"
  {
    echo "# Per-instance config for warmbly-worker@${inst}.service"
    echo "# Generated by install-worker.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "WORKER_BIND_IP=${ip}"
    echo "WORKER_ID=${worker_id}"
  } > "$tmp"
  install -m 0600 "$tmp" "$path"
  rm -f "$tmp"
  echo "$worker_id"
}

write_template_unit() {
  # Container name is per-instance so multiple containers can coexist.
  # %i is the systemd instance specifier, e.g. warmbly-worker@1-2-3-4
  # gives %i=1-2-3-4. We pass it through to the container as the docker
  # name suffix.
  cat > "$TEMPLATE_UNIT_FILE" <<EOF
[Unit]
Description=Warmbly worker (egress %i)
Documentation=https://docs.warmbly.com/workers
Wants=network-online.target docker.service
After=network-online.target docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=10
TimeoutStartSec=0

ExecStartPre=-/usr/bin/docker rm -f ${CONTAINER_NAME}-%i
ExecStartPre=/usr/bin/docker pull ${IMAGE}
ExecStart=/usr/bin/docker run --rm \\
  --name ${CONTAINER_NAME}-%i \\
  --network host \\
  --env-file ${ENV_FILE} \\
  --env-file ${INSTANCES_DIR}/%i.env \\
  --log-driver=journald \\
  ${IMAGE}

ExecStop=/usr/bin/docker stop -t 30 ${CONTAINER_NAME}-%i

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$TEMPLATE_UNIT_FILE"
  systemctl daemon-reload
  ok "wrote $TEMPLATE_UNIT_FILE"
}

list_installed_instances() {
  [[ -d "$INSTANCES_DIR" ]] || return 0
  local f
  for f in "$INSTANCES_DIR"/*.env; do
    [[ -f "$f" ]] || continue
    basename "$f" .env
  done
}

# ---------- actions ----------

prepare_common_env() {
  if [[ -n "$SUPPLIED_ENV_FILE" ]]; then
    [[ -f "$SUPPLIED_ENV_FILE" ]] || die "env file not found: $SUPPLIED_ENV_FILE"
    install -d -m 0700 "$CONFIG_DIR"
    install -m 0600 "$SUPPLIED_ENV_FILE" "$ENV_FILE"
    ok "installed env file from $SUPPLIED_ENV_FILE"
    return
  fi

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
}

do_install() {
  require_root
  ensure_docker
  ensure_uuidgen

  if [[ -n "$IPS_LIST" ]]; then
    do_install_multi
    return
  fi

  prepare_common_env

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
changes, the worker id will change too. Re-run this script to refresh.

Register this worker id in the Warmbly control plane to start receiving work.

Manage:
  systemctl status warmbly-worker
  journalctl -u warmbly-worker -f
  $0 --update
  $0 --uninstall

EOF
}

do_install_multi() {
  # Parse, dedupe, validate the IP list before touching anything on disk.
  local -a ips
  read -ra ips <<< "$(parse_ips_list "$IPS_LIST")"
  local count="${#ips[@]}"

  log "installing ${C_BLD}${count}${C_RST} worker processes, one per IP"

  prepare_common_env
  write_template_unit

  # Track which instance names we end up with so we can disable any stale
  # instances from a previous install with a different IP set.
  local -a desired=()
  local ip inst worker_id
  for ip in "${ips[@]}"; do
    inst="$(ip_to_instance "$ip")"
    worker_id="$(write_instance_env "$ip")"
    desired+=("$inst")
    log "egress ${C_BLD}${ip}${C_RST}  -> instance ${C_BLD}${inst}${C_RST}  worker_id ${worker_id}"
  done

  # Stop any previously-installed instance that's no longer in the desired set.
  local existing
  while IFS= read -r existing; do
    [[ -z "$existing" ]] && continue
    local keep=0
    for d in "${desired[@]}"; do
      [[ "$d" == "$existing" ]] && keep=1 && break
    done
    if [[ $keep -eq 0 ]]; then
      log "removing stale instance: $existing"
      systemctl stop "warmbly-worker@${existing}.service" 2>/dev/null || true
      systemctl disable "warmbly-worker@${existing}.service" 2>/dev/null || true
      docker rm -f "${CONTAINER_NAME}-${existing}" >/dev/null 2>&1 || true
      rm -f "${INSTANCES_DIR}/${existing}.env"
    fi
  done < <(list_installed_instances)

  # If a legacy single-IP unit exists, take it down so it doesn't fight the
  # template instances over network identity.
  if [[ -f "$UNIT_FILE" ]]; then
    warn "legacy ${UNIT_FILE} found; disabling in favour of template instances"
    systemctl stop warmbly-worker.service 2>/dev/null || true
    systemctl disable warmbly-worker.service 2>/dev/null || true
    rm -f "$UNIT_FILE"
    systemctl daemon-reload
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  fi

  for inst in "${desired[@]}"; do
    systemctl enable "warmbly-worker@${inst}.service" >/dev/null 2>&1 || true
    systemctl restart "warmbly-worker@${inst}.service"
  done
  ok "started ${count} worker instances"

  sleep 2
  local healthy=0
  for inst in "${desired[@]}"; do
    if systemctl is-active --quiet "warmbly-worker@${inst}.service"; then
      healthy=$((healthy + 1))
    else
      warn "instance ${inst} not active; check: journalctl -u warmbly-worker@${inst} -n 100 --no-pager"
    fi
  done
  ok "${healthy}/${count} instances healthy"

  cat <<EOF

${C_BLD}Multi-IP workers installed.${C_RST}

  ips           ${IPS_LIST}
  instances     ${count}
  image         ${IMAGE}
  env file      ${ENV_FILE}
  per-instance  ${INSTANCES_DIR}/<dashed-ip>.env
  unit template ${TEMPLATE_UNIT_FILE}

Each instance derives its worker id from its bound IP, so reinstalling with
the same IP set is idempotent. Bringing one IP down only stops that one
instance; the rest keep sending. Hosted Warmbly tip: do not put more than
~25% of your fleet IPs on a single box (blast radius).

Manage:
  systemctl status 'warmbly-worker@*'
  journalctl -u warmbly-worker@1-2-3-4 -f
  $0 --status
  $0 --update --ips ${IPS_LIST}
  $0 --uninstall

EOF
}

do_update() {
  require_root

  local -a instances=()
  while IFS= read -r line; do
    [[ -n "$line" ]] && instances+=("$line")
  done < <(list_installed_instances)

  if [[ ${#instances[@]} -gt 0 ]]; then
    log "pulling $IMAGE"
    docker pull "$IMAGE"

    log "rewriting template unit"
    write_template_unit

    local inst
    for inst in "${instances[@]}"; do
      systemctl restart "warmbly-worker@${inst}.service"
    done
    ok "${#instances[@]} worker instances updated to $IMAGE"
    return
  fi

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

  # Multi-IP: stop every templated instance and remove the template + drop-ins.
  local -a instances=()
  while IFS= read -r line; do
    [[ -n "$line" ]] && instances+=("$line")
  done < <(list_installed_instances)

  if [[ ${#instances[@]} -gt 0 ]]; then
    log "stopping ${#instances[@]} worker instances"
    local inst
    for inst in "${instances[@]}"; do
      systemctl stop "warmbly-worker@${inst}.service" 2>/dev/null || true
      systemctl disable "warmbly-worker@${inst}.service" 2>/dev/null || true
      docker rm -f "${CONTAINER_NAME}-${inst}" >/dev/null 2>&1 || true
      rm -f "${INSTANCES_DIR}/${inst}.env"
    done
    rm -f "$TEMPLATE_UNIT_FILE"
    rmdir "$INSTANCES_DIR" 2>/dev/null || true
  fi

  # Legacy single-IP cleanup (also runs when no instances exist).
  if [[ -f "$UNIT_FILE" ]] || systemctl is-active --quiet warmbly-worker.service 2>/dev/null; then
    log "stopping warmbly-worker"
    systemctl stop warmbly-worker.service 2>/dev/null || true
    systemctl disable warmbly-worker.service 2>/dev/null || true
    rm -f "$UNIT_FILE"
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  fi

  systemctl daemon-reload
  ok "worker removed (config preserved in $CONFIG_DIR)"
}

do_purge() {
  do_uninstall
  rm -rf "$CONFIG_DIR"
  ok "purged $CONFIG_DIR"
}

do_status() {
  local -a instances=()
  while IFS= read -r line; do
    [[ -n "$line" ]] && instances+=("$line")
  done < <(list_installed_instances)

  if [[ ${#instances[@]} -gt 0 ]]; then
    log "multi-IP mode: ${#instances[@]} instance(s)"
    printf "\n  %-18s %-20s %-12s %s\n" "IP" "INSTANCE" "STATE" "WORKER_ID"
    printf "  %-18s %-20s %-12s %s\n" "------------------" "--------------------" "------------" "------------------------------------"
    local inst ip state wid
    for inst in "${instances[@]}"; do
      ip="$(instance_to_ip "$inst")"
      state="$(systemctl is-active "warmbly-worker@${inst}.service" 2>/dev/null || true)"
      wid="$(grep '^WORKER_ID=' "${INSTANCES_DIR}/${inst}.env" 2>/dev/null | head -1 | cut -d= -f2- || true)"
      printf "  %-18s %-20s %-12s %s\n" "$ip" "$inst" "${state:-unknown}" "${wid:-?}"
    done
    echo
    log "tail one instance: ${C_BLD}journalctl -u warmbly-worker@${instances[0]} -f${C_RST}"
    return
  fi

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
