#!/bin/sh
#
#   curl -fsSL https://warmbly.com/cli.sh | sh
#
# Installs the `warmbly` CLI on this machine: one static binary, no Go
# toolchain, no package manager, no root. It downloads the archive for your
# platform from the GitHub release, checks it against the published checksum,
# and puts the binary somewhere on your PATH.
#
#   sh cli.sh --help          every flag, and the environment variable for each
#   sh cli.sh --dry-run       print exactly what it would do, touch nothing
#   sh cli.sh --uninstall     remove the binary and the completions it wrote
#
# What it does, in full:
#
#   * detects your OS and CPU, and stops with a real message if we publish no
#     build for it rather than downloading something that cannot run
#   * resolves the newest release (or the one you pin with --version)
#   * downloads warmbly_<os>_<arch>.tar.gz and checksums.txt, and REFUSES to
#     install if the two disagree
#   * installs to ~/.local/bin by default, which needs no sudo. Nothing else on
#     your system is touched
#   * writes shell completions, and tells you the one line to add to your shell
#     profile if the install directory is not already on PATH
#
# Re-running it upgrades in place and says so when there is nothing to do.
#
# Verify before running, if you would rather:
#
#   curl -fsSLO https://warmbly.com/cli.sh
#   curl -fsSLO https://warmbly.com/cli.sh.sha256
#   sha256sum -c cli.sh.sha256
#   less cli.sh && sh cli.sh
#
# https://docs.warmbly.com/api/cli/

set -eu

# ─────────────────────────────────────────────────────────────────────────
# Constants
# ─────────────────────────────────────────────────────────────────────────

REPO="warmbly/warmbly"
BIN="warmbly"
DOCS="https://docs.warmbly.com/api/cli/"
RELEASES="https://github.com/${REPO}/releases"

# Every platform scripts/build-cli.sh publishes. The two lists have to agree:
# a platform here with no archive downloads a 404, and one missing here is a
# build nobody can install.
PLATFORMS="darwin_amd64 darwin_arm64 linux_amd64 linux_arm64"

# ─────────────────────────────────────────────────────────────────────────
# Options. Every one is also an environment variable, so the same install runs
# from Ansible, cloud-init, a Dockerfile or an agent with no keyboard.
# ─────────────────────────────────────────────────────────────────────────

DIR=${WARMBLY_INSTALL_DIR:-}
VERSION=${WARMBLY_CLI_VERSION:-}
# Where the archives come from. Overridable so an air-gapped or
# egress-restricted network can mirror the release assets internally and still
# use this exact script.
BASE_URL=${WARMBLY_CLI_BASE_URL:-}
NO_MODIFY_PATH=${WARMBLY_NO_MODIFY_PATH:-}
NO_COMPLETIONS=${WARMBLY_NO_COMPLETIONS:-}
DRY_RUN=""
UNINSTALL=""
FORCE=""
QUIET=""
USE_COLOR=1

# ─────────────────────────────────────────────────────────────────────────
# Output
#
# The colour variables are defined empty here rather than only in
# setup_colors, because parse_args runs first and can call die: under `set -u`
# an unset C_RED turns a "unknown option" message into an unbound-variable
# error, which is what a mistyped flag would have printed.
# ─────────────────────────────────────────────────────────────────────────

C_RESET=""; C_DIM=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_CYAN=""

setup_colors() {
    if [ -n "$USE_COLOR" ] && [ -t 2 ] && [ "${TERM:-dumb}" != "dumb" ] && [ -z "${NO_COLOR:-}" ]; then
        C_RESET=$(printf '\033[0m')
        C_DIM=$(printf '\033[2m')
        C_BOLD=$(printf '\033[1m')
        C_RED=$(printf '\033[31m')
        C_GREEN=$(printf '\033[32m')
        C_YELLOW=$(printf '\033[33m')
        C_CYAN=$(printf '\033[36m')
    else
        C_RESET=""; C_DIM=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_CYAN=""
    fi
}

