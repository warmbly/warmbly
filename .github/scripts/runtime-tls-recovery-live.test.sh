#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
guard=.github/scripts/runtime-tls-recovery-live.sh
printf '{"schemaVersion":2}\n' >"$work/manifest.json"
digest="sha256:$(sha256sum "$work/manifest.json" | cut -d' ' -f1)"
jq -n --arg digest "$digest" -f .github/scripts/runtime-tls-recovery-live.fixture.jq >"$work/fixture.json"
for kind in baseline state vars images live; do jq ".$kind" "$work/fixture.json" >"$work/$kind.json"; done
state_hash=$(sha256sum "$work/state.json" | cut -d' ' -f1)
vars_hash=$(sha256sum "$work/vars.json" | cut -d' ' -f1)
jq --arg state "$state_hash" --arg vars "$vars_hash" '.state.sha256=$state | .snapshot.sha256=$vars' "$work/baseline.json" >"$work/reviewed.json"
check() { bash "$guard" check "$1" "$work/reviewed.json" "$work/vars.json" "$work/state.json" "$work/images.json"; }
check "$work/live.json"
for change in \
  '.account.Account="123456789012"' '.services.failures=[{}]' '.services.services|=.[1:]' \
  '.services.services[0].clusterArn="other-cluster"' '.services.services[0].serviceArn="other-service"' \
  '.services.services[0].desiredCount=0' '.services.services[0].runningCount=0' '.services.services[0].pendingCount=1' \
  '.services.services[0].deployments[0].rolloutState="IN_PROGRESS"' '.services.services[0].deployments+=.services.services[0].deployments' \
  '.services.services[0].loadBalancers[0].targetGroupArn="other-group"' \
  '.services.services[0].networkConfiguration.awsvpcConfiguration.securityGroups+=["sg-extra"]' \
  '.definitions[0].tags[0].value="v1.2.2"' '.definitions[0].taskDefinition.containerDefinitions[0].image="other-image"' \
  '.definitions[0].taskDefinition.containerDefinitions[0].secrets=[{name:"SERVE_TLS",valueFrom:"other"}]' \
  '.definitions[0].taskDefinition.containerDefinitions[0].environmentFiles=[{value:"other"}]' \
  '.definitions[0].taskDefinition.containerDefinitions[0].environment[2].value="false"' \
  '.definitions[0].taskDefinition.containerDefinitions[0].environment[0].value="self-signed"' \
  '.definitions[0].taskDefinition.containerDefinitions[0].environment[3].value="other-cert"' \
  '.definitions[2].taskDefinition.containerDefinitions[0].environment[4].value="other-ca"' \
  '.definitions[2].taskDefinition.containerDefinitions[0].environment[3].value="http://backend.warmbly.internal:8080"' \
  '.tasks|=.[1:]' '.tasks+=.tasks' '.tasks[0].taskDefinitionArn="older-definition"' \
  '.tasks[0].healthStatus="UNHEALTHY"' '.tasks[0].containers[0].imageDigest="sha256:other"' \
  '.tasks[1].healthStatus="UNHEALTHY"' '.tasks[1].containers[0].lastStatus="STOPPED"' \
  '.tasks[0].overrides.containerOverrides=[{environment:[{name:"SERVE_TLS",value:"off"}]}]' \
  '.httpsGroups|=.[1:]' '.httpsGroups[0].Protocol="HTTP"' '.httpsGroups[0].targets=[]' \
  '.httpsGroups[0].targets[0].TargetHealth.State="unhealthy"' '.httpsGroups[0].LoadBalancerArns=[]' \
  '.httpsGroups[0].targets[0].Target.Id="10.0.0.2"' \
  '.httpGroups[0].targets=[{}]' '.httpGroups[0].LoadBalancerArns=["other-lb"]' '.httpGroups[0].TargetGroupArn="other-group"' \
  '.listeners[0].Protocol="HTTP"' '.listeners[0].Port=80' '.rules|=.[1:]' \
  '.rules[0].Actions[0].TargetGroupArn=.httpGroups[0].TargetGroupArn' \
  '.rules[0].Actions[0].ForwardConfig.TargetGroups=[{TargetGroupArn:.httpGroups[0].TargetGroupArn,Weight:0}]' \
  '.rules[0].Conditions[0].HostHeaderConfig.Values=["other.example.com"]' \
  '.rules[0].Transforms=[{}]' '.securityGroups=[]' \
  '.securityGroups[0].IpPermissions+=[{IpProtocol:"tcp",FromPort:8080,ToPort:8080}]' \
  '.securityGroups[0].IpPermissions+=[{IpProtocol:"-1"}]' \
  '.alarms.MetricAlarms|=.[1:]' '.alarms.MetricAlarms[0].StateValue="ALARM"' \
  '.alarms.MetricAlarms[0].StateValue="INSUFFICIENT_DATA"' '.public[0].status=503' '.public|=.[1:]'; do
  jq "$change" "$work/live.json" >"$work/changed.json"
  if check "$work/changed.json" 2>/dev/null; then
    echo "Recovery accepted incompatible live evidence: $change" >&2; exit 1
  fi
done
jq '.alarms.MetricAlarms[0].Dimensions[0].Value="targetgroup/old-http/0000000000000000"' "$work/live.json" >"$work/changed.json"
if check "$work/changed.json" 2>/dev/null; then echo 'Recovery accepted an alarm on a different target group' >&2; exit 1; fi
jq '.alarms.MetricAlarms[] |= (.Metrics=[{MetricStat:{Metric:{Dimensions:.Dimensions}}}] | del(.Dimensions))' "$work/live.json" >"$work/metric-math.json"
check "$work/metric-math.json"
jq --arg digest "$digest" '.tasks[].containers[0].imageDigest="sha256:platform" |
  .manifests[].document={manifests:[{digest:"sha256:platform",platform:{os:"linux",architecture:"amd64"}}]}' \
  "$work/live.json" >"$work/multiarch.json"
