#!/usr/bin/env bash
# Run Compose with the CONFENGE override and private environment every time.
set -euo pipefail

PACK="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
source "$PACK/lib.sh"
load_vps_env
compose_cmd "$@"
