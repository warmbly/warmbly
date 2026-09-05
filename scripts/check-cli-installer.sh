#!/usr/bin/env bash
#
# Checks the CLI installer served at https://warmbly.com/cli.sh, and its
# Windows counterpart at /cli.ps1.
#
# Both are served verbatim out of site/public, so this runs against the exact
# bytes a `curl -fsSL https://warmbly.com/cli.sh | sh` executes:
#
#   * it parses as POSIX sh, in dash and not only in bash
#   * shellcheck has nothing to say about it
#   * --help and --dry-run work with no network and no terminal, because that
#     is how someone reads it before trusting it, and --dry-run writes nothing
#   * a real install against a local mirror produces a working binary, puts it
#     on PATH and writes completions
#   * a tampered checksum stops the install rather than warning about it
#   * --uninstall removes exactly what it wrote and nothing else
#   * the platforms it will download match the ones we actually build
#   * the published checksum matches, so "download, verify, read, run" verifies
set -euo pipefail

cd "$(dirname "$0")/.."
SCRIPT=site/public/cli.sh
PS_SCRIPT=site/public/cli.ps1
SUMFILE=site/public/cli.sh.sha256

fail() { printf '\n\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }
pass() { printf '\033[32m✓\033[0m %s\n' "$*"; }

[[ -f $SCRIPT ]] || fail "$SCRIPT is missing"
[[ -f $PS_SCRIPT ]] || fail "$PS_SCRIPT is missing"

# The script is executed by whatever /bin/sh is on the machine, which on Debian
# and Ubuntu is dash. Checking with bash alone would let a bashism through to
# exactly the hosts this is aimed at.
if command -v dash >/dev/null 2>&1; then
  dash -n "$SCRIPT" || fail "the installer is not valid POSIX sh (dash -n)"
  pass "parses as POSIX sh"
else
  sh -n "$SCRIPT" || fail "the installer does not parse"
  pass "parses (dash not installed; POSIX check was approximate)"
fi

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

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# --dry-run reaches its end with no network and, above all, writes nothing.
HOME="$work/dryhome" SHELL=/bin/bash sh "$SCRIPT" --dry-run --no-color --dir "$work/dryhome/bin" >/dev/null 2>&1 \
  || fail "--dry-run failed"
[[ ! -e "$work/dryhome" ]] || fail "--dry-run created $work/dryhome; it must write nothing"
pass "--dry-run runs and writes nothing"

# Every platform the installer will ask for has to be one we build, and the
# other way round. A mismatch is a 404 for whoever runs it on that machine.
script_platforms=$(sed -n 's/^PLATFORMS="\(.*\)"$/\1/p' "$SCRIPT" | tr ' ' '\n' | sort)
build_platforms=$(sed -n 's/^PLATFORMS="\(.*\)"$/\1/p' scripts/build-cli.sh | tr ' ' '\n' | sed 's|/|_|' | grep -v '^windows' | sort)
if [[ "$script_platforms" != "$build_platforms" ]]; then
  fail "cli.sh and scripts/build-cli.sh disagree about platforms:
installer builds for:
$script_platforms
release builds:
$build_platforms"
fi
pass "installer and release agree on platforms"

# ─────────────────────────────────────────────────────────────────────────
# A real install, against a mirror on disk. file:// keeps this offline, which
# is what lets it run in CI without reaching GitHub.
# ─────────────────────────────────────────────────────────────────────────

mirror="$work/mirror"
mkdir -p "$mirror" "$work/stage/completions"

# A stand-in for the real binary: the installer only needs something that runs
# and answers `version`, and building the real CLI here would make this check
# a minute slower for nothing.
cat > "$work/stage/warmbly" <<'STUB'
#!/bin/sh
[ "${1:-}" = version ] && echo "warmbly v0.0.0-test (test)" && exit 0
exit 0
STUB
chmod +x "$work/stage/warmbly"
echo "# completions" > "$work/stage/completions/warmbly.bash"
echo "# completions" > "$work/stage/completions/warmbly.zsh"
echo "# completions" > "$work/stage/completions/warmbly.fish"
cp LICENSE "$work/stage/" 2>/dev/null || echo license > "$work/stage/LICENSE"

host_os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64|amd64) host_arch=amd64 ;;
  arm64|aarch64) host_arch=arm64 ;;
  *) host_arch=amd64 ;;
esac
asset="warmbly_${host_os}_${host_arch}.tar.gz"
tar -czf "$mirror/$asset" -C "$work/stage" .
( cd "$mirror" && sha256sum "$asset" > checksums.txt )

