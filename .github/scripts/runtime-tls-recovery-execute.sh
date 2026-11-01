#!/usr/bin/env bash
# Keep partial cleanup separate from normal route, drain and ON transitions.
set -euo pipefail
scripts=$(cd "$(dirname "$0")" && pwd)

check_contract() {
  bash "$scripts/runtime-tls-recovery-inputs.sh" check-contract "$1"
  jq -e '
    .schemaVersion == 1 and .repository == "warmbly/warmbly" and .release.tag == .snapshot.releaseTag and
    (.release.imagesSha256 | type == "string" and test("^[a-f0-9]{64}$")) and
    (.planChangesSha256 | type == "string" and test("^[a-f0-9]{64}$"))
  ' "$1" >/dev/null || { echo 'Recovery must bind the current release artifact and reviewed complete plan changes' >&2; return 1; }
}

check_context() {
  [ "${GITHUB_ACTIONS:-}" = true ] && [ "${GITHUB_REPOSITORY:-}" = warmbly/warmbly ] &&
    [ "${GITHUB_REF:-}" = refs/heads/main ] && [ "${GITHUB_EVENT_NAME:-}" = workflow_dispatch ] &&
    [ "${GITHUB_WORKFLOW_REF:-}" = warmbly/warmbly/.github/workflows/runtime-tls-recovery.yml@refs/heads/main ] &&
    [ "${GITHUB_RUN_ATTEMPT:-}" = 1 ] && [[ "${GITHUB_RUN_ID:-}" =~ ^[1-9][0-9]*$ ]] || {
    echo 'Recovery execution requires a new protected main workflow run; reruns are refused' >&2; return 1;
  }
  [ "${GITHUB_SHA:?}" = "$(git rev-parse HEAD)" ]
}

check_approval_environment() {
  gh api repos/warmbly/warmbly/environments/aws-infrastructure-approval |
    jq -e '.can_admins_bypass == false and
      any(.protection_rules[]; .type == "required_reviewers" and .prevent_self_review == true and (.reviewers | length > 0))' >/dev/null || {
    echo 'Recovery requires a separate protected reviewer without self-review or administrator bypass' >&2; return 1;
  }
}

check_final_state() {
  jq -e --slurpfile before "$2" '
    .version == 4 and .lineage == $before[0].lineage and
    (.serial | type == "number" and floor == .) and .serial > $before[0].serial and
    ([.resources[] | select(.mode == "managed" and .type == "aws_lb_target_group" and .name == "service")] | length == 0) and
    (.resources | sort_by([.module,.mode,.type,.name])) ==
      ($before[0].resources | map(select((.mode == "managed" and .type == "aws_lb_target_group" and .name == "service") | not)) | sort_by([.module,.mode,.type,.name]))
  ' "$1" >/dev/null || { echo 'Post-cleanup state must preserve all other reviewed resources' >&2; return 1; }
}

