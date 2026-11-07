#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
complete_cleanup() {
  jq '
    [.resource_changes[].address] as $present |
    .resource_changes += [ ["backend", "forms", "tracking", "realtime"][] |
      {address:("aws_lb_target_group.service[\"" + . + "\"]"),change:{actions:["delete"]}} |
      select(.address as $address | $present | index($address) == null)]
  ' "$1" >"$work/complete.json"
  mv "$work/complete.json" "$1"
}
expect() {
  local want=$1 address=$2 actions=$3 stage=${4:-route}
  jq -n --arg address "$address" --argjson actions "$actions" '{resource_changes:[{address:$address,change:{actions:$actions}}]}' >"$work/plan.json"
  if [ "$stage" = on ]; then complete_cleanup "$work/plan.json"; fi
  if bash .github/scripts/runtime-tls.sh check-plan "$work/plan.json" "$stage" 2>/dev/null; then
    [ "$want" = pass ]
  else
    [ "$want" = fail ]
  fi
}
expect fail 'aws_lb_target_group.service["backend"]' '["delete"]'
expect pass 'aws_lb_target_group.service["backend"]' '["delete"]' on
expect fail 'aws_ecs_service.service["forms"]' '["update"]'
expect fail 'aws_vpc_security_group_ingress_rule.internal_backend[0]' '["delete"]'
expect pass 'aws_vpc_security_group_ingress_rule.internal_backend[0]' '["delete"]' on
expect fail 'aws_vpc_security_group_ingress_rule.service["backend"]' '["delete"]'
expect pass 'aws_vpc_security_group_ingress_rule.service["backend"]' '["delete"]' on
expect fail 'aws_ecs_task_definition.validation' '["delete","create"]'
expect fail 'aws_s3_bucket.data' '["delete"]'
expect fail 'aws_ecs_service.service["consumer"]' '["update"]'
expect fail 'aws_lb_target_group.service["backend"]' '["delete","create"]'
expect fail 'aws_lb_target_group.service["backend"]' '["delete"]' drain
expect fail 'aws_vpc_security_group_ingress_rule.internal_backend[0]' '["delete"]' drain
jq -n '{resource_changes:[{address:"aws_ecs_service.service[\"forms\"]",change:{actions:["update"],
  before:{load_balancer:[{target_group_arn:"forms/1",container_port:8090},{target_group_arn:"forms-tls/2",container_port:8443}]},
  after:{load_balancer:[{target_group_arn:"forms-tls/2",container_port:8443}]}}}]}' >"$work/plan.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/plan.json" drain
for stage in route on; do
  cp "$work/plan.json" "$work/registration-$stage.json"
  if [ "$stage" = on ]; then complete_cleanup "$work/registration-$stage.json"; fi
  if bash .github/scripts/runtime-tls.sh check-plan "$work/registration-$stage.json" "$stage" 2>/dev/null; then
    echo 'Registration detached outside the drain stage' >&2
    exit 1
  fi
done
jq '.resource_changes[0].change |= (
  .before.load_balancer |= map(. + {elb_name:"",advanced_configuration:[]}) |
  .after.load_balancer |= map(. + {elb_name:null,advanced_configuration:[]}))' "$work/plan.json" >"$work/normalized.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/normalized.json" drain
for change in '.after.load_balancer[0].elb_name="classic-lb"' \
  '.before.load_balancer[1].elb_name="classic-lb"' \
  '.after.load_balancer[0].container_port=9443' \
  '.after.load_balancer[0].container_name="other"' \
  '.after.load_balancer[0].target_group_arn="other-tls/3"' \
  '.after.load_balancer[0].advanced_configuration=[{alternate_target_group_arn:"other-tls/3"}]'; do
  jq ".resource_changes[0].change |= ($change)" "$work/normalized.json" >"$work/changed.json"
  if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" drain 2>/dev/null; then
    echo 'Changed HTTPS registration accepted as empty ELB name normalization' >&2
    exit 1
  fi
done
jq -n '{resource_changes:[{address:"aws_vpc_security_group_ingress_rule.service[\"backend\"]",change:{actions:["update"],
  before:{description:"",from_port:8080},after:{description:null,from_port:8080}}}]}' >"$work/plan.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/plan.json" route
jq '.resource_changes[0].change.after.from_port=8443' "$work/plan.json" >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" route 2>/dev/null; then
  echo 'Network change accepted as metadata normalization' >&2
  exit 1
fi
jq -n '{resource_changes:[{address:"aws_ecs_task_definition.service[\"backend\"]",change:{actions:["delete","create"],before:{container_definitions:([{image:"pinned",environment:[]}]|tojson)},after:{container_definitions:([{image:"pinned",environment:[{name:"SERVE_HTTP_LOOPBACK_ONLY",value:"true"}]}]|tojson)}}}]}' >"$work/loopback-only.json"
cp "$work/loopback-only.json" "$work/plan.json"
complete_cleanup "$work/plan.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/plan.json" on
if bash .github/scripts/runtime-tls.sh check-plan "$work/loopback-only.json" route 2>/dev/null; then
  echo 'Loopback-only listener accepted before HTTP registrations drained' >&2
  exit 1