say()  { [ -n "$QUIET" ] || printf '%s\n' "$*" >&2; }
step() { [ -n "$QUIET" ] || printf '%s>%s %s\n' "$C_CYAN" "$C_RESET" "$*" >&2; }
ok()   { [ -n "$QUIET" ] || printf '%s✓%s %s\n' "$C_GREEN" "$C_RESET" "$*" >&2; }
warn() { printf '%s!%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
die()  { printf '%s✗%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 1; }

usage() {
    cat <<EOF
Install the warmbly CLI.

Usage:
  curl -fsSL https://warmbly.com/cli.sh | sh
  curl -fsSL https://warmbly.com/cli.sh | sh -s -- [flags]

Flags:
  --dir PATH          Where to install the binary        (WARMBLY_INSTALL_DIR)
                      Default: \$HOME/.local/bin
  --version VERSION   Install a specific release tag     (WARMBLY_CLI_VERSION)
                      Default: the newest release
  --base-url URL      Download from a mirror of the release
                      assets instead of GitHub             (WARMBLY_CLI_BASE_URL)
  --no-modify-path    Never touch a shell profile        (WARMBLY_NO_MODIFY_PATH)
  --no-completions    Do not write shell completions     (WARMBLY_NO_COMPLETIONS)
  --force             Reinstall even if the version already matches
  --uninstall         Remove the binary and its completions
  --dry-run           Print what would happen, change nothing
  --quiet             Only errors
  --no-color          Never colourise output
  --help              This

Examples:
  curl -fsSL https://warmbly.com/cli.sh | sh
  curl -fsSL https://warmbly.com/cli.sh | sh -s -- --dir /usr/local/bin
  curl -fsSL https://warmbly.com/cli.sh | sh -s -- --version v1.4.0
  curl -fsSL https://warmbly.com/cli.sh | sh -s -- --uninstall

After installing, sign in:

  warmbly auth login

Docs: ${DOCS}
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case $1 in
            --dir) shift; [ $# -gt 0 ] || die "--dir needs a path"; DIR=$1 ;;
            --dir=*) DIR=${1#*=} ;;
            --version) shift; [ $# -gt 0 ] || die "--version needs a release tag"; VERSION=$1 ;;
            --version=*) VERSION=${1#*=} ;;
            --base-url) shift; [ $# -gt 0 ] || die "--base-url needs a URL"; BASE_URL=$1 ;;
            --base-url=*) BASE_URL=${1#*=} ;;
            --no-modify-path) NO_MODIFY_PATH=1 ;;
            --no-completions) NO_COMPLETIONS=1 ;;
            --force) FORCE=1 ;;
            --uninstall) UNINSTALL=1 ;;
            --dry-run) DRY_RUN=1 ;;
            --quiet|-q) QUIET=1 ;;
            --no-color) USE_COLOR="" ;;
            --help|-h) usage; exit 0 ;;
            *) usage >&2; die "unknown option $1" ;;
        esac
        shift
    done
}

# ─────────────────────────────────────────────────────────────────────────
# Platform
# ─────────────────────────────────────────────────────────────────────────

detect_platform() {
    os=$(uname -s 2>/dev/null || echo unknown)
    arch=$(uname -m 2>/dev/null || echo unknown)

    case $os in
        Linux)  OS=linux ;;
        Darwin) OS=darwin ;;
        MINGW*|MSYS*|CYGWIN*)
            die "this script installs the Unix build.
On Windows run this in PowerShell instead:
  irm https://warmbly.com/cli.ps1 | iex" ;;
        *) die "no published build for $os. Build from source with: go install github.com/${REPO}/cmd/cli@latest" ;;
    esac

    case $arch in
        x86_64|amd64) ARCH=amd64 ;;
        arm64|aarch64) ARCH=arm64 ;;
        *) die "no published build for $arch on $OS.
We publish amd64 and arm64. Build from source with:
  go install github.com/${REPO}/cmd/cli@latest" ;;
    esac

    TARGET="${OS}_${ARCH}"
    for known in $PLATFORMS; do
        if [ "$known" = "$TARGET" ]; then
            return 0
        fi
    done
    die "no published build for $TARGET"
}

