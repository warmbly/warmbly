#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repository/.github/scripts" "$work/bin"
cp .github/scripts/runtime-tls-recovery{,-inputs,-lineage,-execute}.sh "$work/repository/.github/scripts/"
git -C "$work/repository" init -q
git -C "$work/repository" config user.name 'Recovery fixture'
git -C "$work/repository" config user.email 'fixture@example.invalid'
git -C "$work/repository" commit -qm 'feat: establish recovery drain fixture' --allow-empty
drain=$(git -C "$work/repository" rev-parse HEAD)
git -C "$work/repository" commit -qm 'feat: establish recovery ON fixture' --allow-empty
on=$(git -C "$work/repository" rev-parse HEAD)
git -C "$work/repository" commit -qm 'feat: establish recovery release fixture' --allow-empty
release=$(git -C "$work/repository" rev-parse HEAD)
git -C "$work/repository" tag v1.2.3
jq -n --arg digest "sha256:$(printf fixture | sha256sum | cut -d' ' -f1)" \
  -f .github/scripts/runtime-tls-recovery-live.fixture.jq >"$work/fixture.json"
jq '.state' "$work/fixture.json" >"$work/state.json"
jq '.state | .serial+=1 | .resources |= map(select(.type != "aws_lb_target_group" or .name != "service"))' "$work/fixture.json" >"$work/final-state.json"
jq '.vars' "$work/fixture.json" >"$work/snapshot.json"
jq '.images | .images += {"backend-kafka":.images.backend,"consumer-kafka":.images.consumer,"worker":.images.backend,"worker-kafka":.images.backend,"tracking-kafka":.images.tracking}' \
  "$work/fixture.json" >"$work/images.json"
jq '.baseline | {resource_changes:[.httpGroups[] | {address:("aws_lb_target_group.service[\""+(.TargetGroupName|ltrimstr("warmbly-production-"))+"\"]"),
  change:{actions:["delete"],after:null,before:{arn:.TargetGroupArn,id:.TargetGroupArn,name:.TargetGroupName,protocol:.Protocol,port:.Port,vpc_id:.VpcId}}}]}' \
  "$work/fixture.json" >"$work/plan.json"
changes=$(jq -cS '{resource_changes:(.resource_changes|sort_by(.address)),resource_drift:[]}' "$work/plan.json" | sha256sum | cut -d' ' -f1)
jq --arg drain "$drain" --arg on "$on" --arg release "$release" --arg changes "$changes" \
  --arg state "$(sha256sum "$work/state.json" | cut -d' ' -f1)" --arg vars "$(sha256sum "$work/snapshot.json" | cut -d' ' -f1)" \
  --arg images "$(sha256sum "$work/images.json" | cut -d' ' -f1)" '
  .baseline + {schemaVersion:1,repository:"warmbly/warmbly",bucket:"warmbly-terraform-use1-925323749693",
    release:{tag:"v1.2.3",source:$release,runId:101,attempt:1,imagesSha256:$images},
    drain:{tag:"v1.2.2",source:$drain,runId:102,attempt:1},failedOn:{tag:"v1.2.2",source:$on,runId:103,attempt:1},planChangesSha256:$changes} |
  .state += {key:"warmbly/production/runtime.tfstate",sha256:$state} |
  .snapshot += {key:"warmbly/production/vars/runtime-tls-recovery/fixture.tfvars.json",versionId:"vars-version",sha256:$vars}' \
  "$work/fixture.json" >"$work/contract.json"
jq '
  def run($path;$event;$branch;$conclusion): {id:.runId,run_attempt:.attempt,head_sha:.source,repository:{full_name:"warmbly/warmbly"},
    path:$path,event:$event,head_branch:$branch,status:"completed",conclusion:$conclusion};
  {runs:{release:(.release|run(".github/workflows/release.yml";"push";.tag;"success")),
    drain:(.drain|run(".github/workflows/runtime-tls.yml";"workflow_dispatch";"main";"success")+{display_title:("Runtime TLS drain "+.tag)}),
    failedOn:(.failedOn|run(".github/workflows/runtime-tls.yml";"workflow_dispatch";"main";"failure")+{display_title:("Runtime TLS on "+.tag)})},
    failedOnJobs:[{id:1,run_id:103,run_attempt:1,status:"completed",name:"apply",conclusion:"success"},
      {id:2,run_id:103,run_attempt:1,status:"completed",name:"redeploy / fixture",conclusion:"failure"}]}' \
  "$work/contract.json" >"$work/lineage.json"