fi
jq '.resource_changes |= map(select(.address | startswith("aws_ecs_task_definition"))) |
  .resource_changes[0].change |= ({actions:.actions,before:.after,after:.before})' "$work/plan.json" >"$work/route.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/route.json" route
jq '.resource_changes[0].change.after.container_definitions |= (fromjson | .[0].image = "changed" | tojson)' "$work/plan.json" >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" on 2>/dev/null; then
  echo 'Changed image was accepted' >&2
  exit 1
fi
jq '.resource_changes[0].change.before.cpu = "512" | .resource_changes[0].change.after.cpu = "1024"' "$work/plan.json" >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" on 2>/dev/null; then
  echo 'Changed task settings were accepted' >&2
  exit 1
fi
jq -n '{resource_changes:[{address:"aws_ecs_service.service[\"backend\"]",change:{actions:["update"],before:{network_configuration:[]},after:{network_configuration:[{subnets:["other"]}]}}}]}' >"$work/changed.json"
if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" 2>/dev/null; then
  echo 'Changed service network was accepted' >&2
  exit 1
fi
jq -n '{services:[range(4) | {loadBalancers:[{containerPort:8443,targetGroupArn:"service-tls/123"}]}],
  httpGroups:[range(4) | {LoadBalancerArns:[],targets:[]}]}' >"$work/cleanup.json"
bash .github/scripts/runtime-tls.sh check-cleanup "$work/cleanup.json"
for change in '.services[0].loadBalancers[0].containerPort=8080' \
  '.httpGroups[0].LoadBalancerArns=["listener"]' '.httpGroups[0].targets=[{Id:"still-draining"}]' \
  '.services=[]'; do
  jq "$change" "$work/cleanup.json" >"$work/changed.json"
  if bash .github/scripts/runtime-tls.sh check-cleanup "$work/changed.json" 2>/dev/null; then
    echo 'HTTP cleanup accepted before all references drained' >&2
    exit 1
  fi
done
jq -n '
  ["backend","forms","tracking","realtime"] as $names |
  {services:{failures:[],services:[$names[] | {serviceName:("test-"+.),taskDefinition:.,desiredCount:1,runningCount:1,pendingCount:0,
    deployments:[{rolloutState:"COMPLETED"}],loadBalancers:[{containerPort:8080,targetGroupArn:(.+"/1")},{containerPort:8443,targetGroupArn:(.+"-tls/2")}]}]},
   definitions:[$names[] | {tags:[{key:"warmbly-release",value:"v0.6.70"}],taskDefinition:{taskDefinitionArn:.,containerDefinitions:[{
     environment:[{name:"SERVE_TLS",value:"supplied"},{name:"SERVE_TLS_PORT",value:"8443"}]}]}}]}' >"$work/live.json"
bash .github/scripts/runtime-tls.sh check-live "$work/live.json" route v0.6.70 8443 test
for change in '.services.services[0].pendingCount=1' '.services.services[0].deployments[0].rolloutState="IN_PROGRESS"' \
  '.services.services[0].loadBalancers|=.[0:1]' '.definitions[0].tags[0].value="v0.6.69"' \
  '.definitions[0].taskDefinition.containerDefinitions[0].environment=[]' \
  '.definitions[0].taskDefinition.containerDefinitions[0].environment += [{name:"SERVE_HTTP_LOOPBACK_ONLY",value:"true"}]' \
  '.services.services|=.[0:3]'; do
  jq "$change" "$work/live.json" >"$work/changed.json"
  if bash .github/scripts/runtime-tls.sh check-live "$work/changed.json" route v0.6.70 8443 test 2>/dev/null; then
    echo 'Incompatible or unstable live services accepted' >&2
    exit 1
  fi
done
jq -n '{resource_changes:[]}' >"$work/deletions.json"
complete_cleanup "$work/deletions.json"
bash .github/scripts/runtime-tls.sh check-plan "$work/deletions.json" on
for change in '.resource_changes=[]' '.resource_changes|=.[1:]' \
  '.resource_changes[0].change.actions=["no-op"]' \
  '.resource_changes[0].change.actions=["delete","create"]' \
  '.resource_changes[0].address="aws_lb_target_group.service[\"consumer\"]"'; do
  jq "$change" "$work/deletions.json" >"$work/changed.json"
  if bash .github/scripts/runtime-tls.sh check-plan "$work/changed.json" on 2>/dev/null; then
    echo 'ON accepted without the exact four HTTP group deletions' >&2
    exit 1
  fi
done
echo 'Runtime TLS plan guards passed'
