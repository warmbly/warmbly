#!/usr/bin/env bash
# Preserve recovery raw evidence only in the dedicated private diagnostic prefix.
set -euo pipefail
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${GITHUB_REPOSITORY:-}" = warmbly/warmbly ] &&
  [ "${GITHUB_REF:-}" = refs/heads/main ] && [ "${GITHUB_RUN_ATTEMPT:-}" = 1 ] &&
  [[ "${GITHUB_RUN_ID:-}" =~ ^[1-9][0-9]*$ ]] || exit 1
phase=${1:?}
[[ "$phase" = plan || "$phase" = apply || "$phase" = verify ]]
: "${WORK:?}"
[ -d "$WORK" ] && [ ! -e "$WORK.evidence.tgz" ]
umask 077
aws sts get-caller-identity --region us-east-1 >"$WORK/archive-identity.json"
jq -e '.Account == "925323749693" and (.Arn | test("^arn:aws:sts::925323749693:assumed-role/warmbly-production-tls-recovery-evidence/[^/]+$"))' \
  "$WORK/archive-identity.json" >/dev/null
tar -czf "$WORK.evidence.tgz" -C "$WORK" .
checksum=$(openssl dgst -sha256 -binary "$WORK.evidence.tgz" | openssl base64 -A)
aws s3api put-object --region us-east-1 --bucket warmbly-terraform-use1-925323749693 \
  --key "warmbly/production/diagnostics/runtime-tls-recovery/$GITHUB_RUN_ID/1/$phase.tgz" \
  --expected-bucket-owner 925323749693 --server-side-encryption AES256 --if-none-match '*' \
  --checksum-algorithm SHA256 --checksum-sha256 "$checksum" --body "$WORK.evidence.tgz" >"$WORK.evidence-metadata.json"
jq -e --arg checksum "$checksum" '.ServerSideEncryption == "AES256" and .ChecksumSHA256 == $checksum and
  (.VersionId | type == "string" and length > 0 and . != "null")' "$WORK.evidence-metadata.json" >/dev/null
echo 'Full recovery evidence preserved in the private versioned diagnostic object'
