#!/usr/bin/env bash
# Read-only validators for a separately reviewed hosted TLS cleanup recovery.
set -euo pipefail

check_baseline() {
  jq -e '
    def group_identity: {TargetGroupName, TargetGroupArn, Protocol, Port, VpcId};
    . as $baseline |
    .account == "925323749693" and .region == "us-east-1" and
    (.state.lineage | type == "string" and test("^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$")) and
    (.state.serial | type == "number" and . >= 0 and floor == .) and
    (.state.versionId | type == "string" and length > 0 and . != "null" and (test("[\\r\\n]") | not)) and
    (.state.sha256 | type == "string" and test("^[a-f0-9]{64}$")) and
    (.httpGroups | type == "array" and length == 4) and
    ([.httpGroups[].TargetGroupName] | sort) ==
      (["backend", "forms", "tracking", "realtime"] | map("warmbly-production-" + .) | sort) and
    ([.httpGroups[].TargetGroupArn] | unique | length == 4) and
    ([.httpGroups[].VpcId] | unique | length == 1) and
    all(.httpGroups[];
      .Protocol == "HTTP" and
      (.Port | type == "number" and . > 0 and . <= 65535 and floor == .) and
      (.VpcId | type == "string" and test("^vpc-[a-f0-9]+$")) and
      .TargetGroupArn == ("arn:aws:elasticloadbalancing:us-east-1:925323749693:targetgroup/" + .TargetGroupName + "/" + (.TargetGroupArn | split("/")[-1])) and
      (.TargetGroupArn | split("/")[-1] | test("^[a-f0-9]{16}$")) and
      (.LoadBalancerArns | type == "array" and length == 0) and
      (.targets | type == "array" and length == 0)) and
    (.state.httpGroups | type == "array" and length == 4) and
    (.state.httpGroups | map(group_identity) | sort_by(.TargetGroupName)) ==
      ($baseline.httpGroups | map(group_identity) | sort_by(.TargetGroupName))
  ' "$1" >/dev/null || { echo 'Recovery requires four identified, drained HTTP groups matching reviewed state' >&2; return 1; }
}

check_plan() {
  check_baseline "$2"
  jq -e --slurpfile baseline "$2" '
    (.resource_changes | type == "array") and
    (
    [.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])] as $changes |
    ($baseline[0].httpGroups | map({
      address:("aws_lb_target_group.service[\"" + (.TargetGroupName | ltrimstr("warmbly-production-")) + "\"]"),
      group:.
    })) as $expected |
    ($changes | map(.address) | sort) == ($expected | map(.address) | sort) and
    all($changes[];
      . as $change |
      .change.actions == ["delete"] and (.change | has("after")) and .change.after == null and
      any($expected[];
        .address == $change.address and
        $change.change.before.arn == .group.TargetGroupArn and
        $change.change.before.id == .group.TargetGroupArn and
        $change.change.before.name == .group.TargetGroupName and
        $change.change.before.protocol == .group.Protocol and
        $change.change.before.port == .group.Port and
        $change.change.before.vpc_id == .group.VpcId)) and
    ([.resource_drift[]? | select(.change.actions != ["no-op"])] | length == 0))
  ' "$1" >/dev/null || { echo 'Recovery plan must contain only the four reviewed HTTP group deletions without drift' >&2; return 1; }
}

case "${1:-}" in
  check-baseline) check_baseline "${2:?}" ;;
  check-plan) check_plan "${2:?}" "${3:?}" ;;
  *) echo 'Only read-only recovery validation is supported' >&2; exit 1 ;;
esac
