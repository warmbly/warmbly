#!/usr/bin/env bash
#
# Checks that every published image can be pulled by a stranger.
#
# GHCR creates a package private and does NOT inherit the repository's
# visibility, so an image published from a public repo by a green workflow is
# still unreadable to everyone outside the org until an owner flips it by hand
# in the UI. There is no API for that flip, which means nothing in CI can fix
# it and everything in CI must at least notice it: releases v0.1.0 through
# v0.4.0 all shipped a `curl | sh` installer that could not pull a single byte,
# and every check we had passed, because each one ran authenticated (#371).
#
# So this speaks to the registry the way an anonymous `docker pull` does: an
# unauthenticated token, then the manifest. No docker, no login, no
# credentials to accidentally inherit from the runner.
#
# It prints "<service><TAB><digest>" per image on stdout, so the release can
# build images.json out of the same public view it just verified rather than
# out of a privileged one.
#
#   scripts/check-images-public.sh [--prefix P] [--tag T] [--warn] [service...]
set -euo pipefail

PREFIX="ghcr.io/warmbly/warmbly"
TAG=""
WARN_ONLY=0
SERVICES=()

while [[ $# -gt 0 ]]; do
  case $1 in
    --prefix) PREFIX=$2; shift 2 ;;
    --tag) TAG=$2; shift 2 ;;
    --warn) WARN_ONLY=1; shift ;;
    -h|--help)
      sed -n '2,20p' "$0" | sed 's|^# \{0,1\}||'
      exit 0 ;;
    -*) echo "unknown flag: $1" >&2; exit 2 ;;
    *) SERVICES+=("$1"); shift ;;
  esac
done

# Every image the release advertises. The ten compose services, plus the cli
# image, which the release notes table tells people to pull.
if [[ ${#SERVICES[@]} -eq 0 ]]; then
  SERVICES=(backend consumer worker forms portal updater web admin tracking realtime cli)
fi

fail() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; }
pass() { printf '\033[32m✓\033[0m %s\n' "$*" >&2; }
info() { printf '%s\n' "$*" >&2; }

HOST=${PREFIX%%/*}
NAMESPACE=${PREFIX#*/}
if [[ $HOST != "ghcr.io" ]]; then
  info "· $PREFIX is not on ghcr.io; the anonymous check only speaks GHCR's token flow. Skipped."
  exit 0
fi

# No tag given means "whatever a fresh install would resolve to", which is the
# newest GitHub release, because that is what install.sh pins.
if [[ -z $TAG ]]; then
  TAG=$(curl -fsSL -H 'Accept: application/vnd.github+json' \
    https://api.github.com/repos/warmbly/warmbly/releases/latest 2>/dev/null |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  [[ -n $TAG ]] || { fail "could not resolve the newest release; pass --tag"; exit 1; }
fi

info "Checking $PREFIX/*:$TAG is pullable with no credentials"
info ""

# A manifest list is what a multi-arch pull resolves, and its digest is what
# `docker image inspect` reports as the RepoDigest, so this Accept set is what
# makes the printed digest comparable to what lands on an operator's machine.
ACCEPT='application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json'

# Sets CODE and DIGEST rather than printing them: the response headers carry
# both, and a command substitution would run this in a subshell and lose them.
CODE=""
DIGEST=""
probe() {
  local svc=$1 token headers
  CODE="000"; DIGEST=""
  token=$(curl -fsSL --max-time 20 \
    "https://${HOST}/token?scope=repository:${NAMESPACE}/${svc}:pull&service=${HOST}" 2>/dev/null |
    sed -n 's/.*"token":"\([^"]*\)".*/\1/p') || true
  # No token at all is a network problem, not a visibility one; the caller
  # retries before believing it.
  [[ -n $token ]] || return 0
  headers=$(curl -sS -I --max-time 20 -H "Authorization: Bearer $token" -H "Accept: $ACCEPT" \
    "https://${HOST}/v2/${NAMESPACE}/${svc}/manifests/${TAG}" 2>/dev/null) || true
  CODE=$(printf '%s' "$headers" | sed -n 's|^HTTP/[0-9.]* \([0-9][0-9][0-9]\).*|\1|p' | tail -1)
  CODE=${CODE:-000}
  DIGEST=$(printf '%s' "$headers" |
    sed -n 's/[Dd]ocker-[Cc]ontent-[Dd]igest: *//p' | tr -d '\r' | head -1)
}

private=()
missing=()
for svc in "${SERVICES[@]}"; do
  probe "$svc"
  # 5xx and a dead token are transient often enough that one retry is worth
  # more than a flaky release gate.
  if [[ $CODE == "000" || $CODE == 5* ]]; then
    sleep 2
    probe "$svc"
  fi
  case $CODE in
    200)
      pass "$svc:$TAG is public${DIGEST:+ ($DIGEST)}"
      if [[ -n $DIGEST ]]; then printf '%s\t%s\n' "$svc" "$DIGEST"; fi
      ;;
    403|401)
      # GHCR answers 403 for private and for does-not-exist alike, so this
      # cannot tell them apart and does not pretend to.
      fail "$svc:$TAG is NOT publicly pullable (HTTP $CODE)"
      private+=("$svc")
      ;;
    404)
      fail "$svc:$TAG does not exist (HTTP 404)"
      missing+=("$svc")
      ;;
    *)
      fail "$svc:$TAG could not be checked (HTTP $CODE)"
      private+=("$svc")
      ;;
  esac
done

if [[ ${#private[@]} -eq 0 && ${#missing[@]} -eq 0 ]]; then
  info ""
  printf '\033[32mAll %d images are pullable with no credentials.\033[0m\n' "${#SERVICES[@]}" >&2
  exit 0
fi

broken=()
if [[ ${#private[@]} -gt 0 ]]; then broken+=("${private[@]}"); fi
if [[ ${#missing[@]} -gt 0 ]]; then broken+=("${missing[@]}"); fi
summary="not publicly pullable: ${broken[*]}"

info ""
info "  $summary"
info ""
info "  A package on GHCR is created private and does not inherit the"
info "  repository's visibility. There is no API for the fix; an org owner has"
info "  to do it in the UI, once per package:"
info ""
info "    1. https://github.com/organizations/warmbly/settings/packages"
info "       Package Creation must allow Public, or the control below is greyed out."
info "    2. https://github.com/orgs/warmbly/packages"
info "       each package > Package settings > Danger Zone > Change visibility"
info ""
info "  It applies to every tag at once and cannot be undone."
info ""

if [[ -n ${GITHUB_ACTIONS:-} ]]; then
  if [[ $WARN_ONLY == 1 ]]; then
    echo "::warning title=Images are not public::$summary. The next release will fail its publicity gate. See scripts/check-images-public.sh."
  else
    echo "::error title=Images are not public::$summary. A self-host install of this release cannot pull them."
  fi
fi

if [[ $WARN_ONLY == 1 ]]; then
  exit 0
fi
exit 1