# fetch writes a URL to a file. curl and wget are both accepted because a
# minimal container image has exactly one of them and it is never the one you
# assumed.
fetch() {
    url=$1
    dest=$2
    if [ -n "$DOWNLOADER" ] && [ "$DOWNLOADER" = curl ]; then
        curl -fsSL --retry 3 --retry-delay 1 -o "$dest" "$url"
    else
        wget -q -O "$dest" "$url"
    fi
}

require_downloader() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOADER=curl
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOADER=wget
    else
        die "neither curl nor wget is installed, so there is nothing to download with"
    fi
}

# sha256_of prints a file's checksum with whichever tool the host has. macOS
# ships shasum, Linux ships sha256sum, Alpine ships both or neither.
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    elif command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    else
        echo ""
    fi
}

# ─────────────────────────────────────────────────────────────────────────
# Install directory
# ─────────────────────────────────────────────────────────────────────────

# A literal tilde is what this matches: someone who typed --dir '~/bin' inside
# quotes meant their home directory, not a folder called "~".
# shellcheck disable=SC2088
expand_tilde() {
    case $DIR in
        "~/"*) DIR="${HOME}/${DIR#\~/}" ;;
    esac
}

# resolve_dir picks where the binary goes. ~/.local/bin is the default because
# it needs no sudo and is on PATH by default on most modern distributions;
# piping an installer into a shell should never need root.
resolve_dir() {
    if [ -z "$DIR" ]; then
        DIR="${HOME:-/root}/.local/bin"
    fi
    expand_tilde
}

# on_path answers whether DIR is already searched, so we only talk about shell
# profiles when there is a real problem to solve.
on_path() {
    case ":${PATH}:" in
        *":${DIR}:"*) return 0 ;;
        *) return 1 ;;
    esac
}

# profile_file is the file a login shell reads, chosen from $SHELL rather than
# the shell running this script: this runs under sh no matter what the person
# actually uses.
profile_file() {
    shell_name=$(basename "${SHELL:-sh}")
    case $shell_name in
        zsh)  printf '%s' "${ZDOTDIR:-$HOME}/.zshrc" ;;
        bash)
            if [ -f "$HOME/.bashrc" ]; then
                printf '%s' "$HOME/.bashrc"
            else
                printf '%s' "$HOME/.bash_profile"
            fi ;;
        fish) printf '%s' "$HOME/.config/fish/config.fish" ;;
        *)    printf '%s' "$HOME/.profile" ;;
    esac
}

# The single quotes below are the point: $PATH has to reach the profile
# unexpanded, so it still resolves every time the shell reads it.
# shellcheck disable=SC2016
path_line() {
    shell_name=$(basename "${SHELL:-sh}")
    case $shell_name in
        fish) printf 'fish_add_path %s' "$DIR" ;;
        *)    printf 'export PATH="%s:$PATH"' "$DIR" ;;
    esac
}

# ensure_on_path appends the PATH line to the right profile, once. The marker
# comment is what makes a second run a no-op instead of a growing file.
ensure_on_path() {
    if on_path; then
        return 0
    fi
    line=$(path_line)
    if [ -n "$NO_MODIFY_PATH" ]; then
        warn "$DIR is not on your PATH. Add this yourself:"
        say "    $line"
        return 0
    fi

    profile=$(profile_file)
    if [ -n "$DRY_RUN" ]; then
        say "    would add to $profile: $line"
        return 0
    fi

    if [ -f "$profile" ] && grep -q "warmbly CLI" "$profile" 2>/dev/null; then
        ok "$profile already has the PATH line"
    else
        mkdir -p "$(dirname "$profile")"
        {
            printf '\n# Added by the warmbly CLI installer\n'
            printf '%s\n' "$line"
        } >> "$profile"
        ok "added $DIR to your PATH in $profile"
    fi
    warn "open a new terminal, or run: $line"
}

# ─────────────────────────────────────────────────────────────────────────
# Completions
# ─────────────────────────────────────────────────────────────────────────

