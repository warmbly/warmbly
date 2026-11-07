#!/usr/bin/env bash
# Preserve historical workflow identities when collecting recovery evidence.
set -euo pipefail

check_contract() {
  jq -e '
    .schemaVersion == 1 and .repository == "warmbly/warmbly" and
    all(.release, .drain, .failedOn;
      (.tag | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")) and
      (.source | type == "string" and test("^[a-f0-9]{40}$")) and
      (.runId | type == "number" and . > 0 and floor == .) and
      (.attempt | type == "number" and . > 0 and floor == .)) and
    ([.release.runId, .drain.runId, .failedOn.runId] | unique | length == 3) and
    .drain.tag == .failedOn.tag and .release.tag != .drain.tag
  ' "$1" >/dev/null || { echo 'Recovery requires explicit current and historical workflow identities' >&2; return 1; }
}

check_lineage() {
  local evidence=$1 contract=$2 repository=$3 tag source
  check_contract "$contract"
  jq -e --slurpfile contract "$contract" '
    $contract[0] as $expected |
    def run_matches($identity; $path; $event; $branch; $conclusion):
      .id == $identity.runId and .run_attempt == $identity.attempt and
      .head_sha == $identity.source and .repository.full_name == $expected.repository and
      .path == $path and .event == $event and .head_branch == $branch and
      .status == "completed" and .conclusion == $conclusion;
    (.runs.release | run_matches($expected.release; ".github/workflows/release.yml"; "push"; $expected.release.tag; "success")) and
    (.runs.drain | run_matches($expected.drain; ".github/workflows/runtime-tls.yml"; "workflow_dispatch"; "main"; "success") and
      .display_title == ("Runtime TLS drain " + $expected.drain.tag)) and
    (.runs.failedOn | run_matches($expected.failedOn; ".github/workflows/runtime-tls.yml"; "workflow_dispatch"; "main"; "failure") and
      .display_title == ("Runtime TLS on " + $expected.failedOn.tag)) and
    (.failedOnJobs | type == "array" and length > 0) and
    ([.failedOnJobs[].id] | unique | length) == (.failedOnJobs | length) and
    all(.failedOnJobs[];
      .run_id == $expected.failedOn.runId and .run_attempt == $expected.failedOn.attempt and
      .status == "completed") and
    ([.failedOnJobs[] | select(.name == "apply")] | length == 1) and
    any(.failedOnJobs[]; .name == "apply" and .conclusion == "success") and
    any(.failedOnJobs[]; (.name | startswith("redeploy / ")) and .conclusion == "failure")
  ' "$evidence" >/dev/null || { echo 'Recovery workflow evidence differs from the reviewed lineage' >&2; return 1; }
  tag=$(jq -r '.release.tag' "$contract")
  source=$(git -C "$repository" rev-parse --verify "refs/tags/$tag^{commit}")
  [ "$source" = "$(jq -r '.release.source' "$contract")" ] || { echo 'Recovery release tag differs from its reviewed source' >&2; return 1; }
  git -C "$repository" merge-base --is-ancestor "$(jq -r '.drain.source' "$contract")" "$(jq -r '.failedOn.source' "$contract")" || {
    echo 'Recovery ON source does not descend from the reviewed drain source' >&2; return 1;
  }
  git -C "$repository" merge-base --is-ancestor "$(jq -r '.failedOn.source' "$contract")" "$source" || {
    echo 'Recovery release does not descend from the reviewed ON source' >&2; return 1;
  }
}

collect_lineage() {
  local contract=$1 work=$2 repository=$3 repo identity run attempt
  check_contract "$contract"
  umask 077
  mkdir "$work"
  repo=$(jq -r '.repository' "$contract")
  for identity in release drain failedOn; do
    run=$(jq -r --arg identity "$identity" '.[$identity].runId' "$contract")
    attempt=$(jq -r --arg identity "$identity" '.[$identity].attempt' "$contract")
    gh api "repos/$repo/actions/runs/$run/attempts/$attempt" >"$work/$identity.json"
  done
  run=$(jq -r '.failedOn.runId' "$contract")
  attempt=$(jq -r '.failedOn.attempt' "$contract")
  gh api "repos/$repo/actions/runs/$run/attempts/$attempt/jobs?per_page=100" --paginate --slurp >"$work/job-pages.json"
  jq -e '.[0].total_count == ([.[].jobs[]] | length)' "$work/job-pages.json" >/dev/null || {
    echo 'Recovery requires the complete failed ON job inventory' >&2; return 1;
  }
  jq -n --slurpfile release "$work/release.json" --slurpfile drain "$work/drain.json" \
    --slurpfile failedOn "$work/failedOn.json" --slurpfile jobs "$work/job-pages.json" \
    '{runs:{release:$release[0],drain:$drain[0],failedOn:$failedOn[0]},failedOnJobs:[$jobs[0][].jobs[]]}' >"$work/candidate.json"
  check_lineage "$work/candidate.json" "$contract" "$repository"
  mv "$work/candidate.json" "$work/lineage.json"
}

case "${1:-}" in
  check) check_lineage "${2:?}" "${3:?}" "${4:?}" ;;
  collect) collect_lineage "${2:?}" "${3:?}" "${4:?}" ;;
  *) echo 'Only read-only recovery lineage check and collection are supported' >&2; exit 1 ;;
esac
