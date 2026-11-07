#!/usr/bin/env bash
# Bind private recovery inputs to their reviewed S3 versions and file hashes.
set -euo pipefail
guard="$(dirname "$0")/runtime-tls-recovery.sh"

check_contract() {
  bash "$guard" check-baseline "$1"
  jq -e '
    .bucket == "warmbly-terraform-use1-925323749693" and
    .state.key == "warmbly/production/runtime.tfstate" and
    (.snapshot.key | type == "string" and test("^warmbly/production/vars/runtime-tls-recovery/[a-z0-9][a-z0-9-]{1,63}\\.tfvars\\.json$")) and
    (.snapshot.versionId | type == "string" and length > 0 and . != "null" and (test("[\\r\\n]") | not)) and
    (.snapshot.sha256 | type == "string" and test("^[a-f0-9]{64}$")) and
    (.snapshot.releaseTag | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
  ' "$1" >/dev/null || { echo 'Recovery requires separately versioned private state and cleanup inputs' >&2; return 1; }
}

collect_inputs() {
  local baseline=$1 work=$2 require_latest=${3:-true} bucket key version digest kind
  check_contract "$baseline"
  umask 077
  mkdir "$work"
  cp "$baseline" "$work/baseline.json"
  baseline="$work/baseline.json"
  bucket=$(jq -r '.bucket' "$baseline")
  aws sts get-caller-identity --region us-east-1 >"$work/identity.json"
  jq -e '.Account == "925323749693"' "$work/identity.json" >/dev/null || {
    echo 'Recovery input collection requires the reviewed AWS account' >&2; return 1;
  }
  for kind in state snapshot; do
    key=$(jq -r --arg kind "$kind" '.[$kind].key' "$baseline")
    version=$(jq -r --arg kind "$kind" '.[$kind].versionId' "$baseline")
    aws s3api get-object --region us-east-1 --bucket "$bucket" --key "$key" \
      --version-id "$version" --expected-bucket-owner 925323749693 "$work/$kind.json" >"$work/$kind-metadata.json"
    jq -e --arg version "$version" '
      .VersionId == $version and .DeleteMarker != true and
      (.ServerSideEncryption == "AES256" or .ServerSideEncryption == "aws:kms")
    ' "$work/$kind-metadata.json" >/dev/null || {
      echo 'Recovery object metadata differs from the reviewed version' >&2; return 1;
    }
    digest=$(sha256sum "$work/$kind.json" | cut -d' ' -f1)
    [ "$digest" = "$(jq -r --arg kind "$kind" '.[$kind].sha256' "$baseline")" ] || {
      echo 'Recovery object bytes differ from the reviewed hash' >&2; return 1;
    }
  done
  bash "$guard" check-state "$work/state.json" "$baseline"
  jq -e '.target_tls == "on" and .name == "warmbly-production"' "$work/snapshot.json" >/dev/null || {
    echo 'Recovery vars must select the reviewed production cleanup stage' >&2; return 1;
  }
  if [ "$require_latest" = true ]; then
    aws s3api head-object --region us-east-1 --bucket "$bucket" --key warmbly/production/runtime.tfstate \
      --expected-bucket-owner 925323749693 >"$work/latest-state.json"
    jq -e --slurpfile baseline "$baseline" '.VersionId == $baseline[0].state.versionId' "$work/latest-state.json" >/dev/null || {
      echo 'Recovery state advanced beyond the reviewed version' >&2; return 1;
    }
  fi
  jq '{account,region,bucket,state:{key:.state.key,versionId:.state.versionId,sha256:.state.sha256,lineage:.state.lineage,serial:.state.serial},snapshot}' \
    "$baseline" >"$work/inputs.json"
}

case "${1:-}" in
  check-contract) check_contract "${2:?}" ;;
  collect) collect_inputs "${2:?}" "${3:?}" ;;
  collect-archived) collect_inputs "${2:?}" "${3:?}" false ;;
  *) echo 'Only read-only recovery input validation and collection are supported' >&2; exit 1 ;;
esac
