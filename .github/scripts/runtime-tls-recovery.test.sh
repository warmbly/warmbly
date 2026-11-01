#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
guard=.github/scripts/runtime-tls-recovery.sh
jq -n '
  ["backend", "forms", "tracking", "realtime"] |
  map({TargetGroupName:("warmbly-production-" + .),
    TargetGroupArn:("arn:aws:elasticloadbalancing:us-east-1:925323749693:targetgroup/warmbly-production-" + . + "/0123456789abcdef"),
    Protocol:"HTTP",Port:8080,VpcId:"vpc-0123456789abcdef0",LoadBalancerArns:[],targets:[]}) as $groups |
  {account:"925323749693",region:"us-east-1",httpGroups:$groups,
   state:{lineage:"01234567-89ab-cdef-0123-456789abcdef",serial:1,versionId:"reviewed-version",sha256:("a"*64),httpGroups:$groups}}
' >"$work/baseline.json"
jq '{resource_changes:[.httpGroups[] | {
  address:("aws_lb_target_group.service[\"" + (.TargetGroupName | ltrimstr("warmbly-production-")) + "\"]"),
  change:{actions:["delete"],after:null,before:{arn:.TargetGroupArn,id:.TargetGroupArn,name:.TargetGroupName,protocol:.Protocol,port:.Port,vpc_id:.VpcId}}
}]}' "$work/baseline.json" >"$work/plan.json"
bash "$guard" check-baseline "$work/baseline.json"
bash "$guard" check-plan "$work/plan.json" "$work/baseline.json"
jq '.httpGroups |= reverse' "$work/baseline.json" >"$work/reordered-baseline.json"
jq '.resource_changes |= reverse |
  .resource_changes += [{address:"data.aws_caller_identity.current",change:{actions:["read"]}}]' "$work/plan.json" >"$work/reordered-plan.json"
bash "$guard" check-plan "$work/reordered-plan.json" "$work/reordered-baseline.json"
for change in '.account="123456789012"' '.region="us-west-2"' \
  '.httpGroups|=.[1:]' '.httpGroups[0]=.httpGroups[1]' \
  '.httpGroups[0].LoadBalancerArns=["still-referenced"]' \
  '.httpGroups[0].targets=[{Target:{Id:"still-registered"}}]' \
  '.httpGroups[0].Protocol="HTTPS"' '.httpGroups[0].Port=0' \
  '.httpGroups[0].VpcId="vpc-11111111"' \
  '.httpGroups[0].TargetGroupArn="arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/warmbly-production-backend/0123456789abcdef"' \
  '.httpGroups[0].targets=null' '.httpGroups[0].LoadBalancerArns=null' \
  '.state.httpGroups[0].TargetGroupArn="changed"' '.state.httpGroups|=.[1:]' \
  '.state.versionId=""' '.state.versionId="null"' '.state.sha256=""' \
  '.state.lineage=null' '.state.lineage=("-"*36)' '.state.serial=-1'; do
  jq "$change" "$work/baseline.json" >"$work/changed.json"
  if bash "$guard" check-plan "$work/plan.json" "$work/changed.json" 2>/dev/null; then
    echo "Recovery accepted an invalid baseline: $change" >&2
    exit 1
  fi
done
for change in '.resource_changes=[]' '.resource_changes|=.[1:]' \
  '.resource_changes[0]=.resource_changes[1]' \
  '.resource_changes[0].change.actions=["no-op"]' \
  '.resource_changes[0].change.actions=["delete","create"]' \
  '.resource_changes[0].change.after={}' \
  'del(.resource_changes[0].change.after)' \
  '.resource_changes[0].change.before.arn="changed"' \
  '.resource_changes[0].change.before.id="changed"' \
  '.resource_changes[0].change.before.port=465' \
  '.resource_changes[0].change.before.protocol="HTTPS"' \
  '.resource_changes[0].change.before.name="different"' \
  '.resource_changes[0].change.before.vpc_id="vpc-11111111"' \
  '.resource_changes[0].address="aws_lb_target_group.service[\"consumer\"]"' \
  '.resource_changes += [{address:"aws_ecs_service.service[\"backend\"]",change:{actions:["update"]}}]' \
  '.resource_changes += [{address:"aws_vpc_security_group_ingress_rule.internal_backend[0]",change:{actions:["delete"]}}]' \
  '.resource_drift=[{address:"aws_lb_target_group.service[\"backend\"]",change:{actions:["update"]}}]'; do
  jq "$change" "$work/plan.json" >"$work/changed.json"
  if bash "$guard" check-plan "$work/changed.json" "$work/baseline.json" 2>/dev/null; then
    echo "Recovery accepted a different cleanup plan: $change" >&2
    exit 1
  fi
done
for action in plan apply delete; do
  if bash "$guard" "$action" 2>/dev/null; then
    echo 'Read-only recovery validator accepted an execution action' >&2
    exit 1
  fi
done
echo 'Read-only TLS recovery identity and scope guards passed'
