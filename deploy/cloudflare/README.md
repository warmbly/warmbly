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

# Portal

The portal service runs here as a Worker. Its source is `portal/worker/` and its
page app is `portal/`, shared with the self-hosted deployment; only
`portal/wrangler.jsonc` is Cloudflare's. It answers two kinds of host on the
Cloudflare for SaaS zone `PORTAL_DOMAIN` is on (`CLOUDFLARE_SAAS_PORTAL_ZONE_ID`
on the backend):

- `PORTAL_DOMAIN` itself, the shared portal address. Every connect link and
  report of a workspace without an agency portal domain is built on it, for any
  workspace, agency or not. It serves the client pages below and nothing else:
  every other path answers the "nothing at this address" page, so the shared
  address never looks like a sign-in page. No host lookup is made for it.
- every agency portal domain, which customers `CNAME` to `PORTAL_DOMAIN` and
  Cloudflare issues a certificate for. For each request the Worker asks the
  backend whether the host is a verified portal domain
  (`GET /api/v1/internal/portals/:host`) and keeps only that verdict for a
  minute, in the isolate and the Worker cache. A host that is not one,
  including the configured origins themselves, gets a plain `404`. A lookup
  that fails answers `503` and is not cached.

On both:

- `/connect/<token>` and `/r/<token>` (also with `?embed=1`) are the client
  connect page and the shared report: the shell of the page build with a render
  token stamped in, sent with `Content-Security-Policy: frame-ancestors *`,
  `X-Robots-Tag: noindex, nofollow`, `Referrer-Policy: no-referrer` and
  `Cache-Control: no-store`. A report is resolved first, so an unknown or revoked
  link gets the "no longer shared" page
- `/api/connect/<token>` (lookup, `/oauth`, `/oauth/result`, `/smtp-imap`) and
  `/api/reports/<token>` are the JSON routes those pages call. Each needs the
  page's render token, refuses a repeated query parameter or render token, and
  calls the backend's internal `connect-links` and `reports` routes with the
  visitor's host, so a portal domain only ever resolves its own agency's
  clients' links and the shared address resolves any
- `/embed.js` is the embed loader (`portal/public/embed.js`) and
  `/_portal/assets/` the page build's hashed files
- `/f/`, `/forms.js` and every other `/api/` path answer `404`: forms are served
  by the forms service only
- on an agency portal domain, every other GET or HEAD goes to
  `DASHBOARD_ORIGIN`, the dashboard's Pages build, whose `_redirects` keep the
  app's routes on `index.html`. Other methods answer `405`

Per-visitor budgets use Cloudflare rate-limiting bindings, keyed on
`CF-Connecting-IP` and set in `portal/wrangler.jsonc`: link lookups 30, sign-in
starts and server-details submits 10, sign-in result polls 60 and report reads
(page and JSON together) 60, each per minute. They count per Cloudflare location
and are approximate; the backend also limits attempts per link. Without the
bindings (workerd on a self-host) the same budgets are counted in the process.
Report answers are kept in the isolate's memory for a minute (half a minute for
a missing link), so a revoke takes effect within it.

The Worker reaches the backend at `API_ORIGIN` with `PORTAL_EDGE_TOKEN`, a
Worker secret matching the backend's `PORTAL_EDGE_TOKEN`. That token opens only
the portal lookup, connect-link and report routes, and nothing else opens them,
so the Worker holds neither `INTERNAL_API_TOKEN` nor `NODE_BROKER_TOKEN`.
Cookies and authorization never leave the Worker. Dashboard responses keep
their security headers except `Strict-Transport-Security`, which is never sent
for a customer's domain. No request URL, token or body is logged.

The checked-in `portal/wrangler.jsonc` sets no origins, routes or domains.
Deployed as is, it answers `404` to everything. It also leaves `FALLBACK_ORIGIN`
unset, the self-host image's pass-through for hosts that are not portal domains
(`deploy/docker/portal.Dockerfile`), so here every other host answers `404`. An invalid configuration, a
missing page build or an invalid rate-limit binding answers `503`. The entry
module, `portal/worker/index.mjs`, exports only its handler, because the
runtime starts every export of the main module as an entrypoint.

## Building and deploying

The page build is not checked in. Build it, then deploy:

```bash
pnpm --dir portal install --frozen-lockfile
pnpm --dir portal build
pnpm dlx wrangler@4.45.0 deploy --config deploy/cloudflare/portal/wrangler.jsonc
```