cat >"$work/repository/.github/scripts/runtime-tls-recovery-live.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "live $1" >>"$FIXTURES/requests.log"
[ "${MODE:-}" != live ] || exit 1
mkdir "$4"
echo '{}' >"$4/live.json"
SH
cat >"$work/bin/aws" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "aws $*" >>"$FIXTURES/requests.log"
service=$1 operation=$2
shift 2
key= version= owner= bucket= region= output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --key) key=$2; shift 2 ;; --version-id) version=$2; shift 2 ;; --expected-bucket-owner) owner=$2; shift 2 ;;
    --region) region=$2; shift 2 ;; --bucket) bucket=$2; shift 2 ;; *) output=$1; shift ;;
  esac
done
[ "$region" = us-east-1 ]
case "$service $operation" in
  'sts get-caller-identity') echo '{"Account":"925323749693"}' ;;
  's3api get-object')
    [ "$owner" = 925323749693 ] && [ "$bucket" = warmbly-terraform-use1-925323749693 ]
    case "$key" in
      */fixture.contract.json) kind=contract ;;
      */fixture.tfvars.json) kind=snapshot ;;
      */runtime.tfstate) kind=state ;;
      *) exit 1 ;;
    esac
    if [ -z "$version" ]; then kind=final-state; version=final-version; fi
    cp "$FIXTURES/$kind.json" "$output"
    jq -n --arg version "$version" '{VersionId:$version,ServerSideEncryption:"AES256"}' ;;
  's3api head-object')
    version=state-version
    if [ -e "$FIXTURES/applied" ] || [ "${MODE:-}" = advanced ]; then version=final-version; fi
    jq -n --arg version "$version" '{VersionId:$version}' ;;
  *) exit 1 ;;
esac
SH
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "gh $*" >>"$FIXTURES/requests.log"
case "$1 $2" in
  'api repos/warmbly/warmbly/environments/aws-infrastructure-approval')
    jq -n --arg mode "${MODE:-}" '{can_admins_bypass:($mode=="bypass"),protection_rules:[{type:"required_reviewers",prevent_self_review:($mode!="self-review"),reviewers:[{type:"User"}]}]}' ;;
  'api repos/warmbly/warmbly/actions/runs/101/attempts/1') jq '.runs.release' "$FIXTURES/lineage.json" ;;
  'api repos/warmbly/warmbly/actions/runs/102/attempts/1') jq '.runs.drain' "$FIXTURES/lineage.json" ;;
  'api repos/warmbly/warmbly/actions/runs/103/attempts/1') jq '.runs.failedOn' "$FIXTURES/lineage.json" ;;
  'api repos/warmbly/warmbly/actions/runs/103/attempts/1/jobs?per_page=100') jq '[{total_count:2,jobs:.failedOnJobs}]' "$FIXTURES/lineage.json" ;;
  'release view') echo '{"tagName":"v1.2.3","isDraft":false,"isPrerelease":false}' ;;
  'release download')
    while [ "$1" != --dir ]; do shift; done
    cp "$FIXTURES/images.json" "$2/images.json"
    if [ "${MODE:-}" = artifact ]; then printf '\n' >>"$2/images.json"; fi ;;
  'attestation verify') [ "${MODE:-}" != provenance ] ;;
  *) exit 1 ;;
