#!/bin/sh
# Everything CI should know about the fleet join script.
#
# The script is served verbatim from the backend at GET /join.sh and is what a
# stranger pipes into a root shell to add a machine, which makes it the
# highest-consequence file in the repo that is not Go. Nothing else covered it,
# and that is how a systemd unit that could never start, an env file with a
# stray JSON fragment in it, and a state directory the node could not write all
# reached the branch at once.
#
# What is checked:
#   - POSIX parse under dash, which is /bin/sh on Debian and Ubuntu
#   - shellcheck, in sh mode
#   - --help exits 0 and says something
#   - the generated systemd unit is ONE ExecStart line with the image as a
#     systemd variable, not a command substitution systemd would never expand
#   - the mount list always includes the agent directory
#   - a relative BLOB_FS_ROOT is refused rather than mounted
#   - the local override env file is passed AFTER node.env, so it wins, and is
#     created without ever truncating one that is already there
#   - --dry-run masks every credential, including the ones that hide inside a
#     URL, and does it with an expression this sed actually supports
set -eu

SCRIPT="internal/api/handler/nodescript/join.sh"
fail() { printf 'check-join-script: %s\n' "$*" >&2; exit 1; }
ok()   { printf '  ok  %s\n' "$*"; }

[ -f "$SCRIPT" ] || fail "$SCRIPT not found (run from the repository root)"

# POSIX parse. sh -n under a non-POSIX shell proves nothing about dash, which
# is /bin/sh on Debian and Ubuntu, so dash is used when it is there.
parse_check() {
  if command -v dash >/dev/null 2>&1; then
    dash -n "$1" || fail "dash -n failed on $1"
  else
    sh -n "$1" || fail "sh -n failed on $1"
  fi
}

parse_check "$SCRIPT"
# The checker too: its own shellcheck-disable directives are load-bearing.
parse_check "$0"
if command -v dash >/dev/null 2>&1; then
  ok "dash -n (script and checker)"
else
  printf '  --  dash not installed; parsed with sh -n instead\n'
fi

if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -s sh "$SCRIPT" || fail "shellcheck failed on $SCRIPT"
  shellcheck -s sh "$0" || fail "shellcheck failed on $0"
  ok "shellcheck -s sh (script and checker)"
else
  printf '  --  shellcheck not installed; skipped\n'
fi

out=$(sh "$SCRIPT" --help) || fail "--help exited non-zero"
printf '%s' "$out" | grep -q -- "--token" || fail "--help does not document --token"
ok "--help"

# Everything below asserts on what the script RENDERS, never on its source
# text. A previous version of this file checked a heredoc copied in here, which
# meant putting the original `$(cat ...)` bug back left it passing green.
# The one non-default environment the unit is rendered under. Named for what
# it is rather than dressed up as a list: sh has no clean way to iterate blocks
# that themselves contain newlines, so a third variant means adding it to the
# `for` below by hand.
FS_BLOB_ENV='BLOB_PROVIDER=fs
BLOB_FS_ROOT=/var/lib/warmbly/blobs'

# NODE_ENV is forced empty rather than inherited: this repo exports NODE_ENV in
# several trees, and an inherited value would render a unit this check did not
# choose, or fail validate_blob_root for an unrelated reason.
unit=$(NODE_ENV="" sh "$SCRIPT" --print-unit) || fail "--print-unit failed"

# shellcheck disable=SC2016  # the pattern is literal on purpose; it must not expand
printf '%s\n' "$unit" | grep -q 'ExecStart=.*\${WARMBLY_IMAGE_REF}$' \
  || fail "ExecStart must end with the systemd variable \${WARMBLY_IMAGE_REF}"
# shellcheck disable=SC2016  # literal on purpose
if printf '%s\n' "$unit" | grep -q 'ExecStart=.*\$('; then
  fail "ExecStart contains a command substitution; systemd never expands one"
fi
[ "$(printf '%s\n' "$unit" | grep -c '^ExecStart=')" = "1" ] \
  || fail "ExecStart must be exactly one line"
printf '%s\n' "$unit" | grep -q '^EnvironmentFile=/var/lib/warmbly/image-ref$' \
  || fail "image-ref must stay in the root-owned state dir; the node must not be able to rewrite it"
