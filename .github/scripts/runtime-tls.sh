#!/usr/bin/env bash
# Finalize runtime TLS through the protected environment using a versioned private snapshot.
set -euo pipefail

check_plan() {
  local stage=${2:-route}
  [[ "$stage" = route || "$stage" = drain || "$stage" = on ]]
  jq -e --arg stage "$stage" '
    def service: "\\[\\\"(backend|forms|tracking|realtime)\\\"\\]";
    def http_groups:
      ["backend", "forms", "tracking", "realtime"] |
      map("aws_lb_target_group.service[\"" + . + "\"]") | sort;
    def normalize:
      map(.mountPoints //= [] | .systemControls //= [] | .volumesFrom //= [] |
        .portMappings = ((.portMappings // []) | map(.hostPort //= .containerPort)));
    def same_except($keys):
      (.change.before | delpaths($keys | map([.]))) == (.change.after | delpaths($keys | map([.])));
    def normalize_empty_description:
      .change.actions == ["update"] and same_except(["description"]) and
      .change.before.description == "" and .change.after.description == null;
    def normalize_registration:
      if .elb_name == "" then .elb_name = null else . end;
    def task_settings:
      del(.arn, .arn_without_revision, .id, .revision, .container_definitions) |
      .enable_fault_injection //= false | .ipc_mode //= "" | .pid_mode //= "";
    def same_task_settings:
      (.change.before | task_settings) == (.change.after | task_settings);
    def same_container_except_loopback:
      (.change.before.container_definitions | fromjson | normalize) as $before |
      (.change.after.container_definitions | fromjson | normalize) as $after |
      ($after | map(.environment |= map(select(.name != "SERVE_HTTP_LOOPBACK_ONLY")))) ==
        ($before | map(.environment |= map(select(.name != "SERVE_HTTP_LOOPBACK_ONLY")))) and
      all($after[]; if $stage == "on" then
        any(.environment[]; .name == "SERVE_HTTP_LOOPBACK_ONLY" and .value == "true")
        else all(.environment[]; .name != "SERVE_HTTP_LOOPBACK_ONLY") end);
    [.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])] as $changes |
    ($stage != "on" or
      ([$changes[] | select(.change.actions == ["delete"] and
        (.address | test("^aws_lb_target_group\\.service" + service + "$"))) | .address] | sort) == http_groups) and
    all($changes[];
      if (.address | test("^aws_ecs_task_definition\\.service" + service + "$")) then
        .change.actions == ["delete", "create"] and same_container_except_loopback and same_task_settings
      elif (.address | test("^aws_ecs_service\\.service" + service + "$")) then
        .change.actions == ["update"] and same_except(["load_balancer"]) and
        ($stage == "drain") and
        (.change.before.load_balancer | length == 2) and
        (.change.after.load_balancer | length == 1) and
        ((.change.after.load_balancer[0] | normalize_registration) as $tls |
          ($tls.target_group_arn | contains("-tls/")) and
          (.change.before.load_balancer | map(normalize_registration) | index($tls) != null))
      elif (.address | test("^aws_cloudwatch_metric_alarm\\.target_5xx" + service + "$")) then
        .change.actions == ["update"] and same_except(["metric_query"])
      elif (.address | test("^aws_cloudwatch_metric_alarm\\.unhealthy_hosts" + service + "$")) then
        .change.actions == ["update"] and same_except(["dimensions"])
      elif (.address | test("^aws_lb_listener_rule\\.(service" + service + "|tracking_alias\\[\\\"[0-9]+\\\"\\])$")) then
        .change.actions == ["update"] and same_except(["action"])
      elif (.address | test("^aws_lb_target_group\\.service" + service + "$")) then
        $stage == "on" and .change.actions == ["delete"]
      elif (.address | test("^aws_vpc_security_group_ingress_rule\\.service" + service + "$")) or
        .address == "aws_vpc_security_group_ingress_rule.internal_backend[0]" then
        ($stage == "on" and .change.actions == ["delete"]) or normalize_empty_description
      else false end)
  ' "$1" >/dev/null || { echo 'Plan contains changes outside the runtime TLS switch' >&2; return 1; }
}

check_live() {
  jq -e --arg stage "$2" --arg release "$3" --arg port "$4" --arg prefix "$5" '
    . as $live |
    (.services.failures | length == 0) and
    ([.services.services[].serviceName] | sort) ==
      (["backend", "forms", "tracking", "realtime"] | map($prefix + "-" + .) | sort) and
    all(.services.services[];
      . as $service |
      .desiredCount > 0 and .runningCount == .desiredCount and .pendingCount == 0 and
      (.deployments | length == 1) and .deployments[0].rolloutState == "COMPLETED" and
      (.loadBalancers | length == (if $stage == "on" then 1 else 2 end)) and
      any(.loadBalancers[]; .containerPort == ($port | tonumber) and
        (.targetGroupArn | contains("-tls/"))) and
      any($live.definitions[];
        .taskDefinition.taskDefinitionArn == $service.taskDefinition and
        any(.tags[]; .key == "warmbly-release" and .value == $release) and
        (.taskDefinition.containerDefinitions | length > 0) and
        all(.taskDefinition.containerDefinitions[];
          (.environment | map({key:.name,value:.value}) | from_entries) as $env |
          ($env.SERVE_TLS == "supplied" or $env.SERVE_TLS == "self-signed") and
          $env.SERVE_TLS_PORT == $port and $env.SERVE_HTTP_LOOPBACK_ONLY != "true")))
  ' "$1" >/dev/null || { echo 'Live services must be stable on the expected release with compatible TLS listeners and registrations' >&2; return 1; }
}

check_cleanup() {
  jq -e '
    (.tlsPort // 8443) as $port |
    (.services | length == 4) and (.httpGroups | length == 4) and
    all(.services[]; (.loadBalancers | length > 0) and
      all(.loadBalancers[]; .containerPort == $port and (.targetGroupArn | contains("-tls/")))) and
    all(.httpGroups[]; (.LoadBalancerArns | length == 0) and (.targets | length == 0))
  ' "$1" >/dev/null || { echo 'HTTP listeners and registrations must be drained before cleanup' >&2; return 1; }
}

main() {
  local action=${1:?}
  if [ "$action" = check-plan ]; then check_plan "${2:?}" "${3:-route}"; return; fi
  if [ "$action" = check-live ]; then check_live "${2:?}" "${3:?}" "${4:?}" "${5:?}" "${6:?}"; return; fi
  if [ "$action" = check-cleanup ]; then check_cleanup "${2:?}"; return; fi
  case "$action" in plan|apply) ;; *) echo 'Unknown action' >&2; return 1 ;; esac
  : "${STATE_BUCKET:?}" "${WORK:?}" "${VARS_VERSION:?}" "${RELEASE:?}"
  : "${STAGE:?}"
  [[ "$STAGE" = route || "$STAGE" = drain || "$STAGE" = on ]]
  [[ "$VARS_VERSION" != null && "$VARS_VERSION" != *$'\n'* && "$RELEASE" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
  mkdir -p "$WORK"
  chmod 700 "$WORK"
  umask 077
  local dir=deploy/terraform/aws/runtime vars="$WORK/runtime.tfvars.json" log="$WORK/runtime.log"
  aws s3api get-object --bucket "$STATE_BUCKET" --key warmbly/production/vars/runtime-tls.tfvars.json \
    --version-id "$VARS_VERSION" "$vars" >/dev/null
  local previous=both
  case "$STAGE" in drain) previous=route ;; on) previous=drain ;; esac
  jq -e --arg previous "$previous" '.target_tls == $previous' "$vars" >/dev/null
  jq -r '.. | strings | select(length >= 6)' "$vars" | while IFS= read -r line; do
    printf '::add-mask::%s\n' "$line" >&2
  done
  local account cluster prefix definition port
  account=$(aws sts get-caller-identity --query Account --output text)
  [ "$account" = 925323749693 ]
  cluster=$(jq -r '.name // "warmbly-production"' "$vars")
  prefix=$cluster
  port=$(jq -r '.target_tls_port // 8443' "$vars")
  aws ecs describe-services --cluster "$cluster" --services \
    "$prefix-backend" "$prefix-forms" "$prefix-tracking" "$prefix-realtime" >"$WORK/services.json"
  : >"$WORK/definitions.jsonl"
  while IFS= read -r definition; do
    aws ecs describe-task-definition --task-definition "$definition" --include TAGS >>"$WORK/definitions.jsonl"
  done < <(jq -r '.services[].taskDefinition' "$WORK/services.json")
  jq -n --slurpfile services "$WORK/services.json" --slurpfile definitions "$WORK/definitions.jsonl" \
    '{services:$services[0],definitions:$definitions}' >"$WORK/live.json"
  check_live "$WORK/live.json" "$STAGE" "$RELEASE" "$port" "$prefix"
  local service arn count
  for service in backend forms tracking realtime; do
    arn=$(aws elbv2 describe-target-groups --names "$prefix-$service-tls" --query 'TargetGroups[0].TargetGroupArn' --output text)
    count=$(aws elbv2 describe-target-health --target-group-arn "$arn" --query 'TargetHealthDescriptions' --output json |
      jq 'if length > 0 and all(.[]; .TargetHealth.State == "healthy") then length else 0 end')
    [ "$count" -gt 0 ] || { echo "HTTPS targets are not healthy for $service" >&2; return 1; }
    if [ "$STAGE" = route ]; then
      arn=$(aws elbv2 describe-target-groups --names "$prefix-$service" --query 'TargetGroups[0].TargetGroupArn' --output text)
      count=$(aws elbv2 describe-target-health --target-group-arn "$arn" --query 'TargetHealthDescriptions' --output json |
        jq 'if length > 0 and all(.[]; .TargetHealth.State == "healthy") then length else 0 end')
      [ "$count" -gt 0 ] || { echo "HTTP fallback is not healthy for $service" >&2; return 1; }
    fi
  done
  if [ "$STAGE" != route ]; then
    : "${PREVIOUS_RUN:?}" "${GITHUB_REPOSITORY:?}"
    [[ "$PREVIOUS_RUN" =~ ^[0-9]+$ ]]
    gh api "repos/$GITHUB_REPOSITORY/actions/runs/$PREVIOUS_RUN" |
      jq -e --arg title "Runtime TLS $previous $RELEASE" '
        .conclusion == "success" and .head_branch == "main" and
        .path == ".github/workflows/runtime-tls.yml" and .display_title == $title
      ' >/dev/null || { echo 'The preceding stage workflow and its release bakes must succeed first' >&2; return 1; }
  fi
  if [ "$STAGE" = on ]; then
    aws elbv2 describe-target-groups --names "$prefix-backend" "$prefix-forms" \
      "$prefix-tracking" "$prefix-realtime" >"$WORK/http-groups.json"
    : >"$WORK/http-health.jsonl"
    while IFS= read -r arn; do
      aws elbv2 describe-target-health --target-group-arn "$arn" |
        jq --arg arn "$arn" '{arn:$arn,targets:.TargetHealthDescriptions}' >>"$WORK/http-health.jsonl"
    done < <(jq -r '.TargetGroups[].TargetGroupArn' "$WORK/http-groups.json")
    jq -n --slurpfile services "$WORK/services.json" --slurpfile groups "$WORK/http-groups.json" \
      --argjson port "$(jq '.target_tls_port // 8443' "$vars")" \
      --slurpfile health "$WORK/http-health.jsonl" '{tlsPort:$port,services:$services[0].services,httpGroups:[
        $groups[0].TargetGroups[] | . as $group | . + {targets:[$health[] | select(.arn == $group.TargetGroupArn) | .targets[]]}]}' >"$WORK/cleanup.json"
    check_cleanup "$WORK/cleanup.json"
  fi
  terraform -chdir="$dir" init -input=false -no-color -backend-config="bucket=$STATE_BUCKET" >"$log" 2>&1
  terraform -chdir="$dir" plan -input=false -no-color -lock-timeout=5m -var-file="$vars" \
    -var="state_bucket=$STATE_BUCKET" -var="target_tls=$STAGE" \
    -target=aws_ecs_service.service -target=aws_lb_listener_rule.tracking_alias \
    -target=aws_lb_target_group.service \
    -target=aws_vpc_security_group_ingress_rule.service -target=aws_vpc_security_group_ingress_rule.internal_backend \
    -out="$WORK/runtime.tfplan" >>"$log" 2>&1 || { echo 'Runtime TLS plan failed; inspect the private runner log' >&2; return 1; }
  terraform -chdir="$dir" show -json "$WORK/runtime.tfplan" >"$WORK/runtime.json"
  check_plan "$WORK/runtime.json" "$STAGE"
  local digest
  digest=$(jq -cS '[.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])] | sort_by(.address)' "$WORK/runtime.json" | sha256sum | cut -d' ' -f1)
  if [ "$action" = plan ]; then
    printf 'digest=%s\n' "$digest" >>"${GITHUB_OUTPUT:?}"
    jq -r '.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"]) | "\(.change.actions | join("/")) \(.address)"' "$WORK/runtime.json" >>"${GITHUB_STEP_SUMMARY:?}"
  else
    [ "$digest" = "${APPROVED_DIGEST:?}" ] || { echo 'Runtime TLS plan changed; start a new approval run' >&2; return 1; }
    if ! terraform -chdir="$dir" apply -input=false -no-color -lock-timeout=5m "$WORK/runtime.tfplan" >>"$log" 2>&1; then
      local diagnostic="warmbly/production/diagnostics/runtime-tls/${GITHUB_RUN_ID:?}/${GITHUB_RUN_ATTEMPT:?}/apply.log"
      aws s3 cp "$log" "s3://$STATE_BUCKET/$diagnostic" --only-show-errors || echo 'Private diagnostic upload failed' >&2
      echo "Runtime TLS apply failed; inspect state and private diagnostic $diagnostic before retrying" >&2
      return 1
    fi
    echo 'Runtime TLS apply completed'
  fi
}
main "$@"
