import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import worker from "./index.mjs";
import { BUNDLE, lookupCache, memoryLimiter, mintRenderToken, pathKind, readConfig, servePortal, verifyRenderToken } from "./portal.mjs";

const TOKEN = "fixture-portal-edge-token-0123456789abcdef";
const PORTAL = "portal.agency.test";
const SHARED = "portal.warmbly.test";
const SHELL = '<!doctype html><html><head><meta name="wf-token" content="" /><script src="/_portal/assets/index-abc.js"></script></head><body></body></html>';
const LOADER = "/* loader */";
const tok = (c) => c.repeat(43);

// limiter is a rate-limit binding that admits budget calls per key.
function limiter(budget = Infinity) {
  const seen = new Map();
  return { seen, limit: async ({ key }) => { seen.set(key, (seen.get(key) ?? 0) + 1); return { success: seen.get(key) <= budget }; } };
}

const assets = {
  fetch: async (req) => {
    const path = new URL(req.url).pathname;
    if (path === `${BUNDLE}/index.html`) return new Response(SHELL, { headers: { "Content-Type": "text/html" } });
    if (path === `${BUNDLE}/embed.js`) return new Response(LOADER, { headers: { "Content-Type": "text/javascript" } });
    if (path === `${BUNDLE}/assets/index-abc.js`) return new Response("bundle", { headers: { "Content-Type": "text/javascript" } });
    return new Response("missing", { status: 404 });
  },
};

function makeEnv(overrides = {}) {
  return {
    API_ORIGIN: "https://api.warmbly.test", PORTAL_DOMAIN: SHARED, DASHBOARD_ORIGIN: "https://dash.pages.dev", PORTAL_EDGE_TOKEN: TOKEN, ASSETS: assets,
    CONNECT_VIEWS: limiter(), CONNECT_ACTIONS: limiter(), CONNECT_POLLS: limiter(), REPORT_VIEWS: limiter(),
    ...overrides,
  };
}
const config = readConfig(makeEnv());

// origins plays the backend and the Pages build; backend calls are recorded with their parsed body.
function origins({ portals = [PORTAL], backend = {}, pages } = {}, calls = []) {
  return async (upstream) => {
    const url = new URL(upstream.url);
    const body = upstream.method === "POST" ? await upstream.json() : undefined;
    calls.push({ url, method: upstream.method, auth: upstream.headers.get("Authorization"), body });
    if (url.origin === "https://api.warmbly.test") {
      const m = /^\/api\/v1\/internal\/portals\/(.+)$/.exec(url.pathname);
      if (m) return portals.includes(decodeURIComponent(m[1])) ? Response.json({ host: m[1] }) : new Response(null, { status: 404 });
      const handler = backend[url.pathname];
      if (handler) return handler(url, body);
      return assert.fail("unexpected backend call " + url.pathname);
    }
    if (pages) return pages(upstream, url);
    return assert.fail("unexpected fetch " + url.href);
  };
}

const at = (path, init = {}) => new Request(`https://${PORTAL}${path}`, init);
const renderOf = async (res) => /<meta name="wf-token" content="([^"]+)" \/>/.exec(await res.text())?.[1];

function memoryCache() {
  const store = new Map();
  return { store, match: async (key) => store.get(key.url)?.clone(), put: async (key, res) => { store.set(key.url, res); } };
}

test("paths are split between the portal's own pages, the dashboard and nothing", () => {
  const cases = {
    "/embed.js": "loader", "/_portal/assets/index-abc.js": "bundle", [`/connect/${tok("a")}`]: "connect", [`/r/${tok("a")}`]: "report",
    [`/api/connect/${tok("a")}`]: "connect-api", [`/api/connect/${tok("a")}/oauth`]: "connect-api", [`/api/connect/${tok("a")}/oauth/result`]: "connect-api",
    [`/api/connect/${tok("a")}/smtp-imap`]: "connect-api", [`/api/reports/${tok("a")}`]: "report-api",
    [`/api/connect/${tok("a")}/vendor`]: "connect-api", [`/api/connect/${tok("a")}/vendor/import`]: "connect-api", [`/api/connect/${tok("a")}/vendor/other`]: "api-none",
    "/api/forms/pub/submit": "api-none", "/api/v1/me": "api-none",
    "/f/pub123": "none", "/forms.js": "none", "/_portal/index.html": "none", "/connect/a/b": "none", "/r/a/b": "none",
    "/": "dashboard", "/connect": "dashboard", "/app/campaigns": "dashboard", "/assets/index-def.js": "dashboard", "/rx": "dashboard",
  };
  for (const [path, kind] of Object.entries(cases)) assert.equal(pathKind(path).kind, kind, path);
});

