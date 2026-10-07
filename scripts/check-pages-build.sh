#!/bin/sh
# Everything CI should know about serving web and admin from a static host.
#
# Those apps read window.__WARMBLY_ENV__ from /config.js, which the container
# entrypoint renders at start. A static host has no container start, so the
# same script renders it at build time via WARMBLY_CONFIG_OUT. Both paths share
# one definition of the key set precisely so they cannot drift, and this checks
# the sharing still works.
#
# It runs the real entrypoints rather than reading them: a config.js that is
# subtly malformed still looks fine in a diff and leaves the app with no API
# URL at runtime, which presents as a blank page and nothing in the logs.
set -eu

fail() { printf 'check-pages-build: %s\n' "$*" >&2; exit 1; }
ok()   { printf '  ok  %s\n' "$*"; }
skip() { printf '  --  %s\n' "$*"; }

WORK=$(mktemp -d)
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

for app in web admin; do
  entry="$app/docker-entrypoint.sh"
  [ -f "$entry" ] || fail "$entry not found (run from the repository root)"

  if command -v dash >/dev/null 2>&1; then
    dash -n "$entry" || fail "dash -n failed on $entry"
  else
    sh -n "$entry" || fail "sh -n failed on $entry"
  fi

  # The container path must stay exactly what it was: this file is the nginx
  # entrypoint hook, and a changed default silently stops the image working.
  grep -q '/usr/share/nginx/html/config.js' "$entry" \
    || fail "$entry no longer defaults to the nginx path; the container image would serve no config"

  out="$WORK/$app/config.js"
  WARMBLY_CONFIG_OUT="$out" \
  WARMBLY_API_URL="https://api.example.com" \
  WARMBLY_APP_URL="https://app.example.com" \
  WARMBLY_DASHBOARD_URL="https://app.example.com" \
    sh "$entry" || fail "$entry failed to render to WARMBLY_CONFIG_OUT"

  [ -f "$out" ] || fail "$entry ignored WARMBLY_CONFIG_OUT; a static build would ship no config.js"

  grep -q 'window.__WARMBLY_ENV__' "$out" \
    || fail "$app config.js does not define window.__WARMBLY_ENV__"
  grep -q 'API_URL: "https://api.example.com"' "$out" \
    || fail "$app config.js did not pick up WARMBLY_API_URL"

  if command -v node >/dev/null 2>&1; then
    node --check "$out" >/dev/null 2>&1 \
      || fail "$app config.js is not valid JavaScript; the app would load with no configuration at all"
  fi

  # Vite copies public/ to the build root, so this is what lands beside
  # index.html. Without it every deep link 404s on a static host.
  [ -f "$app/public/_redirects" ] || fail "$app/public/_redirects is missing; deep links would 404 on a static host"
  if [ "$app" = web ]; then
    [ -f "$app/public/404.html" ] || fail "web needs a 404.html to disable Pages' implicit SPA fallback for assets"
    grep -qE '^/app/\*[[:space:]]+/[[:space:]]+200' "$app/public/_redirects" \
      || fail "web has no dashboard deep-link rewrite"
    if grep -qE '^/\*[[:space:]]+/([[:space:]]|index\.html)' "$app/public/_redirects"; then
      fail "web rewrites missing assets to HTML"
    fi
  else
    grep -qE '^/\*[[:space:]]+/index\.html[[:space:]]+200' "$app/public/_redirects" \
      || fail "$app/public/_redirects has no SPA rule serving index.html with 200"
  fi

  # The build a static host runs has to exist and has to render the config.
  grep -q '"build:pages"' "$app/package.json" \
    || fail "$app/package.json has no build:pages script"
  grep -q 'WARMBLY_CONFIG_OUT=dist/config.js' "$app/package.json" \
    || fail "$app build:pages does not render config.js into the build output"

  ok "$app renders config.js, has the SPA rule, and keeps the container default"
done

if ! command -v node >/dev/null 2>&1; then
  skip "node not installed; skipped the JavaScript syntax check"
fi

printf 'check-pages-build: all checks passed\n'
