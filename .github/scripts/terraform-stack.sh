#!/usr/bin/env bash
# Plans or applies one AWS stack without putting a value in a public log:
# var files come from the private state bucket and are masked first, plan and
# apply output goes to a private file, and only resource addresses and actions
# are printed.
#
#   terraform-stack.sh plan <foundation|runtime|workers>
#   terraform-stack.sh apply <stack>      applies the plan saved by plan
#   terraform-stack.sh classify <plan.json>
#
# plan writes $WORK/<stack>.summary (one "action address" per line) and prints
# safe, gated or none. Safe means only creates, in-place updates, and new
# revisions of task definitions, excluding protected recovery evidence IAM changes.
set -euo pipefail

classify() {
  jq -r '
    [.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"])] as $c |
    if ($c | length) == 0 then "none"
    elif any($c[]; .name == "tls_recovery_evidence" and
      (.type == "aws_iam_role" or .type == "aws_iam_role_policy")) then "gated"
    elif any($c[]; (.change.actions | index("delete")) and
      ((.type == "aws_ecs_task_definition" and (.change.actions | length) == 2) | not)) then "gated"
    else "safe" end' "$1"
}

summary() {
  jq -r '.resource_changes[]? | select(.change.actions != ["no-op"] and .change.actions != ["read"]) |
    "\(.change.actions | join("/")) \(.address)"' "$1" | sort
}

main() {
  local action=$1 stack=${2:-}
  if [ "$action" = classify ]; then classify "$stack"; return 0; fi
  case "$stack" in foundation|runtime|workers) ;; *) echo "unknown stack $stack" >&2; return 1 ;; esac
  : "${STATE_BUCKET:?}" "${WORK:?}"
  local dir="deploy/terraform/aws/$stack" vars="$WORK/$stack.tfvars.json" out="$WORK/$stack.log"
  mkdir -p "$WORK" && chmod 700 "$WORK"

  if [ "$action" = plan ]; then
    aws s3 cp --only-show-errors "s3://$STATE_BUCKET/warmbly/production/vars/$stack.tfvars.json" "$vars"
    chmod 600 "$vars"
    # Mask every value before anything can echo one.
    jq -r '.. | strings | select(length >= 6)' "$vars" | while IFS= read -r line; do
      # stderr: callers capture stdout for the class, and the runner reads commands on both.
      if [ -n "$line" ]; then echo "::add-mask::$line" >&2; fi
    done
    terraform -chdir="$dir" init -input=false -no-color -backend-config="bucket=$STATE_BUCKET" >"$out" 2>&1 || { tail -n 20 "$out" >&2; return 1; }
    local code=0
    terraform -chdir="$dir" plan -input=false -no-color -lock-timeout=5m -var-file="$vars" -var="state_bucket=$STATE_BUCKET" \
      -out="$WORK/$stack.tfplan" -detailed-exitcode >>"$out" 2>"$WORK/$stack.err" || code=$?
    if [ "$code" = 1 ]; then
      echo "terraform plan failed for $stack:" >&2
      grep -E '^(Error|│ Error|  on |│   on )' "$WORK/$stack.err" | head -n 20 >&2 || true
      return 1
    fi
    terraform -chdir="$dir" show -json "$WORK/$stack.tfplan" >"$WORK/$stack.json"
    summary "$WORK/$stack.json" >"$WORK/$stack.summary"
    classify "$WORK/$stack.json"
    rm -f "$WORK/$stack.json"
    return 0
  fi

  if [ "$action" = apply ]; then
    terraform -chdir="$dir" apply -input=false -no-color -lock-timeout=5m "$WORK/$stack.tfplan" >"$out" 2>"$WORK/$stack.err" || {
      echo "terraform apply failed for $stack:" >&2
      grep -E '^(Error|│ Error|  on |│   on )' "$WORK/$stack.err" | head -n 20 >&2 || true
      return 1
    }
    grep -E '^Apply complete' "$out" || true
    return 0
  fi
  echo "unknown action $action" >&2
  return 1
}

main "$@"