test("the dashboard comes from the dashboard origin, without credentials or HSTS, and refuses writes before any fetch", async () => {
  for (const path of ["/", "/app/campaigns", "/connect", "/assets/index-def.js", "/auth/login?next=/app"]) {
    const r = await servePortal(at(path, { headers: { Cookie: "s=1", Authorization: "Bearer x" } }), config, origins({
      pages: async (upstream, url) => {
        assert.equal(url.origin + url.pathname + url.search, "https://dash.pages.dev" + path);
        assert.equal(upstream.headers.get("Cookie"), null);
        assert.equal(upstream.headers.get("Authorization"), null);
        return new Response("dashboard", { headers: { "Content-Security-Policy": "default-src 'none'", "Strict-Transport-Security": "max-age=1" } });
      },
    }));
    assert.equal(await r.text(), "dashboard", path);
    assert.equal(r.headers.get("Content-Security-Policy"), "default-src 'none'");
    assert.equal(r.headers.get("Strict-Transport-Security"), null);
  }
  const r = await servePortal(at("/app", { method: "POST" }), config, origins());
  assert.equal(r.status, 405);
  assert.equal(r.headers.get("Allow"), "GET, HEAD");
});

test("forms are never served on a portal domain", async () => {
  for (const path of ["/f/pub123", "/forms.js", "/_portal/index.html"]) {
    const calls = [];
    const r = await servePortal(at(path), config, origins({}, calls));
    assert.equal(r.status, 404, path);
    assert.match(await r.text(), /There is nothing at this address/);
    assert.equal(calls.length, 1, "only the host lookup");
  }
  const r = await servePortal(at("/api/forms/pub123/submit", { method: "POST", body: "{}" }), config, origins());
  assert.equal(r.status, 404);
  assert.equal((await r.json()).code, "not_found");
});

test("a connect page is the bundle's shell with a render token for that link and the client-page headers", async () => {
  const token = tok("c");
  for (const path of [`/connect/${token}`, `/connect/${token}?embed=1`]) {
    const r = await servePortal(at(path), config, origins());
    assert.equal(r.status, 200);
    assert.equal(r.headers.get("Content-Type"), "text/html; charset=utf-8");
    assert.equal(r.headers.get("Content-Security-Policy"), "frame-ancestors *");
    assert.equal(r.headers.get("X-Robots-Tag"), "noindex, nofollow");
    assert.equal(r.headers.get("Referrer-Policy"), "no-referrer");
    assert.equal(r.headers.get("Cache-Control"), "no-store");
    const render = await renderOf(r);
    assert.ok(await verifyRenderToken(TOKEN, "connect:" + token, render));
    assert.ok(!(await verifyRenderToken(TOKEN, "connect:" + tok("d"), render)), "bound to its own link");
    assert.ok(!(await verifyRenderToken(TOKEN, "report:" + token, render)), "bound to connect pages");
  }
  const bad = await servePortal(at("/connect/short"), config, origins());
  assert.equal(bad.status, 404);
});

