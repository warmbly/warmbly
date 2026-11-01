#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/aws" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1 $2" = 'sts get-caller-identity' ]; then
  jq -n --arg mode "${MODE:-}" '{Account:"925323749693",Arn:("arn:aws:sts::925323749693:assumed-role/"+(if $mode=="role" then "other" else "warmbly-production-tls-recovery-evidence" end)+"/fixture")}'
  exit
fi
[ "$1 $2" = 's3api put-object' ]
[[ "$*" == *'--expected-bucket-owner 925323749693'* && "$*" == *'--server-side-encryption AES256'* && "$*" == *'--if-none-match *'* ]]
[[ "$*" == *'warmbly/production/diagnostics/runtime-tls-recovery/999/1/plan.tgz'* ]]
checksum= body=
while [ "$#" -gt 0 ]; do
  case "$1" in --checksum-sha256) checksum=$2; shift 2 ;; --body) body=$2; shift 2 ;; *) shift ;; esac
done
[ "$checksum" = "$(openssl dgst -sha256 -binary "$body" | openssl base64 -A)" ]
tar -tzf "$body" | grep -Fx './full-raw.json' >/dev/null
if [ "${MODE:-}" = upload ]; then exit 1; fi
jq -n --arg checksum "$checksum" --arg mode "${MODE:-}" '{VersionId:"evidence-version",ServerSideEncryption:"AES256",ChecksumSHA256:(if $mode=="checksum" then "other" else $checksum end)}'
SH
chmod +x "$work/bin/aws"
export PATH="$work/bin:$PATH" GITHUB_ACTIONS=true GITHUB_REPOSITORY=warmbly/warmbly GITHUB_REF=refs/heads/main GITHUB_RUN_ATTEMPT=1 GITHUB_RUN_ID=999
guard=.github/scripts/runtime-tls-recovery-archive.sh
for mode in good role upload checksum; do
  mkdir "$work/$mode"
  printf '{"raw":"private fixture"}\n' >"$work/$mode/full-raw.json"
  if [ "$mode" = good ]; then WORK="$work/$mode" bash "$guard" plan
  elif WORK="$work/$mode" MODE=$mode bash "$guard" plan 2>/dev/null; then echo "Archive accepted $mode" >&2; exit 1; fi
done
if WORK="$work/good" bash "$guard" plan 2>/dev/null; then echo 'Archive overwrote prior evidence' >&2; exit 1; fi
if WORK="$work/unused" GITHUB_RUN_ATTEMPT=2 bash "$guard" plan 2>/dev/null; then echo 'Archive accepted a rerun' >&2; exit 1; fi
echo 'Private evidence archive identity, prefix, encryption, integrity and overwrite checks passed'
