# The portal service (portal/worker) under workerd on a self-host. Warmbly Cloud runs the same two modules as
# a Worker (deploy/cloudflare/portal/wrangler.jsonc); this file is the self-host's only difference from it.
using Workerd = import "/workerd/workerd.capnp";

const config :Workerd.Config = (
  services = [
    (name = "portal", worker = .portal),
    # The page build, read-only; workerd labels its files application/octet-stream and the Worker corrects that.
    (name = "bundle", disk = (path = "/app/dist", writable = false)),
    # API_ORIGIN and DASHBOARD_ORIGIN are container addresses (http://backend:8080), so private ones are allowed.
    (name = "internet", network = (allow = ["public", "private", "local"])),
    # FALLBACK_ORIGIN keeps the visitor's Host only as a service binding; entrypoint.sh sets its address.
    (name = "fallback", external = (address = "127.0.0.1:9", http = ())),
    (name = "health", worker = .health),
  ],
  sockets = [
    # The proxy in front terminates TLS; its X-Forwarded-Proto becomes the request URL's scheme.
    (name = "http", address = "*:8095", http = (forwardedProtoHeader = "X-Forwarded-Proto"), service = "portal"),
    # Loopback only, so the health check never answers for the portal's own hosts.
    (name = "health", address = "127.0.0.1:8096", http = (), service = "health"),
  ],
);

const portal :Workerd.Worker = (
  modules = [
    (name = "index.mjs", esModule = embed "index.mjs"),
    (name = "portal.mjs", esModule = embed "portal.mjs"),
  ],
  # The newest date this workerd release supports; keep it at or below the build's own date.
  compatibilityDate = "2026-10-26",
  bindings = [
    (name = "ASSETS", service = "bundle"),
    (name = "FALLBACK", service = "fallback"),
    (name = "API_ORIGIN", fromEnvironment = "API_ORIGIN"),
    (name = "PORTAL_DOMAIN", fromEnvironment = "PORTAL_DOMAIN"),
    (name = "PORTAL_EDGE_TOKEN", fromEnvironment = "PORTAL_EDGE_TOKEN"),
    (name = "DASHBOARD_ORIGIN", fromEnvironment = "DASHBOARD_ORIGIN"),
    (name = "CLIENT_IP_HEADER", fromEnvironment = "CLIENT_IP_HEADER"),
    (name = "FALLBACK_ORIGIN", fromEnvironment = "FALLBACK_ORIGIN"),
  ],
  globalOutbound = "internet",
);

const health :Workerd.Worker = (
  modules = [(name = "health.mjs", esModule = "export default { fetch: () => new Response(\"ok\") };")],
  compatibilityDate = "2026-10-26",
);