test("connect calls reach the backend's internal routes with the portal token, the visitor's host and address", async () => {
  const token = tok("e");
  const render = await mintRenderToken(TOKEN, "connect:" + token);
  const calls = [];
  const backend = {
    "/api/v1/internal/connect-links/lookup": async () => Response.json({ workspace: "Acme" }),
    "/api/v1/internal/connect-links/oauth/start": async () => Response.json({ url: "https://accounts.example/x", result_key: "k" }),
    "/api/v1/internal/connect-links/oauth/result": async () => Response.json({ status: "pending" }),
    "/api/v1/internal/connect-links/smtp-imap": async () => Response.json({ code: "client_link_full", message: "Full." }, { status: 409 }),
    "/api/v1/internal/connect-links/vendor/mailboxes": async () => Response.json({ ticket: "t", mailboxes: [], remaining: 5 }),
    "/api/v1/internal/connect-links/vendor/import": async () => Response.json({ mailboxes: 2, connected: 2, max_mailboxes: 5 }, { status: 201 }),
  };
  const headers = { "X-Warmbly-Render": render, "CF-Connecting-IP": "203.0.113.9", "X-Forwarded-Host": "other.example", "Content-Type": "application/json" };
  const call = (suffix, body) => servePortal(at(`/api/connect/${token}${suffix}`, body === undefined ? { headers } : { method: "POST", headers, body: JSON.stringify(body) }),
    config, origins({ backend }, calls));

  const lookup = await call("");
  assert.equal(lookup.status, 200);
  assert.deepEqual(await lookup.json(), { workspace: "Acme" });
  assert.equal(lookup.headers.get("Cache-Control"), "no-store");
  assert.equal(lookup.headers.get("Referrer-Policy"), "no-referrer");
  assert.equal((await call("/oauth", { provider: "gmail".padEnd(40, "x") })).status, 200);
  assert.equal((await call("/oauth/result", { result_key: "k" })).status, 200);
  const smtp = await call("/smtp-imap", {
    email: "a@b.test", smtp: { host: "smtp.b.test", port: 465, username: "a", password: "p", security: "tls", extra: "dropped" }, imap: null, captcha_token: "c",
  });
  assert.equal(smtp.status, 409);
  assert.equal((await smtp.json()).code, "client_link_full");
  // A vendor key goes to the backend once; only a ticket comes back, and the pick names it.
  const listed = await call("/vendor", { vendor: "zapmail", fields: { api_key: "zm_secret" }, captcha_token: "c", extra: "dropped" });
  assert.equal(listed.status, 200);
  assert.deepEqual(await listed.json(), { ticket: "t", mailboxes: [], remaining: 5 });
  assert.equal((await call("/vendor/import", { ticket: "t", mailbox_ids: ["m1", "m2"], fields: { api_key: "dropped" } })).status, 201);
  for (const bad of [{ vendor: "zapmail" }, { vendor: "zapmail", fields: {} }, { vendor: "zapmail", fields: { api_key: 7 } }, { vendor: 7, fields: { api_key: "k" } },
    { vendor: "zapmail", fields: Object.fromEntries(Array.from({ length: 9 }, (_, i) => ["k" + i, "v"])) }]) {
    assert.equal((await call("/vendor", bad)).status, 400, JSON.stringify(bad));
  }
  for (const bad of [{ ticket: "t" }, { ticket: "t", mailbox_ids: [] }, { ticket: "t", mailbox_ids: ["m1", 2] }, { mailbox_ids: ["m1"] },
    { ticket: "t", mailbox_ids: Array.from({ length: 501 }, (_, i) => "m" + i) }]) {
    assert.equal((await call("/vendor/import", bad)).status, 400, JSON.stringify(bad).slice(0, 60));
  }

  const sent = calls.filter((c) => c.url.pathname.startsWith("/api/v1/internal/connect-links"));
  assert.deepEqual(sent.map((c) => c.url.pathname.replace("/api/v1/internal/connect-links", "")), ["/lookup", "/oauth/start", "/oauth/result", "/smtp-imap", "/vendor/mailboxes", "/vendor/import"]);
  for (const c of sent) {
    assert.equal(c.auth, `Bearer ${TOKEN}`);
    assert.equal(c.body.token, token);
    assert.equal(c.body.host, PORTAL);
  }
  assert.deepEqual(sent[1].body, { token, host: PORTAL, provider: "gmail".padEnd(32, "x"), remote_ip: "203.0.113.9" });
  assert.deepEqual(sent[2].body, { token, host: PORTAL, result_key: "k" });
  assert.deepEqual(sent[3].body.smtp, { host: "smtp.b.test", port: 465, username: "a", password: "p", security: "tls" });
  assert.equal(sent[3].body.imap, null);
  assert.deepEqual(sent[4].body, { token, host: PORTAL, vendor: "zapmail", fields: { api_key: "zm_secret" }, captcha_token: "c", remote_ip: "203.0.113.9" });
  assert.deepEqual(sent[5].body, { token, host: PORTAL, ticket: "t", mailbox_ids: ["m1", "m2"], remote_ip: "203.0.113.9" });
  assert.equal(sent[3].body.remote_ip, "203.0.113.9");
});

test("connect calls without the page's render token, or with a repeated one, never reach the backend", async () => {
  const token = tok("f");
  const fresh = await mintRenderToken(TOKEN, "connect:" + token);
  const cases = [
    [{}, 403, "stale_page"],
    [{ "X-Warmbly-Render": "forged" }, 403, "stale_page"],
    [{ "X-Warmbly-Render": await mintRenderToken(TOKEN, "connect:" + tok("g")) }, 403, "stale_page"],
    [{ "X-Warmbly-Render": await mintRenderToken(TOKEN, "report:" + token) }, 403, "stale_page"],
    [{ "X-Warmbly-Render": await mintRenderToken("another-token-0123456789abcdef0123", "connect:" + token) }, 403, "stale_page"],
    [{ "X-Warmbly-Render": await mintRenderToken(TOKEN, "connect:" + token, Date.now() - 13 * 3600 * 1000) }, 403, "stale_page"],
    [{ "X-Warmbly-Render": `${fresh}, ${fresh}` }, 400, "duplicate_header"],
  ];
  for (const [headers, status, code] of cases) {
    const calls = [];
    const r = await servePortal(at(`/api/connect/${token}`, { headers }), config, origins({}, calls));
    assert.equal(r.status, status, JSON.stringify(headers));
    assert.equal((await r.json()).code, code);
    assert.equal(calls.length, 1, "only the host lookup");
  }
  const dup = await servePortal(at(`/api/reports/${token}?range=a&range=b`, { headers: { "X-Warmbly-Render": fresh } }), config, origins());
  assert.equal(dup.status, 400);
  assert.equal((await dup.json()).code, "duplicate_query_parameter");
});