home="$work/home"
mkdir -p "$home"
HOME="$home" SHELL=/bin/bash sh "$SCRIPT" \
  --base-url "file://$mirror" --dir "$home/bin" --no-color >/dev/null 2>&1 \
  || fail "installing from a local mirror failed"

[[ -x "$home/bin/warmbly" ]] || fail "the installer did not produce $home/bin/warmbly"
[[ "$("$home/bin/warmbly" version)" == "warmbly v0.0.0-test (test)" ]] || fail "the installed binary does not run"
pass "installs a working binary from a mirror"

grep -q 'warmbly CLI installer' "$home/.bash_profile" 2>/dev/null || grep -q 'warmbly CLI installer' "$home/.bashrc" 2>/dev/null \
  || fail "the installer did not put the install directory on PATH"
pass "puts the install directory on PATH"

[[ -f "$home/.local/share/bash-completion/completions/warmbly" ]] || fail "no bash completions were written"
pass "writes shell completions"

# A second run must not append the PATH line again.
HOME="$home" SHELL=/bin/bash sh "$SCRIPT" \
  --base-url "file://$mirror" --dir "$home/bin" --no-color >/dev/null 2>&1 \
  || fail "the second install run failed"
occurrences=$(grep -c 'warmbly CLI installer' "$home/.bash_profile" 2>/dev/null || true)
[[ "${occurrences:-0}" -le 1 ]] || fail "re-running appended the PATH line again ($occurrences times)"
pass "re-running is idempotent"

# A tampered archive must stop the install, not warn about it.
bad="$work/badmirror"
mkdir -p "$bad"
cp "$mirror/$asset" "$bad/"
sed 's/^[0-9a-f]\{64\}/0000000000000000000000000000000000000000000000000000000000000000/' \
  "$mirror/checksums.txt" > "$bad/checksums.txt"
badhome="$work/badhome"
if HOME="$badhome" sh "$SCRIPT" --base-url "file://$bad" --dir "$badhome/bin" --no-color >/dev/null 2>&1; then
  fail "a checksum mismatch did not stop the install"
fi
[[ ! -e "$badhome/bin/warmbly" ]] || fail "a checksum mismatch still installed the binary"
pass "refuses to install on a checksum mismatch"

# --uninstall removes what it wrote, and leaves the credentials alone.
mkdir -p "$home/.config/warmbly"
echo "token" > "$home/.config/warmbly/hosts.yml"
HOME="$home" SHELL=/bin/bash sh "$SCRIPT" --uninstall --dir "$home/bin" --no-color >/dev/null 2>&1 \
  || fail "--uninstall failed"
[[ ! -e "$home/bin/warmbly" ]] || fail "--uninstall left the binary behind"
[[ ! -e "$home/.local/share/bash-completion/completions/warmbly" ]] || fail "--uninstall left completions behind"
[[ -f "$home/.config/warmbly/hosts.yml" ]] || fail "--uninstall removed the credentials; it must not"
pass "--uninstall removes the binary and completions, and keeps credentials"

# ─────────────────────────────────────────────────────────────────────────
# The Windows half. Ubuntu runners ship pwsh, so this is a real parse there.
# ─────────────────────────────────────────────────────────────────────────

if command -v pwsh >/dev/null 2>&1; then
  pwsh -NoProfile -Command "
    \$errors = \$null
    [System.Management.Automation.Language.Parser]::ParseFile('$PWD/$PS_SCRIPT', [ref]\$null, [ref]\$errors) | Out-Null
    if (\$errors) { \$errors | ForEach-Object { Write-Host \$_ }; exit 1 }
  " || fail "$PS_SCRIPT does not parse as PowerShell"
  pass "cli.ps1 parses as PowerShell"
else
  echo "· pwsh not installed; skipped the PowerShell parse"
fi

# ─────────────────────────────────────────────────────────────────────────
# The published checksum, which is what makes "download, verify, read, run" a
# real alternative to piping into a shell.
# ─────────────────────────────────────────────────────────────────────────

[[ -f $SUMFILE ]] || fail "$SUMFILE is missing. Run: make cli-sha"
( cd site/public && sha256sum -c "$(basename "$SUMFILE")" >/dev/null ) \
  || fail "$SUMFILE does not match $SCRIPT. Run: make cli-sha"
pass "published checksum matches"

printf '\n\033[32mAll CLI installer checks passed.\033[0m\n'
