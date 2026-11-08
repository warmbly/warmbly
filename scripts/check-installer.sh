#!/usr/bin/env bash
#
# Checks the one-command installer served at https://warmbly.com/install.sh.
#
# It is served verbatim out of site/public, so this runs against the exact
# bytes a `curl -fsSL https://warmbly.com/install.sh | sh` executes:
#
#   * it parses as POSIX sh, in dash and not only in bash
#   * shellcheck has nothing to say about it
#   * --help, --print-env and --dry-run work without a terminal, a docker or a
#     network, because that is how someone reads it before trusting it
#   * every compose file it can generate is one docker compose accepts
#   * the published checksum matches, so the documented
#     "download, verify, read, run" path actually verifies
set -euo pipefail

cd "$(dirname "$0")/.."
SCRIPT=site/public/install.sh
SUMFILE=site/public/install.sh.sha256

fail() { printf '\n\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }
pass() { printf '\033[32m✓\033[0m %s\n' "$*"; }

[[ -f $SCRIPT ]] || fail "$SCRIPT is missing"

# The script is executed by whatever /bin/sh is on the operator's box, which on
# Debian and Ubuntu is dash. Checking it with bash alone would let a bashism
# through to exactly the hosts this is aimed at.
if command -v dash >/dev/null 2>&1; then
  dash -n "$SCRIPT" || fail "the installer is not valid POSIX sh (dash -n)"
  pass "parses as POSIX sh"
else
  sh -n "$SCRIPT" || fail "the installer does not parse"
  pass "parses (dash not installed; POSIX check was approximate)"
fi

# The version is printed because it matters: SC2015 and friends move between
# releases, so a local run that passes on a newer shellcheck than CI's is not
# the same check. When CI disagrees with you, this line is why.
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -s sh "$SCRIPT" || fail "shellcheck found problems in the installer"
  pass "shellcheck clean ($(shellcheck --version | awk '/^version:/ {print $2}'))"
else
  echo "· shellcheck not installed; skipped"
fi

# --help must work before anything is set up, which is where an unbound
# variable under set -u would otherwise hide.
sh "$SCRIPT" --help >/dev/null || fail "--help failed"
pass "--help works"

# --demo has to reach its end with no terminal, no Docker and no network, and
# above all it must not create the install directory it talks about.
demo_dir=$(mktemp -d)/opt-warmbly
sh "$SCRIPT" --demo --no-color --dir "$demo_dir" >/dev/null 2>&1 || fail "--demo failed"
[[ ! -e $demo_dir ]] || fail "--demo created $demo_dir; it must write nothing"
pass "--demo runs and writes nothing"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

sh "$SCRIPT" --print-env --no-color --dir "$work/inst" --version v0.0.0-test >"$work/env" ||
  fail "--print-env failed"
for key in WARMBLY_TAG AUTH_SECRET CREDENTIALS_ENCRYPTION_KEY KMS_LOCAL_MASTER_KEY \
           INTERNAL_API_TOKEN SECRET_KEY_BASE PRIMARY_DB WARMBLY_SETTINGS_BOOTSTRAP; do
  grep -q "^${key}=." "$work/env" || fail "--print-env wrote no $key"
done
grep -q '^WARMBLY_TAG=v0.0.0-test$' "$work/env" || fail "--version was not pinned into .env"
pass "--print-env writes a complete .env"

# A re-run adopts every secret it finds and adds only the ones missing. The
# portal token is the one an install from before the portal lacks.
mkdir -p "$work/rerun"
grep -v '^PORTAL_EDGE_TOKEN=' "$work/env" >"$work/rerun/.env"
sh "$SCRIPT" --print-env --no-color --dir "$work/rerun" >"$work/rerun.env" || fail "--print-env over an existing .env failed"
for key in AUTH_SECRET CREDENTIALS_ENCRYPTION_KEY KMS_LOCAL_MASTER_KEY INTERNAL_API_TOKEN SECRET_KEY_BASE; do
  [[ "$(grep "^${key}=" "$work/env")" == "$(grep "^${key}=" "$work/rerun.env")" ]] || fail "a re-run changed $key"
done
token=$(sed -n 's/^PORTAL_EDGE_TOKEN=//p' "$work/rerun.env")
(( ${#token} >= 32 )) || fail "a re-run over an .env without PORTAL_EDGE_TOKEN did not add one"
cp "$work/rerun.env" "$work/rerun/.env"
sh "$SCRIPT" --print-env --no-color --dir "$work/rerun" >"$work/rerun2.env" || fail "a second re-run failed"
[[ "$(sed -n 's/^PORTAL_EDGE_TOKEN=//p' "$work/rerun2.env")" == "$token" ]] || fail "a re-run regenerated PORTAL_EDGE_TOKEN"
pass "a re-run keeps every secret and adds a missing portal token once"

# Every shape of answer has to produce a compose file compose accepts. These
# are the four that change the file's structure rather than its values.
check_shape() {
  local label=$1; shift
  local dir="$work/shape"
  rm -rf "$dir"; mkdir -p "$dir"
  sh "$SCRIPT" --dry-run --no-color --dir "$dir/inst" --version v0.0.0-test "$@" >"$dir/out" ||
    fail "--dry-run failed for: $label"
  python3 - "$dir" <<'PY'
import sys, os, re
d = sys.argv[1]
lines = open(os.path.join(d, "out")).read().split("\n")
def extract(marker, out):
    idx = [i for i, l in enumerate(lines) if l.strip().startswith("── ") and marker in l]
    if not idx:
        return
    body = []
    for l in lines[idx[0] + 1:]:
        if l.strip().startswith("── ") and "(mode" in l:
            break
        body.append(l[2:] if l.startswith("  ") else l)
    while body and (body[-1].strip() == "" or body[-1][:1] in "╭│╰"):
        body.pop()
    open(os.path.join(d, out), "w").write("\n".join(body) + "\n")
extract("/.env", ".env")
extract("docker-compose.yml", "docker-compose.yml")
extract("Caddyfile", "Caddyfile")
PY
  [[ -f "$dir/docker-compose.yml" ]] || fail "--dry-run printed no compose file for: $label"
  # The portal ships with tracking and forms, and the backend only builds links on it with PORTAL_DOMAIN set.
  if grep -q '^  tracking:$' "$dir/docker-compose.yml"; then
    grep -q '^  portal:$' "$dir/docker-compose.yml" || fail "no portal service for: $label"
    grep -q '^PORTAL_DOMAIN=.' "$dir/.env" || fail "no PORTAL_DOMAIN for: $label"
  else
    ! grep -q '^  portal:$' "$dir/docker-compose.yml" || fail "a portal service without tracking for: $label"
  fi
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ( cd "$dir" && docker compose config >/dev/null ) || fail "invalid compose file for: $label"
  fi
  pass "generates a valid stack: $label"
}

check_shape "defaults"
check_shape "bundled TLS" --tls caddy --host warmbly.example.com
check_shape "core only" --components core
check_shape "named volumes" --data-root volumes
check_shape "external stores" --database-url postgres://u:p@db:5432/w --redis-url redis://cache:6379 --blobs s3

# A workspace can point its own tracking or forms domain at an instance long
# after it was installed, and the Caddyfile cannot name a host it has never
# heard of. Without on_demand_tls behind an ask endpoint, every tracked link and
# every opt-out link on such a domain fails TLS (issue #400). Asserted on the
# rendered file, and where Caddy is available on what Caddy makes of it, because
# a Caddyfile that parses can still carry no on-demand policy at all.
check_caddy() {
  local label=$1; shift
  local want_catch_all=$1; shift
  local dir="$work/caddy"
  rm -rf "$dir"; mkdir -p "$dir"
  sh "$SCRIPT" --dry-run --no-color --dir "$dir/inst" --version v0.0.0-test \
    --tls caddy --host warmbly.example.com "$@" >"$dir/out" ||
    fail "--dry-run failed for Caddy shape: $label"
  python3 - "$dir" <<'PY'
import sys, os
d = sys.argv[1]
lines = open(os.path.join(d, "out")).read().split("\n")
idx = [i for i, l in enumerate(lines) if l.strip().startswith("── ") and "Caddyfile" in l]
body = []
if idx:
    for l in lines[idx[0] + 1:]:
        if l.strip().startswith("── ") and "(mode" in l:
            break
        body.append(l[2:] if l.startswith("  ") else l)
    while body and (body[-1].strip() == "" or body[-1][:1] in "╭│╰"):
        body.pop()
open(os.path.join(d, "Caddyfile"), "w").write("\n".join(body) + "\n")
PY
  [[ -s "$dir/Caddyfile" ]] || fail "--tls caddy printed no Caddyfile for: $label"

  grep -q 'ask http://backend:8080/tls/authorize' "$dir/Caddyfile" ||
    fail "the Caddyfile has no on-demand ask endpoint for: $label"

  if [[ "$want_catch_all" == yes ]]; then
    grep -q '^https:// {' "$dir/Caddyfile" ||
      fail "the Caddyfile has no custom-domain catch-all for: $label"
    grep -q 'on_demand' "$dir/Caddyfile" ||
      fail "the custom-domain catch-all does not enable on-demand TLS for: $label"
  else
    ! grep -q '^https:// {' "$dir/Caddyfile" ||
      fail "the Caddyfile serves a catch-all with nothing behind it for: $label"
  fi

  # What the file says and what Caddy does with it are different questions. The
  # adapted config is where a catch-all that quietly shadows the named hosts, or
  # an on_demand block that produced no automation policy, becomes visible.
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    docker run --rm -v "$dir:/w" -w /w caddy:2-alpine \
      caddy adapt --config /w/Caddyfile >"$dir/adapted.json" 2>/dev/null ||
      fail "Caddy rejected the generated Caddyfile for: $label"
    python3 - "$dir/adapted.json" "$want_catch_all" <<'PY' || fail "the adapted Caddy config is wrong for: $label"
import json, sys
cfg = json.load(open(sys.argv[1]))
want_catch_all = sys.argv[2] == "yes"
# Two things are deliberately NOT asserted here, because the Caddyfile
# adapter normalizes both and no input can make them fail: it always sorts
# named-host routes ahead of a catch-all whatever order they were written in,
# and it always folds an on_demand site into one policy with no subjects. Both
# were written, mutation-tested, and found to be tautologies. A check that
# cannot fail is worse than no check, so they are gone.
routes = cfg["apps"]["http"]["servers"]["srv0"]["routes"]
named = [r for r in routes if r.get("match")]
catch = [r for r in routes if not r.get("match")]
if not named:
    sys.exit("no named host routes survived")
if want_catch_all:
    if len(catch) != 1:
        sys.exit(f"expected exactly one catch-all route, got {len(catch)}")
elif catch:
    sys.exit("a catch-all route exists with no custom-domain services enabled")

policies = cfg["apps"]["tls"]["automation"]["policies"]
if want_catch_all and not [p for p in policies if p.get("on_demand")]:
    sys.exit("no automation policy enables on-demand issuance")
perm = cfg["apps"]["tls"]["automation"].get("on_demand", {}).get("permission", {})
if perm.get("endpoint") != "http://backend:8080/tls/authorize":
    sys.exit(f"on-demand issuance is not gated on the ask endpoint: {perm!r}")
PY
  fi
  pass "custom domains can get a certificate: $label"
}

check_caddy "tracking and forms" yes
check_caddy "core only" no --components core

# Nothing drawn inside a redraw loop may be wider than the terminal. A wrapped
# line is two physical rows, every cursor-up counts logical ones, and the menu
# then draws over itself and over whatever was on screen before it. This is the
# regression that check exists for.
if command -v python3 >/dev/null 2>&1; then
  python3 - "$SCRIPT" <<'PYEOF' || fail "the installer drew past the terminal width"
import fcntl, os, pty, re, select, struct, sys, termios, time

script = sys.argv[1]
failures = []
for cols in (80, 100):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 45, cols, 0, 0))
    pid = os.fork()
    if pid == 0:
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
        os.dup2(slave, 0); os.dup2(slave, 1); os.dup2(slave, 2)
        os.close(master); os.close(slave)
        os.environ["TERM"] = "xterm-256color"
        os.environ["WARMBLY_DEMO_FAST"] = "1"
        os.execvp("sh", ["sh", script, "--demo"])
        os._exit(1)
    os.close(slave)
    buf = b""
    start = last = time.time()
    sent = 0
    while time.time() - start < 90:
        r, _, _ = select.select([master], [], [], 0.25)
        if r:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            buf += chunk
            last = time.time()
        elif time.time() - last > 0.4 and sent < 80:
            tail = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]", "", buf.decode("utf-8", "replace"))[-400:]
            os.write(master, b"copied\r" if "Type 'copied'" in tail else b"\r")
            sent += 1
            last = time.time()
        if b"That was the demo" in buf:
            break
    for fn in (lambda: os.close(master), lambda: os.waitpid(pid, 0)):
        try:
            fn()
        except OSError:
            pass
    text = buf.decode("utf-8", "replace")
    if "command not found" in text or "syntax error" in text:
        failures.append(f"{cols} columns: the run produced shell errors")
    if "That was the demo" not in text:
        failures.append(f"{cols} columns: the demo did not reach its end")
    plain = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]", "", text)
    over = [l for l in plain.replace("\r", "\n").split("\n") if len(l) > cols]
    if over:
        failures.append(f"{cols} columns: {len(over)} line(s) too wide, first: {over[0][:cols + 20]!r}")

