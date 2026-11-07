#!/usr/bin/env bash
# Collect fresh read-only service and traffic evidence from reviewed state identities.
set -euo pipefail
scripts=$(cd "$(dirname "$0")" && pwd)

check_live() {
  local evidence=$1 baseline=$2 vars=$3 state=$4 images=$5 phase=${6:-before}
  bash "$scripts/runtime-tls-recovery.sh" check-state "$state" "$baseline"
  [ "$(sha256sum "$vars" | cut -d' ' -f1)" = "$(jq -r '.snapshot.sha256' "$baseline")" ]
  jq -e '.name == "warmbly-production" and .target_tls == "on" and
    (.backend_internal_certificate | type == "string" and startswith("-----BEGIN CERTIFICATE-----"))' "$vars" >/dev/null
  jq -e --arg phase "$phase" --slurpfile baseline "$baseline" --slurpfile vars "$vars" --slurpfile state "$state" --slurpfile images "$images" \
    -f "$scripts/runtime-tls-recovery-live.jq" "$evidence" >/dev/null || {
    echo 'Recovery live service, TLS, traffic or health evidence does not match the reviewed inputs' >&2; return 1;
  }
}

collect_live() {
  local baseline=$1 inputs=$2 work=$3 phase=${4:-before} cluster definition arn name image digest url status
  bash "$scripts/runtime-tls-recovery.sh" check-state "$inputs/state.json" "$baseline"
  umask 077
  mkdir "$work"
  export AWS_PAGER=""
  aws sts get-caller-identity --region us-east-1 >"$work/account.json"
  jq -e '.Account == "925323749693"' "$work/account.json" >/dev/null
  cluster=$(jq -er '[.resources[] | select(.mode=="managed" and .module==null and .type=="aws_ecs_service" and .name=="service") |
    .instances[].attributes.cluster] | unique | if length==1 then .[0] else error("cluster identity") end' "$inputs/state.json")
  aws ecs describe-services --region us-east-1 --cluster "$cluster" --services \
    warmbly-production-backend warmbly-production-consumer warmbly-production-forms warmbly-production-tracking warmbly-production-realtime >"$work/services.json"
  : >"$work/definitions.jsonl"
  : >"$work/tasks.jsonl"
  while IFS= read -r definition; do
    aws ecs describe-task-definition --region us-east-1 --task-definition "$definition" --include TAGS >>"$work/definitions.jsonl"
  done < <(jq -r '.services[].taskDefinition' "$work/services.json")
  while IFS= read -r name; do
    aws ecs list-tasks --region us-east-1 --cluster "$cluster" --service-name "$name" --desired-status RUNNING >"$work/task-list.json"
    while IFS= read -r arn; do
      aws ecs describe-tasks --region us-east-1 --cluster "$cluster" --tasks "$arn" >"$work/task.json"
      jq -e '.failures == [] and (.tasks | length == 1)' "$work/task.json" >/dev/null
      jq -c '.tasks[]' "$work/task.json" >>"$work/tasks.jsonl"
    done < <(jq -r '.taskArns[]' "$work/task-list.json")
  done < <(jq -r '.services[].serviceName' "$work/services.json")
  : >"$work/manifests.jsonl"
  while IFS= read -r image; do
    [[ "$image" =~ ^ghcr.io/warmbly/warmbly/(backend|consumer|forms|tracking|realtime)@sha256:[a-f0-9]{64}$ ]]
    docker buildx imagetools inspect "$image" --raw >"$work/manifest.json"
    digest="sha256:$(sha256sum "$work/manifest.json" | cut -d' ' -f1)"
    [ "$digest" = "${image##*@}" ] || { echo 'Registry manifest bytes differ from the pinned image' >&2; return 1; }
    jq -n --arg image "$image" --arg digest "$digest" --slurpfile document "$work/manifest.json" \
      '{image:$image,digest:$digest,document:$document[0]}' >>"$work/manifests.jsonl"
  done < <(jq -r '.taskDefinition.containerDefinitions[].image' "$work/definitions.jsonl" | sort -u)
  aws elbv2 describe-target-groups --region us-east-1 >"$work/all-groups.json"
  : >"$work/groups.jsonl"
  while IFS= read -r arn; do
    aws elbv2 describe-target-health --region us-east-1 --target-group-arn "$arn" >"$work/health.json"
    jq -c --arg arn "$arn" --slurpfile health "$work/health.json" \
      '.TargetGroups[] | select(.TargetGroupArn == $arn) | . + {targets:$health[0].TargetHealthDescriptions}' \
      "$work/all-groups.json" >>"$work/groups.jsonl"
  done < <(jq -r '.TargetGroups[] | select(.TargetGroupName | test("^warmbly-production-(backend|forms|tracking|realtime)(-tls)?$")) | .TargetGroupArn' "$work/all-groups.json")
  arn=$(jq -er '[.resources[] | select(.mode=="managed" and .module==null and .type=="aws_lb" and .name=="main") |
    .instances[].attributes.arn] | if length==1 then .[0] else error("load balancer identity") end' "$inputs/state.json")
  aws elbv2 describe-listeners --region us-east-1 --load-balancer-arn "$arn" >"$work/listeners.json"
  : >"$work/rules.jsonl"
  while IFS= read -r arn; do
    aws elbv2 describe-rules --region us-east-1 --listener-arn "$arn" | jq -c '.Rules[]' >>"$work/rules.jsonl"
  done < <(jq -r '.Listeners[].ListenerArn' "$work/listeners.json")
  : >"$work/security-groups.jsonl"
  while IFS= read -r arn; do
    aws ec2 describe-security-groups --region us-east-1 --group-ids "$arn" | jq -c '.SecurityGroups[]' >>"$work/security-groups.jsonl"
  done < <(jq -r '.services[].networkConfiguration.awsvpcConfiguration.securityGroups[]' "$work/services.json" | sort -u)
  aws cloudwatch describe-alarms --region us-east-1 --alarm-name-prefix warmbly-production- >"$work/alarms.json"
  : >"$work/public.jsonl"
  for url in https://api.warmbly.com/health https://api.warmbly.com/v1/auth/config https://forms.warmbly.com/health \
    https://realtime.warmbly.com/health https://track.monitorably.com/health; do
    status=$(curl --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 10 --max-time 30 --output /dev/null --write-out '%{http_code}' "$url")
    jq -n --arg url "$url" --argjson status "$status" '{url:$url,status:$status}' >>"$work/public.jsonl"
  done
  jq -n --slurpfile account "$work/account.json" --slurpfile services "$work/services.json" \
    --slurpfile definitions "$work/definitions.jsonl" --slurpfile tasks "$work/tasks.jsonl" \
    --slurpfile manifests "$work/manifests.jsonl" --slurpfile groups "$work/groups.jsonl" \
    --slurpfile listeners "$work/listeners.json" --slurpfile rules "$work/rules.jsonl" \
    --slurpfile securityGroups "$work/security-groups.jsonl" --slurpfile alarms "$work/alarms.json" --slurpfile public "$work/public.jsonl" \
    '{account:$account[0],services:$services[0],definitions:$definitions,tasks:$tasks,manifests:$manifests,
      httpsGroups:[$groups[]|select(.Protocol=="HTTPS")],httpGroups:[$groups[]|select(.Protocol=="HTTP")],
      listeners:$listeners[0].Listeners,rules:$rules,securityGroups:$securityGroups,alarms:$alarms[0],public:$public}' >"$work/candidate.json"
  check_live "$work/candidate.json" "$baseline" "$inputs/snapshot.json" "$inputs/state.json" "$inputs/images.json" "$phase"
  mv "$work/candidate.json" "$work/live.json"
}

case "${1:-}" in
  check) check_live "${2:?}" "${3:?}" "${4:?}" "${5:?}" "${6:?}" ;;
  check-final) check_live "${2:?}" "${3:?}" "${4:?}" "${5:?}" "${6:?}" after ;;
  collect) collect_live "${2:?}" "${3:?}" "${4:?}" ;;
  collect-final) collect_live "${2:?}" "${3:?}" "${4:?}" after ;;
  *) echo 'Only read-only recovery live checks and collection are supported' >&2; exit 1 ;;
esac