prepare() {
  : "${WORK:?}" "${CONTRACT_KEY:?}" "${CONTRACT_VERSION:?}" "${CONTRACT_SHA256:?}"
  [[ "$CONTRACT_KEY" =~ ^warmbly/production/vars/runtime-tls-recovery/[a-z0-9][a-z0-9-]{1,63}\.contract\.json$ ]]
  [[ "$CONTRACT_SHA256" =~ ^[a-f0-9]{64}$ && "$CONTRACT_VERSION" != null && -n "$CONTRACT_VERSION" && "$CONTRACT_VERSION" != *$'\n'* ]]
  umask 077
  mkdir "$WORK"
  export AWS_PAGER=""
  aws sts get-caller-identity --region us-east-1 >"$WORK/account.json"
  jq -e '.Account == "925323749693"' "$WORK/account.json" >/dev/null
  aws s3api get-object --region us-east-1 --bucket warmbly-terraform-use1-925323749693 --key "$CONTRACT_KEY" \
    --version-id "$CONTRACT_VERSION" --expected-bucket-owner 925323749693 "$WORK/contract.json" >"$WORK/contract-metadata.json"
  jq -e --arg version "$CONTRACT_VERSION" '.VersionId == $version and .DeleteMarker != true and
    (.ServerSideEncryption == "AES256" or .ServerSideEncryption == "aws:kms")' "$WORK/contract-metadata.json" >/dev/null
  [ "$(sha256sum "$WORK/contract.json" | cut -d' ' -f1)" = "$CONTRACT_SHA256" ]
  check_contract "$WORK/contract.json"
  check_approval_environment
  bash "$scripts/runtime-tls-recovery-lineage.sh" collect "$WORK/contract.json" "$WORK/lineage" . >"$WORK/lineage.log" 2>&1
  local input_action=collect
  if [ "$1" = verify ]; then input_action=collect-archived; fi
  bash "$scripts/runtime-tls-recovery-inputs.sh" "$input_action" "$WORK/contract.json" "$WORK/inputs" >"$WORK/inputs.log" 2>&1
  local release key digest
  release=$(jq -r '.release.tag' "$WORK/contract.json")
  gh release view "$release" --repo warmbly/warmbly --json tagName,isDraft,isPrerelease >"$WORK/release.json"
  jq -e --arg release "$release" '.tagName == $release and .isDraft == false and .isPrerelease == false' "$WORK/release.json" >/dev/null
  gh release download "$release" --repo warmbly/warmbly --pattern images.json --dir "$WORK/inputs"
  [ "$(sha256sum "$WORK/inputs/images.json" | cut -d' ' -f1)" = "$(jq -r '.release.imagesSha256' "$WORK/contract.json")" ]
  jq -e --arg release "$release" '.tag == $release and .registry == "ghcr.io/warmbly/warmbly"' "$WORK/inputs/images.json" >/dev/null
  for key in backend backend-kafka consumer consumer-kafka worker worker-kafka forms tracking tracking-kafka realtime; do
    digest=$(jq -er --arg key "$key" '.images[$key] | select(type == "string" and test("^sha256:[a-f0-9]{64}$"))' "$WORK/inputs/images.json")
    gh attestation verify "oci://ghcr.io/warmbly/warmbly/${key%-kafka}@$digest" --repo warmbly/warmbly \
      --signer-workflow warmbly/warmbly/.github/workflows/release.yml --source-ref "refs/tags/$release" \
      --source-digest "$(jq -r '.release.source' "$WORK/contract.json")" >>"$WORK/provenance.log" 2>&1
  done
}

verify_final() {
  aws s3api get-object --region us-east-1 --bucket warmbly-terraform-use1-925323749693 \
    --key warmbly/production/runtime.tfstate --expected-bucket-owner 925323749693 "$WORK/final-state.json" >"$WORK/final-state-metadata.json"
  jq -e '.DeleteMarker != true and (.VersionId | type == "string" and length > 0 and . != "null") and
    (.ServerSideEncryption == "AES256" or .ServerSideEncryption == "aws:kms")' "$WORK/final-state-metadata.json" >/dev/null
  check_final_state "$WORK/final-state.json" "$WORK/inputs/state.json"
  bash "$scripts/runtime-tls-recovery-live.sh" collect-final "$WORK/contract.json" "$WORK/inputs" "$WORK/final-live" >"$WORK/final-live.log" 2>&1
  aws s3api head-object --region us-east-1 --bucket warmbly-terraform-use1-925323749693 \
    --key warmbly/production/runtime.tfstate --expected-bucket-owner 925323749693 >"$WORK/final-state-head.json"
  jq -e --slurpfile metadata "$WORK/final-state-metadata.json" '.VersionId == $metadata[0].VersionId' "$WORK/final-state-head.json" >/dev/null
}