test("connect bodies are bounded and shaped like the backend expects", async () => {
  const token = tok("h");
  const headers = { "X-Warmbly-Render": await mintRenderToken(TOKEN, "connect:" + token) };
  const post = (suffix, body) => servePortal(at(`/api/connect/${token}${suffix}`, { method: "POST", headers, body }), config, origins());
  const big = await post("/smtp-imap", JSON.stringify({ email: "x".repeat(17 << 10) }));
  assert.equal(big.status, 413);
  assert.equal((await big.json()).code, "body_too_large");
  for (const body of ["not json", "[]", JSON.stringify({ smtp: { port: "465" } }), JSON.stringify({ email: 5 })]) {
    const r = await post("/smtp-imap", body);
    assert.equal(r.status, 400, body);
    assert.equal((await r.json()).code, "invalid_body");
  }
  assert.equal((await post("/oauth", JSON.stringify({ provider: 1 }))).status, 400);
  assert.equal((await post("/oauth/result", "x".repeat(9 << 10))).status, 400);
  const wrongMethod = await servePortal(at(`/api/connect/${token}/oauth`, { headers }), config, origins());
  assert.equal(wrongMethod.status, 404);
});

test("a backend failure is a fixed sentence and a budget overrun is refused before the backend", async () => {
  const token = tok("i");
  const headers = { "X-Warmbly-Render": await mintRenderToken(TOKEN, "connect:" + token), "CF-Connecting-IP": "198.51.100.7" };
  for (const answer of [() => new Response("boom", { status: 500 }), () => new Response("<html>", { status: 200 }), () => { throw new Error("down"); }]) {
    const r = await servePortal(at(`/api/connect/${token}`, { headers }), config, origins({ backend: { "/api/v1/internal/connect-links/lookup": answer } }));
    assert.equal(r.status, 503);
    assert.deepEqual(await r.json(), { code: "unavailable", message: "This page is temporarily unavailable. Try again in a few minutes." });
  }

  const limited = readConfig(makeEnv({ CONNECT_ACTIONS: limiter(1) }));
  const calls = [];
  const backend = { "/api/v1/internal/connect-links/oauth/start": async () => Response.json({ url: "u", result_key: "k" }) };
  const start = () => servePortal(at(`/api/connect/${token}/oauth`, { method: "POST", headers, body: '{"provider":"gmail"}' }), limited, origins({ backend }, calls));
  assert.equal((await start()).status, 200);
  const over = await start();
  assert.equal(over.status, 429);
  assert.equal(over.headers.get("Retry-After"), "60");
  assert.equal((await over.json()).code, "rate_limit_exceeded");
  assert.equal(calls.filter((c) => c.url.pathname.endsWith("/oauth/start")).length, 1);
  assert.equal(limited.limits.connectActions.seen.get("198.51.100.7"), 2);
});

test("a shared report resolves on the visitor's host before its shell is served", async () => {
  const live = tok("j");
  const gone = tok("k");
  const calls = [];
  const backend = {
    [`/api/v1/internal/reports/${live}`]: async () => Response.json({ report: { name: "Weekly" } }),
    [`/api/v1/internal/reports/${gone}`]: async () => new Response(null, { status: 404 }),
  };
  const r = await servePortal(at(`/r/${live}?embed=1`), config, origins({ backend }, calls));
  assert.equal(r.status, 200);
  assert.equal(r.headers.get("Content-Security-Policy"), "frame-ancestors *");
  assert.equal(r.headers.get("X-Robots-Tag"), "noindex, nofollow");
  const render = await renderOf(r);
  assert.ok(await verifyRenderToken(TOKEN, "report:" + live, render));
  const fetched = calls.find((c) => c.url.pathname.startsWith("/api/v1/internal/reports/"));
  assert.equal(fetched.url.searchParams.get("host"), PORTAL);
  assert.equal(fetched.auth, `Bearer ${TOKEN}`);

  const json = await servePortal(at(`/api/reports/${live}`, { headers: { "X-Warmbly-Render": render } }), config, origins({ backend }, calls));
  assert.equal(json.status, 200);
  assert.equal(json.headers.get("Cache-Control"), "private, max-age=60");
  assert.deepEqual(await json.json(), { report: { name: "Weekly" } });
  assert.equal(calls.filter((c) => c.url.pathname === `/api/v1/internal/reports/${live}`).length, 1, "the JSON read reuses the shell's answer");

  const missing = await servePortal(at(`/r/${gone}`), config, origins({ backend }));
  assert.equal(missing.status, 404);
  assert.match(await missing.text(), /no longer shared/);
  const malformed = await servePortal(at("/r/short"), config, origins());
  assert.equal(malformed.status, 404);

  const range = await servePortal(at(`/api/reports/${live}?range=${"x".repeat(33)}`, { headers: { "X-Warmbly-Render": render } }), config, origins({ backend }));
  assert.equal(range.status, 400);
  assert.equal((await range.json()).code, "invalid_range");
});