# completion_dir is where the shell looks without any configuration. When there
# is no such place we say nothing rather than writing a file that is never read.
completion_dir() {
    shell_name=$(basename "${SHELL:-sh}")
    case $shell_name in
        bash)
            if [ -d "$HOME/.local/share/bash-completion/completions" ] || [ "$1" = create ]; then
                printf '%s' "$HOME/.local/share/bash-completion/completions/warmbly"
            fi ;;
        zsh)
            printf '%s' "${ZDOTDIR:-$HOME}/.zfunc/_warmbly" ;;
        fish)
            printf '%s' "$HOME/.config/fish/completions/warmbly.fish" ;;
        *) printf '' ;;
    esac
}

install_completions() {
    if [ -n "$NO_COMPLETIONS" ]; then
        return 0
    fi
    shell_name=$(basename "${SHELL:-sh}")
    src=""
    case $shell_name in
        bash) src="$1/completions/warmbly.bash" ;;
        zsh)  src="$1/completions/warmbly.zsh" ;;
        fish) src="$1/completions/warmbly.fish" ;;
        *) return 0 ;;
    esac

    dest=$(completion_dir create)
    [ -n "$dest" ] || return 0

    # The dry run has no unpacked archive to copy from, so it reports the
    # destination rather than testing for a source that cannot exist yet.
    if [ -n "$DRY_RUN" ]; then
        say "    would write $shell_name completions to $dest"
        return 0
    fi
    [ -f "$src" ] || return 0
    mkdir -p "$(dirname "$dest")"
    cp "$src" "$dest"
    ok "wrote $shell_name completions to $dest"
    if [ "$shell_name" = zsh ]; then
        say "    ${C_DIM}zsh needs ~/.zfunc on its fpath: add \`fpath+=~/.zfunc\` above compinit${C_RESET}"
    fi
    return 0
}

# ─────────────────────────────────────────────────────────────────────────
# Uninstall
# ─────────────────────────────────────────────────────────────────────────

do_uninstall() {
    resolve_dir
    target="$DIR/$BIN"
    removed=""

    if [ -f "$target" ]; then
        if [ -n "$DRY_RUN" ]; then
            say "would remove $target"
        else
            rm -f "$target"
            ok "removed $target"
        fi
        removed=1
    fi

    for c in "$HOME/.local/share/bash-completion/completions/warmbly" \
             "${ZDOTDIR:-$HOME}/.zfunc/_warmbly" \
             "$HOME/.config/fish/completions/warmbly.fish"; do
        if [ -f "$c" ]; then
            if [ -n "$DRY_RUN" ]; then
                say "would remove $c"
            else
                rm -f "$c"
                ok "removed $c"
            fi
            removed=1
        fi
    done

    if [ -z "$removed" ]; then
        warn "nothing to remove: no warmbly found in $DIR"
    fi

    # Deliberately left alone: it holds the credentials, and someone
    # reinstalling in a minute should not have to sign in again.
    if [ -d "${XDG_CONFIG_HOME:-$HOME/.config}/warmbly" ]; then
        say ""
        say "Your sign-ins are still in ${XDG_CONFIG_HOME:-$HOME/.config}/warmbly."
        say "Remove them with: rm -rf ${XDG_CONFIG_HOME:-$HOME/.config}/warmbly"
    fi
    return 0
}

# ─────────────────────────────────────────────────────────────────────────
# Install
# ─────────────────────────────────────────────────────────────────────────

# archive_url builds the download URL. The version-less asset names are what
# let "latest" resolve with no GitHub API call, so the install works on a CI
# runner whose IP has already spent the unauthenticated rate limit.
archive_url() {
    name=$1
    if [ -n "$BASE_URL" ]; then
        printf '%s/%s' "${BASE_URL%/}" "$name"
    elif [ -n "$VERSION" ]; then
        printf '%s/download/%s/%s' "$RELEASES" "$VERSION" "$name"
    else
        printf '%s/latest/download/%s' "$RELEASES" "$name"
    fi
}

