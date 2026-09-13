#!/bin/sh
# Render the runtime config from container env so a single built image serves
# any deployment. Runs before nginx starts (nginx /docker-entrypoint.d hook).
#
# WARMBLY_CONFIG_OUT moves where it writes, which is how a static host that
# has no container start renders the same file at build time. One definition
# of the key set, so the two paths cannot drift apart.
set -eu

CONFIG_OUT="${WARMBLY_CONFIG_OUT:-/usr/share/nginx/html/config.js}"
mkdir -p "$(dirname "$CONFIG_OUT")"

# Values are written into JavaScript string literals, so a double quote, a
# backslash or a line break in one would end the literal early and take the
# whole config with it, leaving the app with no API_URL at all. Escape rather
# than trust whatever ended up in .env.
js() {
    printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\r\n'
}

cat > "$CONFIG_OUT" <<EOF
window.__WARMBLY_ENV__ = {
  API_URL: "$(js "${WARMBLY_API_URL:-}")",
  APP_URL: "$(js "${WARMBLY_APP_URL:-}")",
  TURNSTILE_KEY: "$(js "${WARMBLY_TURNSTILE_KEY:-}")",
  BETA_NOTICE: "$(js "${WARMBLY_BETA_NOTICE:-}")",
  SENTRY_DSN: "$(js "${WARMBLY_SENTRY_DSN:-}")",
  SENTRY_ENVIRONMENT: "$(js "${WARMBLY_SENTRY_ENVIRONMENT:-}")",
  POSTHOG_KEY: "$(js "${WARMBLY_POSTHOG_KEY:-}")",
  POSTHOG_HOST: "$(js "${WARMBLY_POSTHOG_HOST:-}")",
  POSTHOG_UI_HOST: "$(js "${WARMBLY_POSTHOG_UI_HOST:-}")",
  POSTHOG_ERROR_TRACKING: "$(js "${WARMBLY_POSTHOG_ERROR_TRACKING:-}")"
};
EOF

# The redirect truncates in place and keeps whatever mode the built file had, so
# a restrictive umask or checkout leaves nginx serving 403 for the whole config.
chmod 644 "$CONFIG_OUT"