check "$work/multiarch.json"
jq --arg digest "$digest" '.tasks[].containers[0].imageDigest=$digest' "$work/multiarch.json" >"$work/index-reported.json"
check "$work/index-reported.json"
jq '.manifests[].document.manifests[0].platform.architecture="arm64"' "$work/multiarch.json" >"$work/changed.json"
if check "$work/changed.json" 2>/dev/null; then echo 'Recovery accepted another platform manifest' >&2; exit 1; fi
jq '.httpGroups=[]' "$work/live.json" >"$work/final.json"
bash "$guard" check-final "$work/final.json" "$work/reviewed.json" "$work/vars.json" "$work/state.json" "$work/images.json"
if bash "$guard" check-final "$work/live.json" "$work/reviewed.json" "$work/vars.json" "$work/state.json" "$work/images.json" 2>/dev/null; then
  echo 'Recovery final check accepted retained HTTP groups' >&2; exit 1
fi
mkdir "$work/bin" "$work/inputs"
cp "$work/state.json" "$work/images.json" "$work/inputs/"
cp "$work/vars.json" "$work/inputs/snapshot.json"
cat >"$work/bin/aws" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FIXTURES/requests.log"
service=$1 operation=$2
shift 2
region= definition= name= arn=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --region) region=$2; shift 2 ;;
    --task-definition) definition=$2; shift 2 ;;
    --service-name) name=$2; shift 2 ;;
    --tasks|--target-group-arn) arn=$2; shift 2 ;;
    *) shift ;;
  esac
done
[ "$region" = us-east-1 ]
[ "${MODE:-}" != api-failure ] || [ "$service $operation" != 'ecs describe-tasks' ] || exit 1
case "$service $operation" in
  'sts get-caller-identity')
    if [ "${MODE:-}" = account ]; then echo '{"Account":"123456789012"}'; else jq '.account' "$FIXTURES/live.json"; fi ;;
  'ecs describe-services') jq '.services' "$FIXTURES/live.json" ;;
  'ecs describe-task-definition') jq --arg definition "$definition" '.definitions[] | select(.taskDefinition.taskDefinitionArn==$definition)' "$FIXTURES/live.json" ;;
  'ecs list-tasks') jq --arg name "$name" '{taskArns:[.tasks[]|select(.group==("service:"+$name))|.taskArn]}' "$FIXTURES/live.json" ;;
  'ecs describe-tasks') jq --arg arn "$arn" '{failures:[],tasks:[.tasks[]|select(.taskArn==$arn)]}' "$FIXTURES/live.json" ;;
  'elbv2 describe-target-groups')
    if [ "${MODE:-}" = final ]; then jq '{TargetGroups:.httpsGroups}' "$FIXTURES/live.json"
    else jq '{TargetGroups:(.httpsGroups+.httpGroups)}' "$FIXTURES/live.json"; fi ;;
  'elbv2 describe-target-health')
    jq --arg arn "$arn" '{TargetHealthDescriptions:[(.httpsGroups+.httpGroups)[]|select(.TargetGroupArn==$arn)|.targets[]]}' "$FIXTURES/live.json" ;;
  'elbv2 describe-listeners') jq '{Listeners:.listeners}' "$FIXTURES/live.json" ;;
  'elbv2 describe-rules') jq '{Rules:.rules}' "$FIXTURES/live.json" ;;
  'ec2 describe-security-groups') jq '{SecurityGroups:.securityGroups}' "$FIXTURES/live.json" ;;
  'cloudwatch describe-alarms') jq '.alarms' "$FIXTURES/live.json" ;;
  *) echo 'Unexpected AWS operation in read-only collector' >&2; exit 1 ;;
esac
SH
cat >"$work/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'buildx imagetools inspect ghcr.io/warmbly/warmbly/'*' --raw' ]]
cat "$FIXTURES/manifest.json"
if [ "${MODE:-}" = manifest ]; then printf '\n'; fi
SH
cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *"--proto =https --tlsv1.2"* && "$*" != *insecure* ]]
if [ "${MODE:-}" = public ]; then printf '503'; else printf '200'; fi
SH
chmod +x "$work/bin/aws" "$work/bin/docker" "$work/bin/curl"
PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/reviewed.json" "$work/inputs" "$work/collected"
check "$work/collected/live.json"
PATH="$work/bin:$PATH" FIXTURES="$work" MODE=final bash "$guard" collect-final "$work/reviewed.json" "$work/inputs" "$work/final-collected"
for mode in account api-failure manifest public; do
  if PATH="$work/bin:$PATH" FIXTURES="$work" MODE=$mode bash "$guard" collect "$work/reviewed.json" "$work/inputs" "$work/$mode" 2>/dev/null; then
    echo "Recovery collector accepted failed read-only evidence: $mode" >&2; exit 1
  fi
  [ ! -e "$work/$mode/live.json" ]
done
if PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/reviewed.json" "$work/inputs" "$work/collected" 2>/dev/null; then
  echo 'Recovery collector overwrote an earlier collection' >&2; exit 1
fi
if bash "$guard" apply 2>/dev/null; then echo 'Live collector accepted execution' >&2; exit 1; fi
echo 'Recovery live service, TLS, traffic, health and platform checks passed'