for f in failures:
    print("   " + f, file=sys.stderr)
sys.exit(1 if failures else 0)
PYEOF
  pass "draws inside the terminal at 80 and 100 columns"
else
  echo "· python3 not installed; the width check was skipped"
fi

# A registry that will not serve an image answers "unauthorized", and
# reporting that as a missing tag is what sent the first person who hit it
# looking in entirely the wrong place (#371). Both branches of the diagnosis
# are checked, against the literal text docker produces.
diag() {
  local log=$1
  {
    sed -n '/^pull_failed()/,/^}/p' "$SCRIPT"
    cat <<'STUB'
show_log() { :; }
fail_with() { printf '%s\n' "$@"; exit 1; }
REGISTRY=ghcr.io/warmbly/warmbly
RESOLVED_TAG=v0.0.0-test
REPO=warmbly/warmbly
pull_failed
STUB
  } | LOGFILE="$log" sh || true
}

printf 'Error response from daemon: Head "https://ghcr.io/v2/warmbly/warmbly/forms/manifests/v0.4.0": unauthorized\n' >"$work/log.unauth"
printf 'Error response from daemon: manifest unknown\n' >"$work/log.missing"
printf 'Error response from daemon: denied\n' >"$work/log.denied"

diag "$work/log.unauth" | grep -q 'refused to serve' ||
  fail "an unauthorized pull is not diagnosed as a registry refusal"