printf '%s\n' "$unit" | grep -q 'ExecStart=.*-v /var/lib/warmbly/node:/var/lib/warmbly/node' \
  || fail "the agent directory must always be mounted, or auto-update stops silently"
ok "rendered unit (no blob mount)"

# node.local.env is the operator's half of the config and the only one a
# re-join does not rewrite. Docker applies --env-file in order, so it has to
# come AFTER node.env or an override silently loses to the generated value.
# Matched as one ordered pattern rather than two greps, which would pass with
# the files reversed.
printf '%s\n' "$unit" | grep -q 'ExecStart=.*--env-file /etc/warmbly/node.env .*--env-file /etc/warmbly/node.local.env' \
  || fail "node.local.env must be passed after node.env, or a local override loses to the generated value"
ok "local override env file is passed last"

# With local blobs the root has to be mounted too, and the line must still be
# one line: a multi-line mount list is how the continuation collapsed before.
unit=$(NODE_ENV="$FS_BLOB_ENV" sh "$SCRIPT" --print-unit) || fail "--print-unit with blobs failed"
printf '%s\n' "$unit" | grep -q 'ExecStart=.*-v /var/lib/warmbly/blobs:/var/lib/warmbly/blobs' \
  || fail "BLOB_FS_ROOT must be mounted (the fs alias counts as filesystem)"
[ "$(printf '%s\n' "$unit" | grep -c '^ExecStart=')" = "1" ] \
  || fail "ExecStart must stay one line when a blob mount is added"
ok "rendered unit (fs alias + blob mount)"

# A relative root is refused rather than rendered into a mount docker rejects.
if NODE_ENV="BLOB_PROVIDER=filesystem
BLOB_FS_ROOT=data/blobs" sh "$SCRIPT" --print-unit >/dev/null 2>&1; then
  fail "a relative BLOB_FS_ROOT must be refused, not mounted"
fi
ok "relative BLOB_FS_ROOT refused"

# NO EnvironmentFile may point into the node-writable mount. Asserting only
# that the right one exists is not enough: an extra one under AGENT_DIR would
# let the container choose the image root's `docker run --network host` runs.
# Checked in every render, not just the default one. EnvironmentFile does not
# vary with NODE_ENV today, but the point of this assertion is what someone
# changes tomorrow, and "today it is redundant" is exactly the reasoning that
# already dropped this guard once. One extra subshell is a fair price.
for variant_env in "" "$FS_BLOB_ENV"; do
  v_unit=$(NODE_ENV="$variant_env" sh "$SCRIPT" --print-unit) || fail "--print-unit failed"
  if printf '%s\n' "$v_unit" | grep '^EnvironmentFile=' | grep -q '/var/lib/warmbly/node'; then
    fail "an EnvironmentFile points into the node-writable mount; the node could choose the image root runs"
  fi
done
ok "no EnvironmentFile is node-writable (every render)"

# --dry-run prints the config a node would receive, which now includes a
# database DSN. Run the real function against the shapes a node is actually
# sent, rather than reading the expression: the first version of it used a BRE
# alternation, which is a GNU extension, so on any other sed it matched nothing
# and printed every secret in clear while looking correct in review.
redact_fn=$(awk '/^redact\(\) \{/ { inside = 1 } inside { print } inside && $0 == "}" { exit }' "$SCRIPT")
[ -n "$redact_fn" ] || fail "function redact() not found in $SCRIPT"

redacted=$(printf '%s\n' "$redact_fn" > /tmp/warmbly-redact.$$ && \
  printf '%s\n' \
    'PRIMARY_DB=postgres://warmbly:dbsecret@db.example.com:5432/warmbly' \
    'NATS_URL=tls://bussecret@bus.example.com:4222' \
    'REDIS=rediss://:cachesecret@bus.example.com:6380' \
    'INTERNAL_API_TOKEN=tokensecret' \
    'NODE_BROKER_TOKEN=brokersecret' \
    'CREDENTIALS_ENCRYPTION_KEY=keysecret' \
    'BOX_GOOGLE_CLIENT_SECRET=oauthsecret' \
    'ENCRYPTED_KEYS_BACKEND_URL=https://api.example.com' \
  | sh -c ". /tmp/warmbly-redact.$$; redact")
