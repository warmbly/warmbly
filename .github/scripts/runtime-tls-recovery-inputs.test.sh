#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
guard=.github/scripts/runtime-tls-recovery-inputs.sh
jq -n '
  ["backend","forms","tracking","realtime"] | map({
    TargetGroupName:("warmbly-production-" + .),
    TargetGroupArn:("arn:aws:elasticloadbalancing:us-east-1:925323749693:targetgroup/warmbly-production-" + . + "/0123456789abcdef"),
    Protocol:"HTTP",Port:8080,VpcId:"vpc-0123456789abcdef0",LoadBalancerArns:[],targets:[]}) as $groups |
  {account:"925323749693",region:"us-east-1",bucket:"warmbly-terraform-use1-925323749693",httpGroups:$groups,
   state:{key:"warmbly/production/runtime.tfstate",versionId:"reviewed-state",lineage:"01234567-89ab-cdef-0123-456789abcdef",serial:1,httpGroups:$groups},
   snapshot:{key:"warmbly/production/vars/runtime-tls-recovery/reviewed-cleanup.tfvars.json",versionId:"reviewed-vars",releaseTag:"v1.0.1"}}
' >"$work/baseline.json"
jq '{version:4,lineage:.state.lineage,serial:.state.serial,resources:[{mode:"managed",type:"aws_lb_target_group",name:"service",
  instances:[.httpGroups[] | {index_key:(.TargetGroupName | ltrimstr("warmbly-production-")),attributes:{
    id:.TargetGroupArn,arn:.TargetGroupArn,name:.TargetGroupName,protocol:.Protocol,port:.Port,vpc_id:.VpcId}}]}]}' \
  "$work/baseline.json" >"$work/state.json"
printf '%s\n' '{"name":"warmbly-production","target_tls":"on"}' >"$work/snapshot.json"
state_digest=$(sha256sum "$work/state.json" | cut -d' ' -f1)
vars_digest=$(sha256sum "$work/snapshot.json" | cut -d' ' -f1)
jq --arg state "$state_digest" --arg vars "$vars_digest" '.state.sha256=$state | .snapshot.sha256=$vars' \
  "$work/baseline.json" >"$work/reviewed.json"
bash "$guard" check-contract "$work/reviewed.json"
for change in '.bucket="other-bucket"' '.state.key="other/state"' \
  '.snapshot.key="warmbly/production/vars/runtime-tls.tfvars.json"' \
  '.snapshot.key="warmbly/production/vars/runtime-tls-recovery/../default.tfvars.json"' \
  '.snapshot.versionId="null"' '.snapshot.sha256=""' '.snapshot.releaseTag="main"'; do
  jq "$change" "$work/reviewed.json" >"$work/changed.json"
  if bash "$guard" check-contract "$work/changed.json" 2>/dev/null; then
    echo "Recovery accepted an unbound input contract: $change" >&2; exit 1
  fi
done
mkdir "$work/bin"
cat >"$work/bin/aws" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FIXTURES/requests.log"
service=$1 operation=$2
shift 2
bucket= key= version= owner= region= output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bucket) bucket=$2; shift 2 ;;
    --key) key=$2; shift 2 ;;
    --version-id) version=$2; shift 2 ;;
    --expected-bucket-owner) owner=$2; shift 2 ;;
    --region) region=$2; shift 2 ;;
    *) output=$1; shift ;;
  esac
done
[ "$region" = us-east-1 ]
case "$service $operation" in
  'sts get-caller-identity')
    if [ "${MODE:-}" = account ]; then echo '{"Account":"123456789012"}'; else echo '{"Account":"925323749693"}'; fi ;;
  's3api get-object')
    [ "$bucket" = warmbly-terraform-use1-925323749693 ] && [ "$owner" = 925323749693 ]
    case "$key $version" in
      'warmbly/production/runtime.tfstate reviewed-state') kind=state ;;
      'warmbly/production/vars/runtime-tls-recovery/reviewed-cleanup.tfvars.json reviewed-vars') kind=snapshot ;;
      *) exit 1 ;;
    esac
    [ "${MODE:-}" != api-failure ] || exit 1
    cp "$FIXTURES/$kind.json" "$output"
    if [ "${MODE:-}" = hash ]; then printf '\n' >>"$output"; fi
    if [ "${MODE:-}" = version ]; then version=other-version; fi
    encryption=AES256
    if [ "${MODE:-}" = kms ]; then encryption=aws:kms; fi
    if [ "${MODE:-}" = encryption ]; then encryption=none; fi
    jq -n --arg version "$version" --arg encryption "$encryption" --arg mode "${MODE:-}" \
      '{VersionId:$version,ServerSideEncryption:$encryption,DeleteMarker:($mode=="deleted")}' ;;
  's3api head-object')
    [ "$bucket" = warmbly-terraform-use1-925323749693 ] && [ "$owner" = 925323749693 ]
    [ "$key" = warmbly/production/runtime.tfstate ]
    version=reviewed-state
    if [ "${MODE:-}" = advanced ]; then version=advanced-state; fi
    jq -n --arg version "$version" '{VersionId:$version}' ;;
  *) echo 'Unexpected AWS operation' >&2; exit 1 ;;
esac
SH
chmod +x "$work/bin/aws"
PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/reviewed.json" "$work/collected"
jq -e --arg state "$state_digest" --arg vars "$vars_digest" '.state.sha256==$state and .snapshot.sha256==$vars' "$work/collected/inputs.json" >/dev/null
PATH="$work/bin:$PATH" FIXTURES="$work" MODE=kms bash "$guard" collect "$work/reviewed.json" "$work/kms"
for mode in account api-failure hash version encryption deleted advanced; do
  if PATH="$work/bin:$PATH" FIXTURES="$work" MODE=$mode bash "$guard" collect "$work/reviewed.json" "$work/$mode" 2>/dev/null; then
    echo "Recovery accepted incompatible collected inputs: $mode" >&2; exit 1
  fi
  [ ! -e "$work/$mode/inputs.json" ]
done
if PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/reviewed.json" "$work/collected" 2>/dev/null; then
  echo 'Recovery overwrote an existing input collection' >&2; exit 1
fi
printf '%s\n' '{"name":"other-deployment","target_tls":"on"}' >"$work/snapshot.json"
digest=$(sha256sum "$work/snapshot.json" | cut -d' ' -f1)
jq --arg digest "$digest" '.snapshot.sha256=$digest' "$work/reviewed.json" >"$work/changed.json"
if PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/changed.json" "$work/wrong-deployment" 2>/dev/null; then
  echo 'Recovery accepted vars for a different deployment' >&2; exit 1
fi
for stage in drain route; do
  printf '%s\n' '{"name":"warmbly-production","target_tls":"on"}' >"$work/snapshot.json"
  jq --arg stage "$stage" '.target_tls=$stage' "$work/snapshot.json" >"$work/changed-vars.json"
  cp "$work/changed-vars.json" "$work/snapshot.json"
  digest=$(sha256sum "$work/snapshot.json" | cut -d' ' -f1)
  jq --arg digest "$digest" '.snapshot.sha256=$digest' "$work/reviewed.json" >"$work/changed.json"
  if PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/changed.json" "$work/$stage" 2>/dev/null; then
    echo 'Recovery accepted vars for a different transition stage' >&2; exit 1
  fi
done
if bash "$guard" apply 2>/dev/null; then
  echo 'Recovery input collector accepted an execution action' >&2; exit 1
fi
echo 'Read-only recovery S3 input collection and checks passed'
