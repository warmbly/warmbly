#!/usr/bin/env bash
# Finalize runtime TLS through the protected environment using a versioned private snapshot.
set -euo pipefail

check_plan() {
  jq -e '
    def service: "\\[\\\"(backend|forms|tracking|realtime)\\\"\\]";
    def normalize:
      map(.mountPoints //= [] | .systemControls //= [] | .volumesFrom //= [] |
        .portMappings = ((.portMappings // []) | map(.hostPort //= .containerPort)));
    def same_except($keys):
      (.change.before | delpaths($keys | map([.]))) == (.change.after | delpaths($keys | map([.])));
    def same_task_settings:
      .change.before as $before | .change.after as $after |
      all(["cpu", "memory", "execution_role_arn", "task_role_arn", "family", "network_mode",
        "runtime_platform", "requires_compatibilities", "volume", "ephemeral_storage", "tags"][];
        . as $key | $before[$key] == $after[$key]);
    def same_container_except_loopback:
      (.change.before.container_definitions | fromjson | normalize) as $before |
      (.change.after.container_definitions | fromjson | normalize) as $after |
      ($after | map(.environment |= map(select(.name != "SERVE_HTTP_LOOPBACK_ONLY")))) == $before and
      all($after[]; any(.environment[]; .name == "SERVE_HTTP_LOOPBACK_ONLY" and .value == "true"));
    [.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])] as $changes |
    all($changes[];
      if (.address | test("^aws_ecs_task_definition\\.service" + service + "$")) then
        .change.actions == ["delete", "create"] and same_container_except_loopback and same_task_settings
      elif (.address | test("^aws_ecs_service\\.service" + service + "$")) then
        .change.actions == ["update"] and same_except(["load_balancer"])
      elif (.address | test("^aws_cloudwatch_metric_alarm\\.target_5xx" + service + "$")) then
        .change.actions == ["update"] and same_except(["metric_query"])
      elif (.address | test("^aws_cloudwatch_metric_alarm\\.unhealthy_hosts" + service + "$")) then
        .change.actions == ["update"] and same_except(["dimensions"])
      elif (.address | test("^aws_lb_listener_rule\\.(service" + service + "|tracking_alias\\[\\\"[0-9]+\\\"\\])$")) then
        .change.actions == ["update"] and same_except(["action"])
      elif (.address | test("^aws_(lb_target_group|vpc_security_group_ingress_rule)\\.service" + service + "$")) then
        .change.actions == ["delete"]
      elif .address == "aws_vpc_security_group_ingress_rule.internal_backend[0]" then
        .change.actions == ["delete"]
      else false end)
  ' "$1" >/dev/null || { echo 'Plan contains changes outside the runtime TLS switch' >&2; return 1; }
}

main() {
  local action=${1:?}
  if [ "$action" = check-plan ]; then check_plan "${2:?}"; return; fi
  case "$action" in plan|apply) ;; *) echo 'Unknown action' >&2; return 1 ;; esac
  : "${STATE_BUCKET:?}" "${WORK:?}" "${VARS_VERSION:?}" "${RELEASE:?}"
  [[ "$VARS_VERSION" != null && "$VARS_VERSION" != *$'\n'* && "$RELEASE" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
  mkdir -p "$WORK"
  chmod 700 "$WORK"
  umask 077
  local dir=deploy/terraform/aws/runtime vars="$WORK/runtime.tfvars.json" log="$WORK/runtime.log"
  aws s3api get-object --bucket "$STATE_BUCKET" --key warmbly/production/vars/runtime-tls.tfvars.json \
    --version-id "$VARS_VERSION" "$vars" >/dev/null
  jq -e '.target_tls == "both"' "$vars" >/dev/null
  jq -r '.. | strings | select(length >= 6)' "$vars" | while IFS= read -r line; do
    printf '::add-mask::%s\n' "$line" >&2
  done
  local account cluster prefix definition live
  account=$(aws sts get-caller-identity --query Account --output text)
  [ "$account" = 925323749693 ]
  cluster=$(jq -r '.name // "warmbly-production"' "$vars")
  prefix=$cluster
  definition=$(aws ecs describe-services --cluster "$cluster" --services "$prefix-backend" --query 'services[0].taskDefinition' --output text)
  live=$(aws ecs describe-task-definition --task-definition "$definition" --include TAGS --query "tags[?key=='warmbly-release'].value | [0]" --output text)
  [ "$live" = "$RELEASE" ] || { echo 'The live backend release changed' >&2; return 1; }
  local service arn count
  for service in backend forms tracking realtime; do
    arn=$(aws elbv2 describe-target-groups --names "$prefix-$service-tls" --query 'TargetGroups[0].TargetGroupArn' --output text)
    count=$(aws elbv2 describe-target-health --target-group-arn "$arn" --query 'TargetHealthDescriptions' --output json |
      jq 'if length > 0 and all(.[]; .TargetHealth.State == "healthy") then length else 0 end')
    [ "$count" -gt 0 ] || { echo "HTTPS targets are not healthy for $service" >&2; return 1; }
  done
  terraform -chdir="$dir" init -input=false -no-color -backend-config="bucket=$STATE_BUCKET" >"$log" 2>&1
  terraform -chdir="$dir" plan -input=false -no-color -lock-timeout=5m -var-file="$vars" \
    -var="state_bucket=$STATE_BUCKET" -var=target_tls=on \
    -target=aws_ecs_service.service -target=aws_lb_listener_rule.tracking_alias \
    -target=aws_vpc_security_group_ingress_rule.service -target=aws_vpc_security_group_ingress_rule.internal_backend \
    -out="$WORK/runtime.tfplan" >>"$log" 2>&1 || { echo 'Runtime TLS plan failed; inspect the private runner log' >&2; return 1; }
  terraform -chdir="$dir" show -json "$WORK/runtime.tfplan" >"$WORK/runtime.json"
  check_plan "$WORK/runtime.json"
  local digest
  digest=$(jq -cS '[.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])] | sort_by(.address)' "$WORK/runtime.json" | sha256sum | cut -d' ' -f1)
  if [ "$action" = plan ]; then
    printf 'digest=%s\n' "$digest" >>"${GITHUB_OUTPUT:?}"
    jq -r '.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"]) | "\(.change.actions | join("/")) \(.address)"' "$WORK/runtime.json" >>"${GITHUB_STEP_SUMMARY:?}"
  else
    [ "$digest" = "${APPROVED_DIGEST:?}" ] || { echo 'Runtime TLS plan changed; start a new approval run' >&2; return 1; }
    terraform -chdir="$dir" apply -input=false -no-color -lock-timeout=5m "$WORK/runtime.tfplan" >>"$log" 2>&1 || { echo 'Runtime TLS apply failed; inspect state before retrying' >&2; return 1; }
    echo 'Runtime TLS apply completed'
  fi
}
main "$@"