rm -f "/tmp/warmbly-redact.$$"

for leaked in dbsecret bussecret cachesecret tokensecret brokersecret keysecret oauthsecret; do
  if printf '%s\n' "$redacted" | grep -q "$leaked"; then
    fail "--dry-run prints $leaked in clear; the redaction does not cover it on this sed"
  fi
done
# The addresses are the reason --dry-run exists, so they have to survive.
printf '%s\n' "$redacted" | grep -q 'db.example.com:5432' \
  || fail "redaction ate the database host; only the credential should go"
printf '%s\n' "$redacted" | grep -q '^ENCRYPTED_KEYS_BACKEND_URL=https://api.example.com$' \
  || fail "redaction masked an address that carries no credential"
ok "--dry-run masks every credential and keeps the addresses"

# Two invariants that leave no trace in the rendered unit and so cannot be
# caught above: both were real defects, so they are asserted at their call
# sites. Comments are stripped and the call is matched in command position, so
# a commented-out call fails while reformatting does not.
# body_of prints one function's body. Tolerant about the definition's spacing,
# and loud when the function is not found: an empty body would otherwise fail
# every assertion below with a message about the wrong thing.
body_of() {
  out=$(awk -v fn="$1" '
    $0 ~ "^" fn "[ \t]*\\([ \t]*\\)[ \t]*{" { inside = 1 }
    inside { print }
    inside && $0 == "}" { exit }' "$SCRIPT")
  [ -n "$out" ] || fail "function $1() not found in $SCRIPT"
  printf '%s\n' "$out"
}

# Match the call lines themselves, not any line mentioning the word: "enrol"
# also appears inside the word "enrolment" in a comment.
# Captured before asserting: body_of fails when the function is missing, and a
# pipeline would run it in a subshell where that failure is lost and the
# misleading assertion message wins.
main_body=$(body_of main)
printf '%s\n' "$main_body" | awk '
  $1 == "enrol"              { e = NR }
  $1 == "validate_blob_root" { v = NR }
  $1 == "write_config"       { w = NR }
  END { exit !(e && v && w && e < v && v < w) }' \
  || fail "main must call validate_blob_root between enrol and write_config"
ok "config is validated before anything is written"

# Field match on the first word, which cannot be fooled by the name appearing
# inside a string or a comment. That does mean the call has to stay a
# standalone statement; join.sh says so at the call site. A looser regex was
# tried and was satisfied by `warn "... ensure_blob_root ..."`, which is a much
# worse failure than a reformat that reports itself clearly.
install_body=$(body_of install_units)
printf '%s\n' "$install_body" | awk '$1 == "ensure_blob_root" { found = 1 } END { exit !found }' \
  || fail "install_units must call ensure_blob_root as a standalone statement, or a filesystem-blob node restart-loops"
ok "blob root is prepared before the unit is installed"

# The unit names node.local.env, so a join that does not create it leaves the
# service unable to start at all: docker refuses a missing --env-file.
write_body=$(body_of write_config)
printf '%s\n' "$write_body" | awk '$1 == "ensure_local_env" { found = 1 } END { exit !found }' \
  || fail "write_config must call ensure_local_env as a standalone statement; the unit names the file and docker refuses a missing --env-file"
ok "local override env file is created on join"

# The whole point of the file is that a re-join keeps it. A creation path that
# can truncate would discard the credential an operator put there, which is
# both silent and unrecoverable.
# Matched on the shape of the invariant rather than one spelling of it: some
# existence test naming the file, before the line that writes it. `if [ -f ]`,
# `if ! test -f` and an AND-OR all satisfy this; only dropping the guard does
# not.
local_body=$(body_of ensure_local_env)
printf '%s\n' "$local_body" | awk '
  /-f .*node\.local\.env/                  { guard = NR }
  /> *"?\$CONFIG_DIR\/node\.local\.env"?/ { write = NR }
  END { exit !(guard && write && guard < write) }' \
  || fail "ensure_local_env must test for an existing file before writing one, or a re-join truncates the operator's file"
ok "an existing local override file is never truncated"

join_fixture=$(mktemp -d)
trap 'rm -rf "$join_fixture"' EXIT HUP INT TERM
sed '$d' "$SCRIPT" > "$join_fixture/functions.sh"
cat > "$join_fixture/enrol.sh" <<'FIXTURE'
#!/bin/sh
set -eu
. "$JOIN_FUNCTIONS"
CONFIG_DIR="$JOIN_CONFIG"
WARMBLY_URL="https://fixture.invalid"
WARMBLY_TOKEN="fixture-join-token"
WARMBLY_ROLE="worker"
DRY_RUN="${JOIN_DRY_RUN:-false}"
curl() {
  response=""; payload=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -o) response="$2"; shift 2 ;;
      -d) payload="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  if [ "$DRY_RUN" = "false" ]; then
    [ -f "$CONFIG_DIR/node.pending-id" ] || [ -f "$CONFIG_DIR/node.env" ] || exit 8
  fi
  request_id=$(printf '%s\n' "$payload" | json_field node_id)
  printf '%s\n' "$request_id" >> "$JOIN_REQUESTS"
  case "$JOIN_RESULT" in
    failure) printf '{"message":"fixture enrollment unavailable"}' > "$response"; printf 503; return 0 ;;
    lost) return 7 ;;
    mismatch) reply_id="22222222-2222-4222-8222-222222222222" ;;
    *) reply_id="${request_id:-11111111-1111-4111-8111-111111111111}" ;;
  esac
  env_b64=$(printf 'WARMBLY_NODE_ID=%s\nNODE_LOG_TOKEN=fixture\n' "$reply_id" | base64 | tr -d '\n')
  printf '{"node_id":"%s","env_b64":"%s","desired_version":"v0.6.44"}' "$reply_id" "$env_b64" > "$response"
  printf 200
}
enrol
FIXTURE
run_enrol() {
  JOIN_FUNCTIONS="$join_fixture/functions.sh" JOIN_CONFIG="$join_fixture/config" \
    JOIN_REQUESTS="$join_fixture/requests" JOIN_RESULT="$1" JOIN_DRY_RUN="${2:-false}" \
    sh "$join_fixture/enrol.sh" >/dev/null 2>&1
}
if run_enrol failure; then fail "enrollment failure must not succeed"; fi
pending_id=$(cat "$join_fixture/config/node.pending-id")
[ -n "$pending_id" ] || fail "first failed POST did not persist an identity"
if run_enrol lost; then fail "lost reply must not succeed"; fi
run_enrol success || fail "retry after failed/lost reply failed"
[ "$(sort -u "$join_fixture/requests" | wc -l | tr -d ' ')" = "1" ] \
  || fail "retry after enrollment failure/lost reply changed the UUID"
