#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
guard=.github/scripts/runtime-tls-recovery-lineage.sh
mkdir "$work/repository"
git -C "$work/repository" init -q
git -C "$work/repository" config user.name 'Recovery fixture'
git -C "$work/repository" config user.email 'fixture@example.invalid'
git -C "$work/repository" commit -qm 'feat: establish drain fixture' --allow-empty
drain=$(git -C "$work/repository" rev-parse HEAD)
git -C "$work/repository" commit -qm 'feat: establish ON fixture' --allow-empty
on=$(git -C "$work/repository" rev-parse HEAD)
git -C "$work/repository" commit -qm 'feat: establish release fixture' --allow-empty
release=$(git -C "$work/repository" rev-parse HEAD)
git -C "$work/repository" tag v1.0.1
jq -n --arg drain "$drain" --arg on "$on" --arg release "$release" '
  {schemaVersion:1,repository:"warmbly/warmbly",
   release:{tag:"v1.0.1",source:$release,runId:101,attempt:1},
   drain:{tag:"v1.0.0",source:$drain,runId:102,attempt:1},
   failedOn:{tag:"v1.0.0",source:$on,runId:103,attempt:1}}
' >"$work/contract.json"
jq '
  def run($path; $event; $branch; $conclusion):
    {id:.runId,run_attempt:.attempt,head_sha:.source,repository:{full_name:"warmbly/warmbly"},
     path:$path,event:$event,head_branch:$branch,status:"completed",conclusion:$conclusion};
  {runs:{
    release:(.release | run(".github/workflows/release.yml";"push";.tag;"success")),
    drain:(.drain | run(".github/workflows/runtime-tls.yml";"workflow_dispatch";"main";"success") + {display_title:("Runtime TLS drain " + .tag)}),
    failedOn:(.failedOn | run(".github/workflows/runtime-tls.yml";"workflow_dispatch";"main";"failure") + {display_title:("Runtime TLS on " + .tag)})},
   failedOnJobs:[{id:1,run_id:103,run_attempt:1,status:"completed",name:"apply",conclusion:"success"},
     {id:2,run_id:103,run_attempt:1,status:"completed",name:"redeploy / redeploy v1.0.0",conclusion:"failure"}]}
' "$work/contract.json" >"$work/evidence.json"
bash "$guard" check "$work/evidence.json" "$work/contract.json" "$work/repository"
for change in '.schemaVersion=2' '.repository="other/repo"' '.release.runId=.drain.runId' \
  '.drain.tag=.release.tag' '.failedOn.tag="v0.9.0"' '.release.tag="main"' \
  '.failedOn.attempt=0' '.drain.source="unknown"'; do
  jq "$change" "$work/contract.json" >"$work/changed.json"
  if bash "$guard" check "$work/evidence.json" "$work/changed.json" "$work/repository" 2>/dev/null; then
    echo "Recovery accepted a different lineage contract: $change" >&2; exit 1
  fi
done
for change in '.runs.release.id=104' '.runs.release.head_sha=.runs.drain.head_sha' \
  '.runs.release.repository.full_name="other/repo"' '.runs.release.head_branch="main"' \
  '.runs.release.path=".github/workflows/deploy-aws.yml"' '.runs.release.conclusion="failure"' \
  '.runs.release.event="workflow_dispatch"' '.runs.release.status="in_progress"' \
  '.runs.drain.run_attempt=2' '.runs.drain.conclusion="failure"' \
  '.runs.drain.display_title="Runtime TLS drain v1.0.1"' \
  '.runs.failedOn.display_title="Runtime TLS on v1.0.1"' '.runs.failedOn.conclusion="success"' \
  '.failedOnJobs=[]' '.failedOnJobs += [.failedOnJobs[0]]' '.failedOnJobs[0].conclusion="failure"' \
  '.failedOnJobs[1].conclusion="success"' '.failedOnJobs[0].run_id=104' \
  '.failedOnJobs[0].run_attempt=2' '.failedOnJobs[1].status="in_progress"'; do
  jq "$change" "$work/evidence.json" >"$work/changed.json"
  if bash "$guard" check "$work/changed.json" "$work/contract.json" "$work/repository" 2>/dev/null; then
    echo "Recovery accepted different workflow evidence: $change" >&2; exit 1
  fi
