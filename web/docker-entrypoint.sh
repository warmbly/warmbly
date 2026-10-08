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
  GMAIL_OAUTH_CONNECT: "$(js "${WARMBLY_GMAIL_OAUTH_CONNECT:-}")",
  BETA_NOTICE: "$(js "${WARMBLY_BETA_NOTICE:-}")",
  SENTRY_DSN: "$(js "${WARMBLY_SENTRY_DSN:-}")",
  SENTRY_ENVIRONMENT: "$(js "${WARMBLY_SENTRY_ENVIRONMENT:-}")",
  POSTHOG_KEY: "$(js "${WARMBLY_POSTHOG_KEY:-}")",
  POSTHOG_HOST: "$(js "${WARMBLY_POSTHOG_HOST:-}")",
  POSTHOG_UI_HOST: "$(js "${WARMBLY_POSTHOG_UI_HOST:-}")",
  POSTHOG_ERROR_TRACKING: "$(js "${WARMBLY_POSTHOG_ERROR_TRACKING:-}")",
  POSTHOG_SESSION_REPLAY: "$(js "${WARMBLY_POSTHOG_SESSION_REPLAY:-}")",
  COMPANY_LOGOS: "$(js "${WARMBLY_COMPANY_LOGOS:-}")"
};
EOF

# The redirect truncates in place and keeps whatever mode the built file had, so
# a restrictive umask or checkout leaves nginx serving 403 for the whole config.
chmod 644 "$CONFIG_OUT"

# The build hashes the fixed inline bootstrap; deployment supplies resource origins.
CSP_DIR=$(dirname "$CONFIG_OUT")
if [ -f "$CSP_DIR/csp-template.txt" ]; then
    origin() {
        [ -n "$1" ] || return 0
        case "$1" in /*) return 0 ;; esac
        scheme=${1%%://*}
        case "$scheme" in http|https|ws|wss) ;; *) printf 'Invalid CSP resource URL\n' >&2; exit 1 ;; esac
        authority=${1#*://}
        authority=${authority%%/*}
        authority=${authority%%\?*}
        authority=${authority%%\#*}
        authority=${authority##*@}
        printf '%s' "$authority" | grep -Eq '^([a-zA-Z0-9._-]+|\[[a-fA-F0-9:]+\])(:[0-9]+)?$' || {
            printf 'Invalid CSP resource origin\n' >&2; exit 1;
        }
        printf '%s://%s' "$scheme" "$authority"
    }
    defaults() { if [ -f "$CSP_DIR/csp-origins.txt" ]; then sed -n "${1}p" "$CSP_DIR/csp-origins.txt"; fi; }
    api_url=${WARMBLY_API_URL:-${VITE_API_URL:-$(defaults 1)}}
    api=$(origin "$api_url")
    sentry=$(origin "${WARMBLY_SENTRY_DSN:-${VITE_SENTRY_DSN:-$(defaults 2)}}")
    analytics_url=${WARMBLY_POSTHOG_HOST:-${VITE_POSTHOG_HOST:-$(defaults 3)}}
    analytics=$(origin "${analytics_url:-https://us.i.posthog.com}")
    connections=""
    for resource in ${WARMBLY_CSP_CONNECT_ORIGINS:-${VITE_CSP_CONNECT_ORIGINS:-$(defaults 4)}}; do
        connections="$connections $(origin "$resource")"
    done
    # Discover deployment configuration before serving an immutable CSP header.
    realtime=${WEBSOCKET_URL:-}
    if [ -z "$realtime" ] && [ -n "$api" ]; then
        config_url="${api_url%/}"
        config_url="${config_url%/v1}/v1/auth/config"
        if command -v curl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1 &&
            deployment=$(curl --fail --silent --proto '=http,https' --connect-timeout 2 --max-time 4 --max-filesize 65536 --retry 2 --retry-connrefused --retry-delay 1 --retry-max-time 10 "$config_url") &&
            realtime=$(printf '%s' "$deployment" | jq -er 'if .websocket_url == null then "" elif (.websocket_url | type) == "string" then .websocket_url else error("invalid websocket_url") end'); then
            :
        else
            realtime=""
        printf 'Realtime CSP discovery unavailable; the dashboard can use the API-origin socket route. Explicit CSP origins are needed only for direct gateway connections.\n' >&2
        fi
    fi
    if [ -n "$realtime" ]; then
        if realtime_origin=$(origin "$realtime") && [ -n "$realtime_origin" ]; then
            api_websocket=$(printf '%s' "$api" | sed 's/^http/ws/')
            case " $api $api_websocket $connections " in
                *" $realtime_origin "*) ;;
                *) connections="$connections $realtime_origin" ;;
            esac
        else
            printf 'Ignoring invalid discovered realtime origin; CSP remains restricted.\n' >&2
        fi
    fi
    assets=$(printf '%s' "$analytics" | sed -e 's#://us\.i\.posthog\.com$#://us-assets.i.posthog.com#' -e 's#://eu\.i\.posthog\.com$#://eu-assets.i.posthog.com#')
    websocket=$(printf '%s' "$api" | sed 's/^http/ws/')
    policy=$(sed -e "s#__API_SOURCES__#$api $websocket#g" -e "s#__API_IMAGE_SOURCE__#$api#g" -e "s#__SENTRY_SOURCE__#$sentry#g" -e "s#__ANALYTICS_SOURCES__#$analytics $assets#g" -e "s#__CONNECT_SOURCES__#$connections#g" "$CSP_DIR/csp-template.txt")
    if [ "${#policy}" -gt 1973 ]; then printf 'Dashboard CSP exceeds the static host header limit\n' >&2; exit 1; fi
    printf '%s\n' "$policy" > "$CSP_DIR/csp-policy.txt"
    if [ -f "$CSP_DIR/_headers" ]; then
        CSP_POLICY="$policy" awk '/^  Content-Security-Policy:/ && !replaced { print "  Content-Security-Policy: " ENVIRON["CSP_POLICY"]; replaced=1; next } { print }' "$CSP_DIR/_headers" > "$CSP_DIR/_headers.new"
        mv "$CSP_DIR/_headers.new" "$CSP_DIR/_headers"
    fi
    if [ "$CONFIG_OUT" = /usr/share/nginx/html/config.js ]; then
        CSP_POLICY="$policy" awk '/^add_header Content-Security-Policy/ { print "add_header Content-Security-Policy \"" ENVIRON["CSP_POLICY"] "\" always;"; next } { print }' /etc/nginx/warmbly-security-headers.conf > /etc/nginx/warmbly-security-headers.conf.new
        mv /etc/nginx/warmbly-security-headers.conf.new /etc/nginx/warmbly-security-headers.conf
    fi
    chmod 644 "$CSP_DIR/csp-policy.txt"
fi