[ ! -f "$join_fixture/config/node.env" ] || fail "enrol must not write managed config itself"
if run_enrol mismatch; then fail "a response with a different identity was accepted"; fi
[ "$(cat "$join_fixture/config/node.pending-id")" = "$pending_id" ] \
  || fail "rejected response replaced pending identity"
ok "first-join identity persists before POST and survives failed/lost/mismatched replies"

printf 'WARMBLY_NODE_ID=33333333-3333-4333-8333-333333333333\n' > "$join_fixture/config/node.env"
run_enrol success || fail "existing node UUID retry failed"
[ "$(tail -1 "$join_fixture/requests")" = "33333333-3333-4333-8333-333333333333" ] \
  || fail "pending identity overrode the existing managed identity"
ok "existing node.env identity wins over pending identity"

rm -rf "$join_fixture/config"
rm -f "$join_fixture/requests"
run_enrol success & join_one=$!
run_enrol success & join_two=$!
wait "$join_one" || fail "first concurrent initialization failed"
wait "$join_two" || fail "second concurrent initialization failed"
[ "$(sort -u "$join_fixture/requests" | wc -l | tr -d ' ')" = "1" ] \
  || fail "concurrent initialization produced different node identities"
ok "concurrent initialization locks and reuses a single pending UUID"

rm -rf "$join_fixture/config"
run_enrol success true || fail "dry-run fixture failed"
[ ! -e "$join_fixture/config" ] || fail "dry-run persisted an identity or lock"
ok "dry-run leaves no pending identity or lock"

printf 'check-join-script: all checks passed\n'
