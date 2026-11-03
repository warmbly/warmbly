# Hosted static edge

This optional Worker serves public frontend files from explicitly mapped HTTPS
Pages origins. The browser-facing custom domain belongs to the Warmbly zone and
uses that zone's certificate and cipher configuration. Pages remains the release
artifact store, so existing release uploads continue to update the upstream.
Self-hosted deployments do not use this Worker.

`PAGES_ORIGINS` is a JSON object mapping each exact public hostname to its Pages
origin, for example `{"stage.warmbly.com":"https://example.pages.dev"}`. It is
operator configuration, never read from the request. The checked-in configuration
has an empty map and no domains or routes. Deploying it alone serves no website.

Only GET, HEAD and OPTIONS reach the static origin. Application authorization and
cookies are removed before forwarding. API, forms and realtime traffic keep their
existing hosts and paths. Responses retain the Pages security headers, cache
policy, assets and status. Redirects back to the selected Pages origin use the
visitor's hostname. No request URL, token or body is logged by this Worker.

## Deployment and rollback

1. Save current DNS, Pages custom-domain associations, certificate settings and
   production deployment identifiers privately. Identify the account that owns
   the zone, its existing Workers plan, and any usage charges before deployment.
2. Deploy the reviewed module with a private configuration mapping only a staging
   hostname to one existing Pages production origin. Attach that hostname as a
   Worker custom domain. Keep all existing production domains on Pages.
3. Independently verify certificate trust, the allowed TLS 1.2 AEAD suites, TLS
   1.3 and refusal of CBC. Verify the root page, nested SPA routes, JS/CSS assets,
   redirects, 404 behavior and response security headers against the upstream.
4. Prepare the exact production map from authenticated Pages account metadata.
   Before each cutover, preserve that hostname's DNS and Pages association. Move
   one hostname at a time to the Worker custom domain and repeat the checks.
   Keep the Pages projects and deployment history intact.
5. If a check fails, restore that hostname's saved Pages association and DNS, then
   verify it serves the previous content. Do not proceed to another hostname.

For normal releases use the stable production Pages origin. An immutable
deployment URL can pin a staging or rollback comparison. The Worker map is a
separate reviewed configuration; no release workflow changes it automatically.

Validate with `node --test deploy/cloudflare/static-edge.test.mjs` and
`pnpm dlx wrangler@4.45.0 deploy --dry-run --config deploy/cloudflare/wrangler.jsonc`.
Cloudflare custom-domain and certificate API responses are configuration evidence;
independent TLS handshakes are required to establish the effective policy.
