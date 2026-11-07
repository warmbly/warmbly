#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
classifier=${CLASSIFIER:-.github/scripts/terraform-stack.sh}

check() {
  local expected=$1 type=$2 name=$3 actions=$4
  jq -n --arg type "$type" --arg name "$name" --argjson actions "$actions" \
    '{resource_changes:[{address:($type+"."+$name+"[0]"),type:$type,name:$name,change:{actions:$actions}}]}' >"$work/plan.json"
  local actual
  actual=$(bash "$classifier" classify "$work/plan.json")
  if [ "$actual" != "$expected" ]; then
    echo "Expected $expected for $type.$name $actions, received $actual" >&2
    exit 1
  fi
}

for type in aws_iam_role aws_iam_role_policy; do
  for actions in '["create"]' '["update"]' '["delete"]' '["delete","create"]'; do
    check gated "$type" tls_recovery_evidence "$actions"
  done
  check none "$type" tls_recovery_evidence '["no-op"]'
  check none "$type" tls_recovery_evidence '["read"]'
done
check safe aws_iam_role existing_role '["create"]'
check safe aws_ecs_task_definition service '["delete","create"]'
check gated aws_lb_target_group service '["delete"]'
check safe aws_cloudwatch_metric_alarm service '["update"]'
echo 'Protected evidence IAM changes require approval; existing plan classifications remain unchanged'