diag "$work/log.unauth" | grep -q 'probably fine' ||
  fail "an unauthorized pull still blames the tag"
diag "$work/log.unauth" | grep -q 'check it for a typo' ||
  fail "an unauthorized pull does not mention a mistyped --registry, which"
diag "$work/log.missing" | grep -q 'may not exist' ||
  fail "an ordinary pull failure lost its generic message"
if diag "$work/log.missing" | grep -q 'refused to serve'; then
  fail "an ordinary pull failure is misreported as a registry refusal"
fi
diag "$work/log.denied" | grep -q 'check it for a typo' ||
  fail "a bare denied does not offer the mistyped-registry reading"
pass "diagnoses an unauthorized pull separately from a missing tag"

# The checksum is the whole answer to "why would I pipe this into a shell", so
# a stale one is a failure, not a warning.
if [[ ! -f $SUMFILE ]]; then
  fail "$SUMFILE is missing. Regenerate it with: make installer-sha"
fi
expected=$(awk '{print $1}' "$SUMFILE")
actual=$(sha256sum "$SCRIPT" | awk '{print $1}')
if [[ $expected != "$actual" ]]; then
  fail "$SUMFILE is stale.
    published $expected
    actual    $actual
  Regenerate it with: make installer-sha"
fi
pass "published checksum matches"

printf '\n\033[32mThe installer is good.\033[0m\n'
