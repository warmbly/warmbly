#!/usr/bin/env bash
# Stop the warmbly-confenge stack (volumes preserved).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"
load_vps_env
cd "$ROOT"
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-warmbly-confenge}"
compose_cmd down
echo "Stopped. Volumes retained (postgres_data, redis_data, nats_data, blobs, confenge_ops)."
