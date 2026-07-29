#!/bin/sh
# Render the runtime config from container env so a single built image serves
# any deployment. Runs before nginx starts (nginx /docker-entrypoint.d hook).
set -eu

cat > /usr/share/nginx/html/config.js <<EOF
window.__WARMBLY_ENV__ = {
  API_URL: "${WARMBLY_API_URL:-}",
  APP_URL: "${WARMBLY_APP_URL:-}",
  TRACKING_DOMAIN: "${WARMBLY_TRACKING_DOMAIN:-}",
  TURNSTILE_KEY: "${WARMBLY_TURNSTILE_KEY:-}"
};
EOF