test("a report the backend cannot answer is unavailable and an over-budget reader is turned away", async () => {
  const token = tok("l");
  const backend = { [`/api/v1/internal/reports/${token}`]: async () => new Response("down", { status: 502 }) };
  const shell = await servePortal(at(`/r/${token}`), config, origins({ backend }));
  assert.equal(shell.status, 503);
  assert.match(await shell.text(), /temporarily unavailable/);
  const render = await mintRenderToken(TOKEN, "report:" + token);
  const json = await servePortal(at(`/api/reports/${token}`, { headers: { "X-Warmbly-Render": render } }), config, origins({ backend }));
  assert.equal(json.status, 503);
  assert.equal((await json.json()).code, "unavailable");

  const limited = readConfig(makeEnv({ REPORT_VIEWS: limiter(0) }));
  const over = await servePortal(at(`/r/${token}`, { headers: { "CF-Connecting-IP": "192.0.2.1" } }), limited, origins());
  assert.equal(over.status, 503);
  assert.equal(over.headers.get("Retry-After"), "60");
});

test("the embed loader and the bundle's hashed files come from the page bundle", async () => {
  const loader = await servePortal(at("/embed.js"), config, origins());
  assert.equal(await loader.text(), LOADER);
  assert.equal(loader.headers.get("Content-Type"), "application/javascript; charset=utf-8");
  assert.equal(loader.headers.get("Cache-Control"), "public, max-age=3600");
  const asset = await servePortal(at("/_portal/assets/index-abc.js"), config, origins());
  assert.equal(await asset.text(), "bundle");
  assert.equal(asset.headers.get("Cache-Control"), "public, max-age=31536000, immutable");
  const miss = await servePortal(at("/_portal/assets/nope.js"), config, origins());
  assert.equal(miss.status, 404);
});