installed_version() {
    if [ -x "$DIR/$BIN" ]; then
        "$DIR/$BIN" version 2>/dev/null | head -1 | awk '{print $2}'
    fi
}

do_install() {
    detect_platform
    resolve_dir

    archive="warmbly_${TARGET}.tar.gz"
    url=$(archive_url "$archive")
    sums_url=$(archive_url "checksums.txt")

    step "Installing the warmbly CLI"
    say "    platform: ${OS}/${ARCH}"
    say "    version:  ${VERSION:-latest}"
    say "    into:     ${DIR}"
    say ""

    current=$(installed_version)
    if [ -n "$current" ] && [ -z "$FORCE" ] && [ -n "$VERSION" ] && [ "$current" = "$VERSION" ]; then
        ok "warmbly $current is already installed in $DIR"
        say "    ${C_DIM}--force reinstalls it anyway${C_RESET}"
        return 0
    fi

    if [ -n "$DRY_RUN" ]; then
        say "would download $url"
        say "would verify it against $sums_url"
        say "would install $DIR/$BIN"
        install_completions "" || true
        ensure_on_path
        return 0
    fi

    tmp=$(mktemp -d 2>/dev/null || mktemp -d -t warmbly)
    # The trap is set before the first write, so an interrupted install leaves
    # nothing behind in /tmp.
    trap 'rm -rf "$tmp"' EXIT INT TERM

    step "Downloading $archive"
    if ! fetch "$url" "$tmp/$archive"; then
        die "could not download $url
If you pinned --version, check the tag exists: ${RELEASES}"
    fi

    # The checksum is the whole reason this is safer than a bare curl into tar:
    # a truncated download and a tampered one look the same to tar.
    if fetch "$sums_url" "$tmp/checksums.txt" 2>/dev/null; then
        want=$(awk -v f="$archive" '$2 == f || $2 == "*"f { print $1 }' "$tmp/checksums.txt" | head -1)
        got=$(sha256_of "$tmp/$archive")
        if [ -z "$want" ]; then
            warn "checksums.txt has no entry for $archive; continuing without verification"
        elif [ -z "$got" ]; then
            warn "no sha256 tool on this machine, so the download was not verified"
        elif [ "$want" != "$got" ]; then
            die "checksum mismatch for $archive.
  expected $want
  got      $got
Nothing was installed. Try again, and if it happens twice report it: ${RELEASES}"
        else
            ok "checksum verified"
        fi
    else
        warn "could not fetch checksums.txt; continuing without verification"
    fi

    step "Unpacking"
    mkdir -p "$tmp/x"
    tar -xzf "$tmp/$archive" -C "$tmp/x" || die "the archive could not be unpacked"
    [ -f "$tmp/x/$BIN" ] || die "the archive did not contain $BIN"

    mkdir -p "$DIR" 2>/dev/null || die "could not create $DIR.
Pick somewhere writable with --dir, for example: --dir \$HOME/bin"

    # install(1) is not on every minimal image, so this is cp plus chmod, done
    # to a temporary name and moved into place: replacing a running binary with
    # a rename is atomic, overwriting one in place is not.
    cp "$tmp/x/$BIN" "$DIR/.$BIN.new" || die "could not write to $DIR.
Pick somewhere writable with --dir, or re-run with sudo if $DIR is system-owned."
    chmod 0755 "$DIR/.$BIN.new"
    mv -f "$DIR/.$BIN.new" "$DIR/$BIN"

    version_now=$("$DIR/$BIN" version 2>/dev/null | head -1 || echo "")
    ok "installed ${version_now:-warmbly} to $DIR/$BIN"

    install_completions "$tmp/x" || true
    ensure_on_path

    say ""
    say "${C_BOLD}Next:${C_RESET} warmbly auth login"
    say "${C_DIM}Docs: ${DOCS}${C_RESET}"
    return 0
}

main() {
    parse_args "$@"
    setup_colors
    require_downloader

    if [ -n "$UNINSTALL" ]; then
        do_uninstall
        return 0
    fi
    do_install
}

main "$@"