esac
SH
cat >"$work/bin/terraform" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "terraform $*" >>"$FIXTURES/requests.log"
case "$2" in
  init) ;;
  plan) [[ "$*" == *'-target=aws_lb_target_group.service'* && "$*" != *'-target=aws_ecs'* ]] ;;
  show)
    case "${MODE:-}" in
      scope) jq '.resource_changes|=.[1:]' "$FIXTURES/plan.json" ;;
      changes) jq '.resource_changes[0].change.before.tags={new:"tag"}' "$FIXTURES/plan.json" ;;
      *) cat "$FIXTURES/plan.json" ;;
    esac ;;
  apply) [ "${MODE:-}" != apply-failure ] && touch "$FIXTURES/applied" ;;
  *) exit 1 ;;
esac
SH
chmod +x "$work/bin/"*
export FIXTURES="$work" PATH="$work/bin:$PATH"
export GITHUB_ACTIONS=true GITHUB_REPOSITORY=warmbly/warmbly GITHUB_REF=refs/heads/main GITHUB_EVENT_NAME=workflow_dispatch
export GITHUB_WORKFLOW_REF=warmbly/warmbly/.github/workflows/runtime-tls-recovery.yml@refs/heads/main
export GITHUB_RUN_ATTEMPT=1 GITHUB_RUN_ID=999 GITHUB_SHA="$release"
export CONTRACT_KEY=warmbly/production/vars/runtime-tls-recovery/fixture.contract.json CONTRACT_VERSION=contract-version
CONTRACT_SHA256=$(sha256sum "$work/contract.json" | cut -d' ' -f1)
export CONTRACT_SHA256
export GITHUB_OUTPUT="$work/output" GITHUB_STEP_SUMMARY="$work/summary"
guard="$work/repository/.github/scripts/runtime-tls-recovery-execute.sh"
cd "$work/repository"
WORK="$work/plan" bash "$guard" plan
approved=$(sed -n 's/^digest=//p' "$GITHUB_OUTPUT")
[ -n "$approved" ]
for mode in bypass self-review artifact provenance live advanced scope changes; do
  : >"$work/requests.log"
  if WORK="$work/$mode" MODE=$mode APPROVED_DIGEST="$approved" bash "$guard" apply 2>/dev/null; then
    echo "Recovery applied incompatible evidence: $mode" >&2; exit 1
  fi
  if rg -q '^terraform .* apply ' "$work/requests.log"; then echo "Recovery reached apply for $mode" >&2; exit 1; fi
done
if WORK="$work/wrong-approval" APPROVED_DIGEST=other bash "$guard" apply 2>/dev/null; then echo 'Recovery accepted another approval digest' >&2; exit 1; fi
if WORK="$work/retry" GITHUB_RUN_ATTEMPT=2 bash "$guard" plan 2>/dev/null; then echo 'Recovery accepted a rerun' >&2; exit 1; fi
if WORK="$work/local" GITHUB_ACTIONS=false bash "$guard" plan 2>/dev/null; then echo 'Recovery accepted local execution' >&2; exit 1; fi
: >"$work/requests.log"
if WORK="$work/apply-failure" MODE=apply-failure APPROVED_DIGEST="$approved" bash "$guard" apply 2>/dev/null; then
  echo 'Recovery continued after an apply failure' >&2; exit 1
fi
[ ! -e "$work/applied" ]
if rg -q '^live collect-final$' "$work/requests.log"; then echo 'Recovery verified a failed apply as complete' >&2; exit 1; fi
: >"$work/requests.log"
WORK="$work/apply" APPROVED_DIGEST="$approved" bash "$guard" apply
[ "$(rg -c '^live collect$' "$work/requests.log")" = 2 ]
[ "$(rg -c '^live collect-final$' "$work/requests.log")" = 1 ]
[ "$(rg -c '^gh attestation verify ' "$work/requests.log")" = 10 ]
WORK="$work/verify" bash "$guard" verify
jq '.resources[0].instances[0].attributes.arn="different"' "$work/final-state.json" >"$work/changed-state.json"
if bash "$guard" check-final-state "$work/changed-state.json" "$work/state.json" 2>/dev/null; then
  echo 'Recovery accepted an unrelated retained-state change' >&2; exit 1
fi
echo 'Protected recovery plan, approval binding, apply and final-state orchestration checks passed'