done
git -C "$work/repository" tag -f v1.0.1 "$drain" >/dev/null
if bash "$guard" check "$work/evidence.json" "$work/contract.json" "$work/repository" 2>/dev/null; then
  echo 'Recovery accepted a release tag on a different source' >&2; exit 1
fi
git -C "$work/repository" tag -f v1.0.1 "$release" >/dev/null
jq --arg source "$release" '.drain.source=$source' "$work/contract.json" >"$work/reversed-contract.json"
jq --arg source "$release" '.runs.drain.head_sha=$source' "$work/evidence.json" >"$work/reversed-evidence.json"
if bash "$guard" check "$work/reversed-evidence.json" "$work/reversed-contract.json" "$work/repository" 2>/dev/null; then
  echo 'Recovery accepted reversed drain and ON ancestry' >&2; exit 1
fi
jq --arg source "$drain" '.release.source=$source' "$work/contract.json" >"$work/reversed-contract.json"
jq --arg source "$drain" '.runs.release.head_sha=$source' "$work/evidence.json" >"$work/reversed-evidence.json"
git -C "$work/repository" tag -f v1.0.1 "$drain" >/dev/null
if bash "$guard" check "$work/reversed-evidence.json" "$work/reversed-contract.json" "$work/repository" 2>/dev/null; then
  echo 'Recovery accepted a release that predates the ON source' >&2; exit 1
fi
git -C "$work/repository" tag -f v1.0.1 "$release" >/dev/null
mkdir "$work/bin"
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FIXTURES/requests.log"
[ "${API_FAIL:-false}" = false ] || exit 1
case "$2" in
  */runs/101/attempts/1) jq '.runs.release' "$FIXTURES/evidence.json" ;;
  */runs/102/attempts/1) jq '.runs.drain' "$FIXTURES/evidence.json" ;;
  */runs/103/attempts/1) jq '.runs.failedOn' "$FIXTURES/evidence.json" ;;
  */runs/103/attempts/1/jobs\?per_page=100)
    [ "$3 $4" = '--paginate --slurp' ]
    jq --arg mode "${JOB_MODE:-complete}" '[{total_count:2,jobs:(if $mode == "partial" then .failedOnJobs[:1] else .failedOnJobs end)}]' "$FIXTURES/evidence.json" ;;
  *) exit 1 ;;
esac
SH
chmod +x "$work/bin/gh"
PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/contract.json" "$work/collected" "$work/repository"
cmp "$work/evidence.json" "$work/collected/lineage.json"
if PATH="$work/bin:$PATH" FIXTURES="$work" JOB_MODE=partial bash "$guard" collect "$work/contract.json" "$work/partial" "$work/repository" 2>/dev/null; then
  echo 'Recovery accepted an incomplete job collection' >&2; exit 1
fi
[ ! -e "$work/partial/lineage.json" ]
if PATH="$work/bin:$PATH" FIXTURES="$work" API_FAIL=true bash "$guard" collect "$work/contract.json" "$work/api-failure" "$work/repository" 2>/dev/null; then
  echo 'Recovery accepted a failed GitHub API read' >&2; exit 1
fi
[ ! -e "$work/api-failure/lineage.json" ]
if PATH="$work/bin:$PATH" FIXTURES="$work" bash "$guard" collect "$work/contract.json" "$work/collected" "$work/repository" 2>/dev/null; then
  echo 'Recovery overwrote an existing lineage collection' >&2; exit 1
fi
if bash "$guard" apply 2>/dev/null; then
  echo 'Recovery lineage collector accepted an execution action' >&2; exit 1
fi
echo 'Read-only recovery workflow lineage collection and checks passed'
