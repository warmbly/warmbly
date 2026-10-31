#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
expect() {
  local want=$1 address=$2 actions=$3
  jq -n --arg address "$address" --argjson actions "$actions" '{resource_changes:[{address:$address,change:{actions:$actions}}]}' >"$work/plan.json"
  if bash .github/scripts/runtime-tls.sh check-plan "$work/plan.json" 2>/dev/null; then
    [ "$want" = pass ]
  else
    [ "$want" = fail ]
  fi
}
expect pass 'aws_lb_target_group.service["backend"]' '["delete"]'
expect pass 'aws_ecs_service.service["forms"]' '["update"]'
expect pass 'aws_vpc_security_group_ingress_rule.internal_backend[0]' '["delete"]'
expect fail 'aws_ecs_task_definition.validation' '["delete","create"]'
expect fail 'aws_s3_bucket.data' '["delete"]'
expect fail 'aws_ecs_service.service["consumer"]' '["update"]'
expect fail 'aws_lb_target_group.service["backend"]' '["delete","create"]'
jq -n '{resource_changes:[{address:"aws_ecs_task_definition.service[\"backend\"]",change:{actions:["delete","create"],before:{container_definitions:([{image:"pinned",environment:[]}]|tojson)},after:{container_definitions:([{image:"pinned",environment:[{name:"SERVE_HTTP_LOOPBACK_ONLY",value:"true"}]}]|tojson)}}}]}' >"$work/plan.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/plan.json"
jq '.resource_changes[0].change.after.container_definitions |= (fromjson | .[0].image = "changed" | tojson)' "$work/plan.json" >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" 2>/dev/null; then
  echo 'Changed image was accepted' >&2
  exit 1
fi
jq '.resource_changes[0].change.before.cpu = "512" | .resource_changes[0].change.after.cpu = "1024"' "$work/plan.json" >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" 2>/dev/null; then
  echo 'Changed task settings were accepted' >&2
  exit 1
fi
jq -n '{resource_changes:[{address:"aws_ecs_service.service[\"backend\"]",change:{actions:["update"],before:{network_configuration:[]},after:{network_configuration:[{subnets:["other"]}]}}}]}' >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" 2>/dev/null; then
  echo 'Changed service network was accepted' >&2
  exit 1
fi
echo 'Runtime TLS plan guards passed'