test("the loader keeps the embed contract with the connect page and the report", () => {
  const loader = readFileSync(new URL("../public/embed.js", import.meta.url), "utf8");
  const connect = readFileSync(new URL("../src/connectEmbed.ts", import.meta.url), "utf8");
  const embed = readFileSync(new URL("../src/embed.ts", import.meta.url), "utf8");
  assert.match(connect, /CONNECT_CONNECTED_MESSAGE = "warmbly:connect:connected"/);
  assert.match(connect, /CONNECT_RESIZE_MESSAGE = "warmbly:connect:resize"/);
  assert.match(embed, /type: "warmbly:resize", page/);
  assert.match(loader, /d\.type === "warmbly:connect:connected"/);
  assert.match(loader, /d\.type === "warmbly:connect:resize"/);
  assert.match(loader, /new CustomEvent\("warmbly:connected"/);
  for (const want of ["data-warmbly-connect", "data-warmbly-report", '"/connect/"', '"/r/"', "?embed=1"]) assert.ok(loader.includes(want), want);
  assert.ok(!loader.includes("data-warmbly-form"), "forms embed from the forms host's own loader");
});

test("the shared address serves any workspace's client pages without a host lookup, and no dashboard", async () => {
  const token = tok("s");
  const calls = [];
  const backend = {
    "/api/v1/internal/connect-links/lookup": async () => Response.json({ workspace: "Acme" }),
    [`/api/v1/internal/reports/${token}`]: async () => Response.json({ report: { name: "Monthly" } }),
  };
  const on = (path, init) => new Request(`https://${SHARED}${path}`, init);
  const shellPage = await servePortal(on(`/connect/${token}`), config, origins({ portals: [], backend }, calls));
  assert.equal(shellPage.status, 200);
  const render = await renderOf(shellPage);
  const lookup = await servePortal(on(`/api/connect/${token}`, { headers: { "X-Warmbly-Render": render } }), config, origins({ portals: [], backend }, calls));
  assert.equal(lookup.status, 200);
  assert.equal(calls.at(-1).body.host, SHARED);
  const report = await servePortal(on(`/r/${token}`), config, origins({ portals: [], backend }, calls));
  assert.equal(report.status, 200);
  assert.equal(calls.at(-1).url.searchParams.get("host"), SHARED);
  assert.equal((await servePortal(on("/embed.js"), config, origins({ portals: [] }, calls))).status, 200);
  assert.equal(calls.filter((c) => c.url.pathname.startsWith("/api/v1/internal/portals/")).length, 0);

  for (const path of ["/", "/app/campaigns", "/assets/index-def.js", "/f/pub123"]) {
    const r = await servePortal(on(path), config, origins({ portals: [], pages: async () => assert.fail("shared address fetched the dashboard") }));
    assert.equal(r.status, 404, path);
    assert.match(await r.text(), /There is nothing at this address/);
  }
  // PORTAL_DOMAIN may be written as a URL, with a port.
  const withScheme = readConfig(makeEnv({ PORTAL_DOMAIN: "https://Portal.Warmbly.test:8443/" }));
  assert.equal(withScheme.shared, SHARED);
});

test("an agency domain serves no dashboard when no dashboard origin is configured", async () => {
  const bare = readConfig(makeEnv({ DASHBOARD_ORIGIN: "" }));
  const r = await servePortal(at("/app"), bare, origins({ pages: async () => assert.fail("fetched a dashboard") }));
  assert.equal(r.status, 404);
  assert.equal((await servePortal(at(`/connect/${tok("t")}`), bare, origins())).status, 200);
});

test("without rate-limit bindings the budgets are counted in the isolate, keyed on the configured address header", async () => {
  const token = tok("u");
  const env = makeEnv({ CLIENT_IP_HEADER: "X-Forwarded-For" });
  for (const name of ["CONNECT_VIEWS", "CONNECT_ACTIONS", "CONNECT_POLLS", "REPORT_VIEWS"]) delete env[name];
  const unbound = readConfig(env);
  const calls = [];
  const backend = { "/api/v1/internal/connect-links/oauth/start": async () => Response.json({ url: "u", result_key: "k" }) };
  const headers = { "X-Warmbly-Render": await mintRenderToken(TOKEN, "connect:" + token), "X-Forwarded-For": "198.51.100.200, 203.0.113.77" };
  const start = () => servePortal(at(`/api/connect/${token}/oauth`, { method: "POST", headers, body: '{"provider":"gmail"}' }), unbound, origins({ backend }, calls));
  for (let i = 0; i < 10; i++) assert.equal((await start()).status, 200);
  assert.equal((await start()).status, 429);
  assert.equal(calls.find((c) => c.url.pathname.endsWith("/oauth/start")).body.remote_ip, "203.0.113.77");
});

test("the isolate limiter is a fixed one-minute window per key", async () => {
  let now = 0;
  const l = memoryLimiter(2, () => now);
  const ok = async (key) => (await l.limit({ key })).success;
  assert.deepEqual([await ok("a"), await ok("a"), await ok("a"), await ok("b")], [true, true, false, true]);
  now = 60_000;
  assert.equal(await ok("a"), true);
});

test("lookup verdicts survive a shared cache that keeps nothing or throws", async () => {
  const host = "isolate.agency.test";
  const calls = [];
  const broken = { match: async () => { throw new Error("no cache"); }, put: async () => { throw new Error("no cache"); } };
  const cache = lookupCache(broken);
  const req = (path) => new Request(`https://${host}${path}`);
  for (const path of ["/", "/app"]) {
    const r = await servePortal(req(path), config, origins({ portals: [host], pages: async () => new Response("ok") }, calls), cache);
    assert.equal(await r.text(), "ok");
  }
  assert.equal(calls.filter((c) => c.url.pathname.startsWith("/api/v1/internal/portals/")).length, 1);
});

test("an unknown host, or an origin itself, is a plain 404; a failed lookup is a 503 that is not cached", async (t) => {
  for (const url of ["https://unknown.example.test/connect/" + tok("m"), "https://api.warmbly.test/", "https://dash.pages.dev/"]) {
    const r = await servePortal(new Request(url), config, origins());
    assert.equal(r.status, 404, url);
    assert.equal(await r.text(), "Not found.");
  }
  const cache = memoryCache();
  t.mock.method(globalThis, "fetch", async () => new Response("down", { status: 502 }));
  globalThis.caches = { default: cache };
  t.after(() => { delete globalThis.caches; });
  const r = await worker.fetch(at("/"), makeEnv());
  assert.equal(r.status, 503);
  assert.equal(r.headers.get("Cache-Control"), "no-store");
  assert.equal(cache.store.size, 0);
});

test("the lookup is cached as a bare verdict under a path no fetch can reach", async () => {
  const cache = memoryCache();
  const calls = [];
  const pages = async () => new Response("ok");
  await servePortal(at("/"), config, origins({ pages }, calls), cache);
  await servePortal(at("/app"), config, origins({ pages }, calls), cache);
  assert.equal(calls.filter((c) => c.url.pathname.startsWith("/api/v1/internal/portals/")).length, 1);
  const [[key, stored]] = cache.store.entries();
  assert.equal(key, `https://${PORTAL}/_portal/lookup`);
  assert.equal(stored.headers.get("Cache-Control"), "max-age=60");
  assert.deepEqual(await stored.clone().json(), { portal: true });
});

test("upstream redirects keep the visitor on their own host", async () => {
  const r = await servePortal(at("/app"), config, origins({ pages: async () => new Response(null, { status: 308, headers: { Location: "https://dash.pages.dev/app/" } }) }));
  assert.equal(r.headers.get("Location"), `https://${PORTAL}/app/`);
});

test("plain http origins are accepted, for a self-host's internal network", () => {
  const local = readConfig(makeEnv({ API_ORIGIN: "http://backend:8080", DASHBOARD_ORIGIN: "http://web:80" }));
  assert.equal(local.api.href, "http://backend:8080/");
  assert.equal(local.dashboard.href, "http://web/");
});

test("an unconfigured service serves nothing, and a bad configuration fails closed", async (t) => {
  t.mock.method(globalThis, "fetch", async () => assert.fail("unconfigured service fetched"));
  for (const env of [{}, { API_ORIGIN: "", PORTAL_DOMAIN: "" }, makeEnv({ PORTAL_DOMAIN: "" })]) {
    const bare = await worker.fetch(at("/"), env);
    assert.equal(bare.status, 404);
  }
  for (const bad of [
    { DASHBOARD_ORIGIN: "ftp://dash.example" }, { DASHBOARD_ORIGIN: "https://dash.example/app" }, { API_ORIGIN: "https://api.warmbly.test/v1" },
    { API_ORIGIN: "https://user:pw@api.warmbly.test" }, { PORTAL_DOMAIN: "portal.warmbly.test/x" },
    { PORTAL_EDGE_TOKEN: "short" }, { ASSETS: undefined }, { CONNECT_POLLS: {} },
  ]) {
    const r = await worker.fetch(at("/"), makeEnv(bad));
    assert.equal(r.status, 503, JSON.stringify(bad));
    assert.equal(await r.text(), "Service temporarily unavailable.");
    assert.equal(r.headers.get("Strict-Transport-Security"), null);
  }
});

test("the entry module exports nothing but its handler, because workerd starts every export as an entrypoint", async () => {
  const entry = await import("./index.mjs");
  assert.deepEqual(Object.keys(entry), ["default"]);
  assert.deepEqual(Object.keys(entry.default), ["fetch"]);
});

// fallbackService plays FALLBACK, the self-host's tracking service, recording what reached it.
function fallbackService(seen = [], respond = () => new Response("from fallback", { status: 202, headers: { "X-Warmbly-Service": "tracking" } })) {
  return {
    seen,
    fetch: async (req) => {
      seen.push({ method: req.method, url: new URL(req.url), headers: Object.fromEntries(req.headers), body: req.body ? await req.text() : null, redirect: req.redirect });
      return respond(req);
    },
  };
}
const withFallback = (service, overrides = {}) => readConfig(makeEnv({ FALLBACK_ORIGIN: "http://tracking:3000", FALLBACK: service, ...overrides }));

test("a host that is not a portal domain is passed to the fallback as it came, and its answer returned as is", async () => {
  const fallback = fallbackService([], () => new Response(null, { status: 302, headers: { Location: "https://acme.example/", "X-Warmbly-Service": "tracking", "Cache-Control": "public, max-age=300" } }));
  const cfg = withFallback(fallback);
  const headers = { Cookie: "s=1", Authorization: "Bearer x", "X-Forwarded-For": "203.0.113.9", "Content-Type": "application/x-www-form-urlencoded", "X-Warmbly-Render": "r" };
  const r = await servePortal(new Request("https://go.customer.test/unsubscribe/abc?x=1&x=2", { method: "POST", headers, body: "confirm=1" }), cfg, origins());
  assert.equal(r.status, 302);
  assert.equal(r.headers.get("Location"), "https://acme.example/");
  assert.equal(r.headers.get("X-Warmbly-Service"), "tracking");
  assert.equal(r.headers.get("Cache-Control"), "public, max-age=300");
  const [got] = fallback.seen;
  assert.equal(got.method, "POST");
  assert.equal(got.url.href, "https://go.customer.test/unsubscribe/abc?x=1&x=2");
  assert.equal(got.body, "confirm=1");
  assert.equal(got.redirect, "manual");
  for (const [name, value] of Object.entries(headers)) assert.equal(got.headers[name.toLowerCase()], value, name);
  // The listening socket turned X-Forwarded-Proto into the scheme, so it is put back for the service behind.
  assert.equal(got.headers["x-forwarded-proto"], "https");

  const kept = fallbackService();
  await servePortal(new Request("http://t.customer.test/c/link", { headers: { "X-Forwarded-Proto": "https" } }), withFallback(kept), origins());
  assert.equal(kept.seen[0].headers["x-forwarded-proto"], "https");
  assert.equal(kept.seen[0].method, "GET");
  assert.equal(kept.seen[0].url.pathname, "/c/link");
});

test("the fallback is never used for a portal domain, the shared address or an outage", async () => {
  const fallback = { fetch: async () => assert.fail("fell back") };
  const cfg = withFallback(fallback);
  const pages = async () => new Response("dashboard");
  assert.equal(await (await servePortal(at("/app"), cfg, origins({ pages }))).text(), "dashboard");
  for (const path of ["/f/pub123", "/forms.js", "/api/forms/pub123"]) assert.equal((await servePortal(at(path), cfg, origins())).status, 404, path);
  assert.equal((await servePortal(new Request(`https://${SHARED}/f/pub123`), cfg, origins())).status, 404);
  assert.equal((await servePortal(new Request("https://api.warmbly.test/"), cfg, origins())).status, 404);

  const down = async () => new Response("down", { status: 502 });
  await assert.rejects(servePortal(new Request("https://t.customer.test/"), cfg, down));
});

test("with only FALLBACK_ORIGIN set every request passes through, and a fallback without its binding fails closed", async (t) => {
  t.mock.method(globalThis, "fetch", async () => assert.fail("looked a host up without a configuration"));
  const fallback = fallbackService();
  const r = await worker.fetch(new Request(`https://${PORTAL}/connect/${tok("p")}`), { FALLBACK_ORIGIN: "http://tracking:3000", FALLBACK: fallback, ASSETS: assets });
  assert.equal(r.status, 202);
  assert.equal(fallback.seen[0].url.pathname, `/connect/${tok("p")}`);
  for (const bad of [{ FALLBACK_ORIGIN: "http://tracking:3000" }, { FALLBACK_ORIGIN: "https://tracking:3000", FALLBACK: fallback }, { FALLBACK_ORIGIN: "http://tracking:3000/x", FALLBACK: fallback }]) {
    const res = await worker.fetch(new Request("https://t.customer.test/"), makeEnv(bad));
    assert.equal(res.status, 503, JSON.stringify(bad.FALLBACK_ORIGIN));
  }
  // Cloud sets no FALLBACK_ORIGIN, so an unknown host stays a plain 404 whatever is bound.
  const cloud = readConfig(makeEnv({ FALLBACK: fallback }));
  assert.equal(cloud.fallback, null);
});

test("bundle files from a store that labels everything octet-stream get their real type", async () => {
  const disk = { fetch: async () => new Response("body", { headers: { "Content-Type": "application/octet-stream" } }) };
  const cfg = readConfig(makeEnv({ ASSETS: disk }));
  for (const [file, type] of [["index-abc.js", "text/javascript; charset=utf-8"], ["index-abc.css", "text/css; charset=utf-8"], ["logo.svg", "image/svg+xml"], ["blob.bin", "application/octet-stream"]]) {
    const r = await servePortal(at(`/_portal/assets/${file}`), cfg, origins());
    assert.equal(r.headers.get("Content-Type"), type, file);
  }
  const typed = await servePortal(at("/_portal/assets/index-abc.js"), config, origins());
  assert.equal(typed.headers.get("Content-Type"), "text/javascript");
});

test("the runtime's fetch is never called as a method, which workerd and Cloudflare refuse", async (t) => {
  const seen = [];
  t.mock.method(globalThis, "fetch", function (req) {
    if (this !== undefined && this !== globalThis) throw new TypeError("Illegal invocation");
    seen.push(new URL(req.url));
    if (seen.at(-1).pathname.startsWith("/api/v1/internal/portals/")) return Promise.resolve(Response.json({ host: PORTAL }));
    return Promise.resolve(new Response("dashboard"));
  });
  const r = await worker.fetch(at("/app"), makeEnv());
  assert.equal(await r.text(), "dashboard");
  assert.equal(seen.at(-1).href, "https://dash.pages.dev/app");
});