`pnpm build` writes `portal/dist/_portal/`, the Worker's `ASSETS` directory.
Rebuild and redeploy whenever a release changes `portal/`. Like the static edge,
no workflow deploys this Worker; a deploy is a reviewed manual step.

## Setting it up on Warmbly Cloud

1. Enable Cloudflare for SaaS on the zone `PORTAL_DOMAIN` lives on. Create the
   fallback origin as a proxied record (an `AAAA` to `100::` is enough, because
   the Worker answers and nothing is fetched from it) and set it as the zone's
   fallback origin. Create `PORTAL_DOMAIN` itself as a proxied `CNAME` to the
   fallback origin.
2. Generate a token (`openssl rand -hex 32`). Add it as `PORTAL_EDGE_TOKEN` to
   the backend's secret, add the name to the backend's `secret_keys`, apply the
   runtime stack and roll the backend. Until the Worker uses it nothing changes.
3. Build and deploy this Worker with a private configuration: `API_ORIGIN` (the
   public API, such as `https://api.warmbly.com`), `PORTAL_DOMAIN`
   (`portal.warmbly.com`) and `DASHBOARD_ORIGIN` (the dashboard's production
   Pages origin). Set the token with `wrangler secret put PORTAL_EDGE_TOKEN
   --config deploy/cloudflare/portal/wrangler.jsonc`. Give each rate-limit
   binding a `namespace_id` not used by another Worker on the account.
4. Route requests to it. Attach `PORTAL_DOMAIN` to the Worker for the shared
   address, as a custom domain or a route `portal.warmbly.com/*`. Agency portal
   domains reach it one of two ways:
   - **The zone also holds other customer hostnames** (tracking or redirect
     domains registered through `CLOUDFLARE_SAAS_ZONE_ID`, or any added by
     hand). Add no catch-all route: it would send those names to the portal,
     which answers `404` for them. Set `CLOUDFLARE_SAAS_PORTAL_WORKER` to the
     Worker's name in the next step instead, and the backend adds a route
     `<domain>/*` for each portal domain as it registers it and removes it with
     the domain. This is how Warmbly Cloud runs.
   - **The zone holds portal domains only.** Add one route `*/*` on the zone,
     plus routes with no Worker for the zone's own names (`warmbly.com/*`,
     `*.warmbly.com/*`) so they keep their current handling.

   Either way, confirm that each existing hostname on the zone still answers as
   before.
5. Set `PORTAL_DOMAIN`, `CLOUDFLARE_SAAS_PORTAL_ZONE_ID` and
   `CLOUDFLARE_SAAS_PORTAL_API_TOKEN` on the backend, and
   `CLOUDFLARE_SAAS_PORTAL_WORKER` for per-domain routes, then roll it. The
   token needs `SSL and Certificates: Edit` on the zone, and `Workers Routes:
   Edit` as well for per-domain routes. Connect links and reports are built on
   the portal address from then on, and verified portal domains are registered
   on the zone within two minutes.
6. Verify on the shared address and on one test portal domain: the certificate,
   a connect link (also framed through `/embed.js`), a report, a `404` for
   `/f/<id>`, the dashboard on the portal domain and the "nothing at this
   address" page at the shared address's root. To roll back, remove the Worker
   routes and unset `PORTAL_DOMAIN` on the backend, which stops new links being
   built; registered hostnames stay and serve nothing until the routes return.
   With per-domain routes, unsetting `CLOUDFLARE_SAAS_PORTAL_WORKER` stops new
   routes and leaves the existing ones in place.

A zone that already serves portal domains through a `*/*` route moves to
per-domain routes in two steps: set `CLOUDFLARE_SAAS_PORTAL_WORKER` and roll the
backend, then remove the `*/*` route a day later. A domain that is already
active gets its own route at its next daily check, so the old route has to stay
until every one has been checked.

To rotate the token, move the current value into the backend's
`PORTAL_EDGE_PREVIOUS_TOKENS` with a `PORTAL_EDGE_PREVIOUS_UNTIL` cutoff, set the
new value as `PORTAL_EDGE_TOKEN` and roll the backend, put the new value in the
Worker, then let the cutoff pass.

Validate with `pnpm --dir portal test` and, after `pnpm --dir portal build`,
`pnpm dlx wrangler@4.45.0 deploy --dry-run --config deploy/cloudflare/portal/wrangler.jsonc`.
Portal CI runs both.
