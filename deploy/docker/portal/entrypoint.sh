#!/bin/sh
# Starts the portal under workerd, pointing its FALLBACK binding at FALLBACK_ORIGIN's host and port.
set -eu

# fallback_address turns http://tracking:3000 into tracking:3000; unset leaves the binding unused.
fallback_address() {
    _origin=${FALLBACK_ORIGIN:-}
    if [ -z "$_origin" ]; then
        printf '127.0.0.1:9'
        return 0
    fi
    _hostport=${_origin#http://}
    _hostport=${_hostport%/}
    case "$_hostport" in
        */*|*@*|'') echo "FALLBACK_ORIGIN must be an http:// origin such as http://tracking:3000" >&2; exit 1 ;;
        *:*) printf '%s' "$_hostport" ;;
        *) printf '%s:80' "$_hostport" ;;
    esac
}

main() {
    _fallback=$(fallback_address)
    exec /app/workerd serve /app/workerd.capnp --external-addr "fallback=$_fallback" "$@"
}

main "$@"