main() {
  local action=${1:-}
  if [ "$action" = check-contract ]; then check_contract "${2:?}"; return; fi
  if [ "$action" = check-final-state ]; then check_final_state "${2:?}" "${3:?}"; return; fi
  case "$action" in plan|apply|verify) ;; *) echo 'Unknown partial TLS recovery action' >&2; return 1 ;; esac
  check_context
  prepare "$action"
  if [ "$action" = verify ]; then verify_final; echo 'Final recovery state and effective live checks passed'; return; fi
  bash "$scripts/runtime-tls-recovery-live.sh" collect "$WORK/contract.json" "$WORK/inputs" "$WORK/live" >"$WORK/live.log" 2>&1
  local dir=deploy/terraform/aws/runtime changes digest
  terraform -chdir="$dir" init -input=false -no-color -backend-config=bucket=warmbly-terraform-use1-925323749693 >"$WORK/terraform.log" 2>&1
  terraform -chdir="$dir" plan -input=false -no-color -lock-timeout=5m -var-file="$WORK/inputs/snapshot.json" \
    -var=state_bucket=warmbly-terraform-use1-925323749693 -target=aws_lb_target_group.service \
    -out="$WORK/recovery.tfplan" >>"$WORK/terraform.log" 2>&1
  terraform -chdir="$dir" show -json "$WORK/recovery.tfplan" >"$WORK/plan.json"
  bash "$scripts/runtime-tls-recovery.sh" check-plan "$WORK/plan.json" "$WORK/contract.json"
  changes=$(jq -cS '{resource_changes:(.resource_changes | sort_by(.address)),resource_drift:(.resource_drift // [] | sort_by(.address))}' \
    "$WORK/plan.json" | sha256sum | cut -d' ' -f1)
  [ "$changes" = "$(jq -r '.planChangesSha256' "$WORK/contract.json")" ] || {
    echo 'The complete recovery changes differ from the privately reviewed plan; prepare a new contract' >&2; return 1;
  }
  digest=$(jq -cnS --arg contract "$CONTRACT_SHA256" --arg source "$GITHUB_SHA" --arg changes "$changes" \
    '{contract:$contract,workflowSource:$source,changes:$changes}' | sha256sum | cut -d' ' -f1)
  aws s3api head-object --region us-east-1 --bucket warmbly-terraform-use1-925323749693 \
    --key warmbly/production/runtime.tfstate --expected-bucket-owner 925323749693 >"$WORK/preapply-state-head.json"
  jq -e --slurpfile contract "$WORK/contract.json" '.VersionId == $contract[0].state.versionId' "$WORK/preapply-state-head.json" >/dev/null
  if [ "$action" = plan ]; then
    printf 'digest=%s\nrelease=%s\n' "$digest" "$(jq -r '.release.tag' "$WORK/contract.json")" >>"${GITHUB_OUTPUT:?}"
    printf 'Partial TLS cleanup: exactly four reviewed HTTP target-group deletions.\n\nContract SHA-256: %s\n\nComplete changes SHA-256: %s\n\nApproval digest: %s\n\nReview the matching complete private plan before approving.\n' \
      "$CONTRACT_SHA256" "$changes" "$digest" >>"${GITHUB_STEP_SUMMARY:?}"
    return
  fi
  [ "$digest" = "${APPROVED_DIGEST:?}" ] || { echo 'Recovery approval digest changed; a new reviewed run is required' >&2; return 1; }
  bash "$scripts/runtime-tls-recovery-live.sh" collect "$WORK/contract.json" "$WORK/inputs" "$WORK/preapply-live" >"$WORK/preapply-live.log" 2>&1
  terraform -chdir="$dir" apply -input=false -no-color -lock-timeout=5m "$WORK/recovery.tfplan" >>"$WORK/terraform.log" 2>&1 || {
    echo 'Partial cleanup failed; inspect private state before preparing any new recovery contract' >&2; return 1;
  }
  verify_final
  echo 'Reviewed partial cleanup and effective checks passed; attested same-release redeploy is still required'
}
main "$@"
