// The portal service: client connect pages, shared reports and the embed loader on the shared portal address
// (PORTAL_DOMAIN) and on every verified agency portal domain, which also serves the dashboard from
// DASHBOARD_ORIGIN. It runs as a Cloudflare Worker on Warmbly Cloud and under workerd on a self-host, so
// every platform binding it reads but ASSETS has a fallback. With FALLBACK_ORIGIN set (a self-host's shared
// customer-domain door), any other host is passed on to that service unchanged instead of answering 404.

const LOOKUP_TTL_SECONDS = 60;
const LOOKUP_CACHE_MAX = 4096;
const RENDER_TTL_SECONDS = 12 * 60 * 60;
const REPORT_TTL_MS = 60_000;
const REPORT_MISS_TTL_MS = 30_000;
const REPORT_CACHE_MAX = 1024;
const MIN_TOKEN_LENGTH = 32;
// The page bundle lives under its own prefix, so its files never shadow the dashboard's /assets/.
export const BUNDLE = "/_portal";
const SHELL_PATH = BUNDLE + "/index.html";
const LOADER_PATH = BUNDLE + "/embed.js";
const PLACEHOLDER = '<meta name="wf-token" content="" />';
// Connect and report link tokens: 32 random bytes, base64url without padding (internal/formwire).
const LINK_TOKEN = /^[A-Za-z0-9_-]{43}$/;
const CONNECT_MAX_BODY = 16 << 10;
const ACTION_MAX_BODY = 8 << 10;
const VENDOR_IMPORT_MAX_BODY = 192 << 10;
const BACKEND_MAX_BODY = 4 << 20;
const LIMITER_KEYS_MAX = 16384;

// Per-visitor budgets per minute: the named rate-limit binding when the platform has one (wrangler.jsonc sets
// the same numbers), else a counter in this isolate.
export const LIMITS = {
  connectViews: { binding: "CONNECT_VIEWS", perMinute: 30 },
  connectActions: { binding: "CONNECT_ACTIONS", perMinute: 10 },
  connectPolls: { binding: "CONNECT_POLLS", perMinute: 60 },
  reportViews: { binding: "REPORT_VIEWS", perMinute: 60 },
};

const enc = new TextEncoder();
const NOSNIFF = { "X-Content-Type-Options": "nosniff" };

const plain = (status, body, extra = {}) => new Response(body, {
  status,
  headers: {
    ...NOSNIFF,
    "Cache-Control": "no-store",
    "Content-Type": "text/plain; charset=utf-8",
    "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
    ...extra,
  },
});
const notFound = () => plain(404, "Not found.");
const failure = () => plain(503, "Service temporarily unavailable.");
const methodNotAllowed = () => plain(405, "Method not allowed.", { Allow: "GET, HEAD" });

// Pages for a request that never reaches the app.
const page = (title, heading, body) => `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>${title}</title></head><body style="font-family:system-ui,-apple-system,sans-serif;max-width:26rem;margin:5rem auto;padding:0 1rem;color:#0f172a;text-align:center"><h1 style="font-size:1.1rem;margin:0 0 .5rem">${heading}</h1><p style="font-size:.9rem;color:#475569">${body}</p></body></html>`;
const NOT_FOUND_PAGE = page("Not found", "There is nothing at this address", "Check the link you were sent.");
const REPORT_GONE_PAGE = page("Report not available", "This report is no longer shared", "The link may have expired or been turned off. Ask whoever sent it for a new one.");
const REPORT_DOWN_PAGE = page("Report unavailable", "This report is temporarily unavailable", "Please try again in a moment.");

// Client pages: any site may frame them, no search engine may index them, and nothing they link to learns the token.
const CLIENT_PAGE_HEADERS = {
  ...NOSNIFF,
  "Cache-Control": "no-store",
  "Content-Security-Policy": "frame-ancestors *",
  "Referrer-Policy": "no-referrer",
  "X-Robots-Tag": "noindex, nofollow",
};
const html = (status, body, extra = {}) => new Response(body, {
  status,
  headers: { ...CLIENT_PAGE_HEADERS, "Content-Type": "text/html; charset=utf-8", ...extra },
});

const API_HEADERS = { ...NOSNIFF, "Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "Content-Type": "application/json; charset=utf-8" };
const json = (status, body, extra = {}) => new Response(typeof body === "string" ? body : JSON.stringify(body), {
  status,
  headers: { ...API_HEADERS, ...extra },
});
// abort answers with the same {error, message, code} shape as the API.
const abort = (status, code, message, extra = {}) => json(status, { error: code, message, code }, extra);
const apiNotFound = () => abort(404, "not_found", "No portal API route matches this path.");
const CONNECT_UNAVAILABLE = { code: "unavailable", message: "This page is temporarily unavailable. Try again in a few minutes." };
const REPORT_API_HEADERS = { "Content-Security-Policy": "frame-ancestors *", "X-Robots-Tag": "noindex, nofollow" };

// origin accepts a bare http(s) origin: a self-host reaches its backend and dashboard over plain HTTP inside its network.
function origin(raw) {
  const u = new URL(raw);
  const ok = (u.protocol === "https:" || u.protocol === "http:") && !u.username && !u.password && u.pathname === "/" && !u.search && !u.hash;
  if (!ok) throw new Error("invalid origin");
  return u;
}

// sharedHost is PORTAL_DOMAIN's bare host name, given with or without a scheme or port.
function sharedHost(raw) {
  const u = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : "https://" + raw);
  if (u.username || u.password || (u.pathname !== "/" && u.pathname !== "") || u.search || u.hash) throw new Error("invalid PORTAL_DOMAIN");
  return u.hostname.toLowerCase().replace(/\.$/, "");
}

// memoryLimiter is a fixed one-minute window per key, the same contract as a rate-limit binding.
export function memoryLimiter(perMinute, now = () => Date.now()) {
  const windows = new Map();
  return {
    async limit({ key }) {
      const at = now();
      let w = windows.get(key);
      if (!w || at >= w.resets) {
        if (windows.size >= LIMITER_KEYS_MAX) windows.clear();
        w = { count: 0, resets: at + 60_000 };
        windows.set(key, w);
      }
      w.count++;
      return { success: w.count <= perMinute };
    },
  };
}

// Limiters without a binding outlive one request, so their counts mean something within an isolate.
const fallbackLimiters = new Map();
function fallbackLimiter(name, perMinute) {
  let l = fallbackLimiters.get(name);
  if (!l) {
    l = memoryLimiter(perMinute);
    fallbackLimiters.set(name, l);
  }
  return l;
}

// readFallback is the service other hosts go to. workerd's fetch() sets Host from the URL, so the visitor's Host
// only survives through a proxy-style service binding (FALLBACK), which the self-host image points at FALLBACK_ORIGIN.
function readFallback(env) {
  const raw = env?.FALLBACK_ORIGIN;
  if (!raw) return null;
  if (origin(raw).protocol !== "http:") throw new Error("FALLBACK_ORIGIN must be a plain http origin");
  if (typeof env.FALLBACK?.fetch !== "function") throw new Error("FALLBACK_ORIGIN without its FALLBACK binding");
  return env.FALLBACK;
}

// config is null when the service is not configured, so a bare deploy serves nothing; with only a fallback, it
// passes everything on.
export function readConfig(env) {
  const fallback = readFallback(env);
  const { API_ORIGIN: api, PORTAL_DOMAIN: shared, PORTAL_EDGE_TOKEN: token, DASHBOARD_ORIGIN: dashboard } = env ?? {};
  if (!api || !shared || !token) return fallback ? { fallback } : null;
  if (token.length < MIN_TOKEN_LENGTH) throw new Error("short token");
  if (typeof env.ASSETS?.fetch !== "function") throw new Error("no page bundle");
  const limits = {};
  for (const [key, { binding, perMinute }] of Object.entries(LIMITS)) {
    const bound = env[binding];
    if (bound !== undefined && typeof bound?.limit !== "function") throw new Error("invalid rate limit " + binding);
    limits[key] = bound ?? fallbackLimiter(binding, perMinute);
  }
  return {
    api: origin(api),
    shared: sharedHost(shared),
    dashboard: dashboard ? origin(dashboard) : null,
    token,
    assets: env.ASSETS,
    limits,
    // Cloudflare sets CF-Connecting-IP; a self-host names the header its reverse proxy sets.
    ipHeader: (env.CLIENT_IP_HEADER ?? "").trim() || "CF-Connecting-IP",
    fallback,
  };
}

// pathKind names who answers a path; "dashboard" is everything the portal's own pages do not own.
export function pathKind(path) {
  if (path === "/embed.js") return { kind: "loader" };
  if (path.startsWith(BUNDLE + "/assets/")) return { kind: "bundle" };
  let m = /^\/connect\/([^/]*)$/.exec(path);
  if (m) return { kind: "connect", token: m[1] };
  m = /^\/r\/([^/]*)$/.exec(path);
  if (m) return { kind: "report", token: m[1] };
  m = /^\/api\/connect\/([^/]+)(|\/oauth|\/oauth\/result|\/smtp-imap|\/vendor|\/vendor\/import)$/.exec(path);
  if (m) return { kind: "connect-api", token: m[1], action: m[2] };
  m = /^\/api\/reports\/([^/]+)$/.exec(path);
  if (m) return { kind: "report-api", token: m[1] };
  if (path.startsWith("/api/")) return { kind: "api-none" };
  // Forms are served on the forms host, never by the portal.
  if (path === BUNDLE || path.startsWith(BUNDLE + "/") || path.startsWith("/connect/") || path.startsWith("/r/")
    || path.startsWith("/f/") || path === "/f" || path === "/forms.js") return { kind: "none" };
  return { kind: "dashboard" };
}

// Verdicts are also kept in this isolate, for a runtime whose shared cache keeps nothing or refuses.
const localVerdicts = new Map();
export function lookupCache(shared) {
  return {
    async match(key) {
      const held = localVerdicts.get(key.url);
      if (held && held.expires > Date.now()) return new Response(held.body);
      try {
        return shared ? await shared.match(key) : undefined;
      } catch {
        return undefined;
      }
    },
    async put(key, response) {
      if (localVerdicts.size >= LOOKUP_CACHE_MAX) localVerdicts.clear();
      localVerdicts.set(key.url, { body: await response.clone().text(), expires: Date.now() + LOOKUP_TTL_SECONDS * 1000 });
      try {
        if (shared) await shared.put(key, response);
      } catch {
        // the isolate's copy is enough
      }
    },
  };
}

// isPortal asks the backend whether host is a verified agency portal domain and keeps only that verdict for a minute.
export async function isPortal(host, config, fetchOrigin, cache) {
  // A path this service answers itself, so no fetch can ever be served this entry.
  const key = new Request(new URL(BUNDLE + "/lookup", "https://" + host));
  const hit = cache ? await cache.match(key) : undefined;
  if (hit) return (await hit.json()).portal === true;
  const res = await backend(config, fetchOrigin, "/api/v1/internal/portals/" + encodeURIComponent(host));
  if (res.status !== 200 && res.status !== 404) throw new Error("lookup failed");
  const portal = res.status === 200;
  if (cache) {
    await cache.put(key, new Response(JSON.stringify({ portal }), {
      headers: { "Content-Type": "application/json", "Cache-Control": `max-age=${LOOKUP_TTL_SECONDS}` },
    }));
  }
  return portal;
}

function backend(config, fetchOrigin, path, body) {
  const headers = { Accept: "application/json", Authorization: `Bearer ${config.token}` };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  return fetchOrigin(new Request(new URL(path, config.api), {
    method: body === undefined ? "GET" : "POST",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    redirect: "manual",
  }));
}

// Render tokens: the JSON routes answer only a page this service served.
const renderKeys = new Map();
function renderKey(secret) {
  let key = renderKeys.get(secret);
  if (!key) {
    key = crypto.subtle.digest("SHA-256", enc.encode(secret + ":portal-render"))
      .then((raw) => crypto.subtle.importKey("raw", raw, { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]));
    renderKeys.set(secret, key);
  }
  return key;
}

const b64url = (bytes) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
function unb64url(s) {
  if (!/^[A-Za-z0-9_-]+$/.test(s)) return null;
  try {
    return Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
  } catch {
    return null;
  }
}

// mintRenderToken returns base64url(expiry_be8 || HMAC(scope.expiry)).
export async function mintRenderToken(secret, scope, now = Date.now()) {
  const expiry = Math.floor(now / 1000) + RENDER_TTL_SECONDS;
  const mac = new Uint8Array(await crypto.subtle.sign("HMAC", await renderKey(secret), enc.encode(`${scope}.${expiry}`)));
  const out = new Uint8Array(8 + mac.length);
  new DataView(out.buffer).setBigUint64(0, BigInt(expiry));
  out.set(mac, 8);
  return b64url(out);
}

export async function verifyRenderToken(secret, scope, token, now = Date.now()) {
  const raw = token ? unb64url(token) : null;
  if (!raw || raw.length !== 40) return false;
  const expiry = Number(new DataView(raw.buffer, raw.byteOffset, 8).getBigUint64(0));
  if (Math.floor(now / 1000) > expiry) return false;
  return crypto.subtle.verify("HMAC", await renderKey(secret), raw.slice(8), enc.encode(`${scope}.${expiry}`));
}

async function shell(ctx, scope) {
  const res = await ctx.config.assets.fetch(new Request(new URL(SHELL_PATH, ctx.url)));
  if (!res.ok) throw new Error("page bundle missing");
  const source = await res.text();
  const token = await mintRenderToken(ctx.config.token, scope);
  const stamped = source.replace(PLACEHOLDER, `<meta name="wf-token" content="${token}" />`);
  if (stamped === source) throw new Error("page bundle has no token placeholder");
  return html(200, stamped);
}

async function overBudget(ctx, name) {
  if (!ctx.ip) return false;
  const { success } = await ctx.config.limits[name].limit({ key: ctx.ip });
  return !success;
}

// clientIP reads the configured header; behind a proxy that appends, the last entry is the one it saw.
function clientIP(request, header) {
  const raw = request.headers.get(header) ?? "";
  return raw.split(",").pop().trim();
}

function duplicateMessage(kind, name) {
  return /^[A-Za-z0-9_.[\]-]{1,64}$/.test(name)
    ? `The ${kind} ${name} appears more than once. Send it once per request.`
    : `A ${kind} appears more than once. Send each one once per request.`;
}

// gate refuses a repeated query parameter or render token, then a render token not minted for this page.
async function gate(ctx, scope, token) {
  const seen = new Set();
  const dups = [];
  for (const name of ctx.url.searchParams.keys()) {
    if (seen.has(name)) dups.push(name);
    seen.add(name);
  }
  if (dups.length) return abort(400, "duplicate_query_parameter", duplicateMessage("query parameter", dups.sort()[0]));
  const render = ctx.request.headers.get("X-Warmbly-Render") ?? "";
  // Headers joins repeated lines with a comma, which a render token never contains.
  if (render.includes(",")) return abort(400, "duplicate_header", duplicateMessage("header", "X-Warmbly-Render"));
  if (!LINK_TOKEN.test(token) || !(await verifyRenderToken(ctx.config.token, scope, render))) {
    return abort(403, "stale_page", "This page has been open too long. Reload it and try again.");
  }
  return null;
}

// readBody decodes a JSON object (or null) of at most max bytes.
async function readBody(request, max) {
  const declared = Number(request.headers.get("Content-Length") ?? "0");
  if (declared > max) return { error: "overflow" };
  const raw = new Uint8Array(await request.arrayBuffer());
  if (raw.byteLength > max) return { error: "overflow" };
  try {
    const value = JSON.parse(new TextDecoder().decode(raw));
    if (value === null) return { value: {} };
    if (typeof value !== "object" || Array.isArray(value)) return { error: "invalid" };
    return { value };
  } catch {
    return { error: "invalid" };
  }
}

const isStr = (v) => v === undefined || v === null || typeof v === "string";
const str = (v, max) => (typeof v === "string" ? v.slice(0, max) : "");

const SERVICE_FIELDS = { username: "string", password: "string", host: "string", port: "number", security: "string", tls_unverified: "boolean", tls_pin: "string" };
// service keeps the mail-server fields models.Service reads; undefined marks a shape it would refuse.
function service(v) {
  if (v === undefined || v === null) return null;
  if (typeof v !== "object" || Array.isArray(v)) return undefined;
  const out = {};
  for (const [name, type] of Object.entries(SERVICE_FIELDS)) {
    if (v[name] === undefined || v[name] === null) continue;
    if (typeof v[name] !== type || (type === "number" && !Number.isInteger(v[name]))) return undefined;
    out[name] = v[name];
  }
  return out;
}

// A vendor asks for a key and at most a few ids; a pick is bounded by the largest link.
const VENDOR_FIELDS_MAX = 8;
const VENDOR_PICK_MAX = 500;
// vendorFields keeps a flat map of short strings; undefined marks a shape the backend would refuse.
function vendorFields(v) {
  if (!v || typeof v !== "object" || Array.isArray(v)) return undefined;
  const entries = Object.entries(v);
  if (entries.length === 0 || entries.length > VENDOR_FIELDS_MAX) return undefined;
  const out = {};
  for (const [name, value] of entries) {
    if (typeof value !== "string" || name.length > 64 || value.length > 1024) return undefined;
    out[name] = value;
  }
  return out;
}
// mailboxIDs keeps a list of short ids; undefined marks a shape the backend would refuse.
function mailboxIDs(v) {
  if (!Array.isArray(v) || v.length === 0 || v.length > VENDOR_PICK_MAX) return undefined;
  return v.every((id) => typeof id === "string" && id.length > 0 && id.length <= 256) ? v : undefined;
}

// relayConnect passes the backend's verdict through as-is, or a fixed sentence when it could not be reached.
async function relayConnect(ctx, path, body) {
  try {
    const res = await backend(ctx.config, ctx.fetch, "/api/v1/internal/connect-links" + path, body);
    const raw = await res.text();
    if (res.status >= 500 || raw.length > BACKEND_MAX_BODY) throw new Error("backend failed");
    JSON.parse(raw);
    return json(res.status, raw);
  } catch {
    return json(503, CONNECT_UNAVAILABLE);
  }
}

async function connectAPI(ctx, token, action) {
  if (ctx.request.method !== (action === "" ? "GET" : "POST")) return apiNotFound();
  const refused = await gate(ctx, "connect:" + token, token);
  if (refused) return refused;
  const budget = action === "" ? "connectViews" : action === "/oauth/result" ? "connectPolls" : "connectActions";
  if (await overBudget(ctx, budget)) {
    return abort(429, "rate_limit_exceeded", "Too many attempts from this address. Try again later.", { "Retry-After": "60" });
  }
  const lookup = { token, host: ctx.host };
  if (action === "") return relayConnect(ctx, "/lookup", lookup);

  const max = action === "/vendor/import" ? VENDOR_IMPORT_MAX_BODY : action === "/smtp-imap" || action === "/vendor" ? CONNECT_MAX_BODY : ACTION_MAX_BODY;
  const { value: b, error } = await readBody(ctx.request, max);
  if (action === "/smtp-imap") {
    if (error === "overflow") return abort(413, "body_too_large", "The details are too large.");
    const smtp = b && service(b.smtp);
    const imap = b && service(b.imap);
    if (error || smtp === undefined || imap === undefined || !isStr(b.email) || !isStr(b.name) || !isStr(b.captcha_token)) {
      return abort(400, "invalid_body", "The details could not be read.");
    }
    return relayConnect(ctx, "/smtp-imap", {
      ...lookup, email: str(b.email, Infinity), name: str(b.name, Infinity), smtp, imap,
      captcha_token: str(b.captcha_token, Infinity), remote_ip: ctx.ip,
    });
  }
  if (action === "/vendor") {
    const fields = b && vendorFields(b.fields);
    if (error || fields === undefined || typeof b.vendor !== "string" || !isStr(b.captcha_token)) {
      return abort(400, "invalid_body", "The details could not be read.");
    }
    return relayConnect(ctx, "/vendor/mailboxes", {
      ...lookup, vendor: str(b.vendor, 32), fields, captcha_token: str(b.captcha_token, Infinity), remote_ip: ctx.ip,
    });
  }
  if (action === "/vendor/import") {
    if (error === "overflow") return abort(413, "body_too_large", "That is too many mailboxes at once.");
    const ids = b && mailboxIDs(b.mailbox_ids);
    if (error || ids === undefined || typeof b.ticket !== "string") return abort(400, "invalid_body", "The request could not be read.");
    return relayConnect(ctx, "/vendor/import", { ...lookup, ticket: str(b.ticket, 128), mailbox_ids: ids, remote_ip: ctx.ip });
  }
  if (action === "/oauth") {
    if (error || !isStr(b.provider)) return abort(400, "invalid_body", "The request could not be read.");
    return relayConnect(ctx, "/oauth/start", { ...lookup, provider: str(b.provider, 32), remote_ip: ctx.ip });
  }
  if (error || !isStr(b.result_key)) return abort(400, "invalid_body", "The request could not be read.");
  return relayConnect(ctx, "/oauth/result", { ...lookup, result_key: str(b.result_key, 128) });
}

// Report answers per host, token and range, kept briefly in this isolate only; a revoke takes effect within a minute.
const reports = new Map();
async function resolveReport(ctx, token, range) {
  const key = `${ctx.host}\0${token}\0${range}`;
  const hit = reports.get(key);
  if (hit && hit.expires > Date.now()) return hit.answer;
  const q = new URLSearchParams({ host: ctx.host });
  if (range) q.set("range", range);
  const res = await backend(ctx.config, ctx.fetch, `/api/v1/internal/reports/${encodeURIComponent(token)}?${q}`);
  let answer;
  if (res.status === 200) {
    const raw = await res.text();
    if (raw.length > BACKEND_MAX_BODY) throw new Error("report too large");
    JSON.parse(raw);
    answer = { found: true, body: raw };
  } else if (res.status === 404) {
    answer = { found: false };
  } else if (res.status === 400) {
    return { range: true };
  } else {
    throw new Error("report fetch failed");
  }
  if (reports.size >= REPORT_CACHE_MAX) reports.clear();
  reports.set(key, { answer, expires: Date.now() + (answer.found ? REPORT_TTL_MS : REPORT_MISS_TTL_MS) });
  return answer;
}

async function reportShell(ctx, token) {
  if (!LINK_TOKEN.test(token)) return html(404, REPORT_GONE_PAGE);
  // The shell resolves the report too, so it spends the same per-visitor budget as the JSON read.
  if (await overBudget(ctx, "reportViews")) return html(503, REPORT_DOWN_PAGE, { "Retry-After": "60" });
  let answer;
  try {
    answer = await resolveReport(ctx, token, "");
  } catch {
    return html(503, REPORT_DOWN_PAGE);
  }
  if (!answer.found) return html(answer.range ? 503 : 404, answer.range ? REPORT_DOWN_PAGE : REPORT_GONE_PAGE);
  return shell(ctx, "report:" + token);
}

async function reportAPI(ctx, token) {
  if (ctx.request.method !== "GET") return apiNotFound();
  const refused = await gate(ctx, "report:" + token, token);
  if (refused) return refused;
  if (await overBudget(ctx, "reportViews")) {
    return abort(429, "rate_limit_exceeded", "Too many requests from this address. Try again later.", { ...REPORT_API_HEADERS, "Retry-After": "60" });
  }
  const range = ctx.url.searchParams.get("range") ?? "";
  const badRange = () => abort(400, "invalid_range", "That date range is not one this report offers.", REPORT_API_HEADERS);
  if (range.length > 32) return badRange();
  let answer;
  try {
    answer = await resolveReport(ctx, token, range);
  } catch {
    return abort(503, "unavailable", "This report is temporarily unavailable. Try again in a few minutes.", REPORT_API_HEADERS);
  }
  if (answer.range) return badRange();
  if (!answer.found) return abort(404, "not_found", "This report is no longer shared.", REPORT_API_HEADERS);
  // Cached briefly by the visitor's browser, never by a shared cache.
  return json(200, answer.body, { ...REPORT_API_HEADERS, "Cache-Control": "private, max-age=60" });
}

async function loader(ctx) {
  const res = await ctx.config.assets.fetch(new Request(new URL(LOADER_PATH, ctx.url)));
  if (!res.ok) throw new Error("page bundle missing");
  return new Response(res.body, {
    headers: { ...NOSNIFF, "Content-Type": "application/javascript; charset=utf-8", "Cache-Control": "public, max-age=3600" },
  });
}

// workerd's disk service labels every file application/octet-stream, which nosniff makes a browser refuse to run.
const BUNDLE_TYPES = {
  js: "text/javascript; charset=utf-8", mjs: "text/javascript; charset=utf-8", css: "text/css; charset=utf-8",
  json: "application/json; charset=utf-8", map: "application/json; charset=utf-8", svg: "image/svg+xml",
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", webp: "image/webp", gif: "image/gif", ico: "image/x-icon",
  woff: "font/woff", woff2: "font/woff2", txt: "text/plain; charset=utf-8",
};

async function bundle(ctx) {
  const res = await ctx.config.assets.fetch(new Request(new URL(ctx.url.pathname, ctx.url), { method: ctx.request.method }));
  if (res.status === 404) return html(404, NOT_FOUND_PAGE);
  if (!res.ok) throw new Error("page bundle unavailable");
  // Hashed filenames, so the bundles are immutable by construction.
  const out = new Response(res.body, res);
  out.headers.set("Cache-Control", "public, max-age=31536000, immutable");
  out.headers.set("X-Content-Type-Options", "nosniff");
  const type = res.headers.get("Content-Type");
  const known = BUNDLE_TYPES[/\.([a-z0-9]+)$/i.exec(ctx.url.pathname)?.[1].toLowerCase()];
  if (known && (!type || type === "application/octet-stream")) out.headers.set("Content-Type", known);
  return out;
}

// finish keeps the visitor on their own host through an upstream redirect and never pins HSTS on their domain.
function finish(response, upstream, incoming) {
  const result = new Response(response.body, response);
  result.headers.delete("Strict-Transport-Security");
  const location = result.headers.get("Location");
  if (location) {
    const redirect = new URL(location, upstream);
    if (redirect.origin === upstream.origin) {
      redirect.protocol = incoming.protocol;
      redirect.host = incoming.host;
      result.headers.set("Location", redirect.href);
    }
  }
  return result;
}

// The dashboard build is static files; nothing the visitor carries is for it.
const STRIP = ["authorization", "cookie", "proxy-authorization", "host"];

async function dashboard(ctx) {
  const upstream = new URL(ctx.url.pathname + ctx.url.search, ctx.config.dashboard);
  const headers = new Headers(ctx.request.headers);
  for (const name of STRIP) headers.delete(name);
  const response = await ctx.fetch(new Request(upstream, { method: ctx.request.method, headers, redirect: "manual" }));
  return finish(response, upstream, ctx.url);
}

// forward passes a request on as it came: method, URL (so the Host), headers and body. The socket in front
// consumed X-Forwarded-Proto into the URL's scheme, so it is put back.
function forward(request, service) {
  const headers = new Headers(request.headers);
  if (!headers.has("X-Forwarded-Proto")) headers.set("X-Forwarded-Proto", new URL(request.url).protocol.slice(0, -1));
  return service.fetch(new Request(request, { headers, redirect: "manual" }));
}

const readOnly = (method) => method === "GET" || method === "HEAD";
const headOnly = (method, response) => (method === "HEAD" ? new Response(null, response) : response);

export async function servePortal(request, config, fetchOrigin = fetch, cache = undefined) {
  if (!config) return notFound();
  if (!config.shared) return forward(request, config.fallback);
  const url = new URL(request.url);
  const host = url.hostname.toLowerCase().replace(/\.$/, "");
  // The shared address resolves any workspace's links and has no dashboard; anything else must be a verified portal.
  const shared = host === config.shared;
  if (!shared) {
    if (host === config.api.hostname || host === config.dashboard?.hostname) return notFound();
    // A failed lookup throws, so an outage is a 503 and never reaches the fallback.
    if (!(await isPortal(host, config, fetchOrigin, cache))) return config.fallback ? forward(request, config.fallback) : notFound();
  }
  // Wrapped: the runtime's fetch refuses to be called as a method of anything but the global scope.
  const ctx = { request, url, host, config, fetch: (req) => fetchOrigin(req), ip: clientIP(request, config.ipHeader) };
  const route = pathKind(url.pathname);
  switch (route.kind) {
    case "connect-api":
      return connectAPI(ctx, route.token, route.action);
    case "report-api":
      return reportAPI(ctx, route.token);
    case "api-none":
      return apiNotFound();
  }
  if (!readOnly(request.method)) return methodNotAllowed();
  switch (route.kind) {
    case "loader":
      return headOnly(request.method, await loader(ctx));
    case "bundle":
      return bundle(ctx);
    case "connect":
      return headOnly(request.method, LINK_TOKEN.test(route.token) ? await shell(ctx, "connect:" + route.token) : html(404, NOT_FOUND_PAGE));
    case "report":
      return headOnly(request.method, await reportShell(ctx, route.token));
    case "dashboard":
      if (!shared && config.dashboard) return dashboard(ctx);
      return headOnly(request.method, html(404, NOT_FOUND_PAGE));
    default:
      return headOnly(request.method, html(404, NOT_FOUND_PAGE));
  }
}

// handle is the fetch handler index.mjs exports; any failure, including a bad configuration, is a plain 503.
export async function handle(request, env) {
  try {
    return await servePortal(request, readConfig(env), fetch, lookupCache(globalThis.caches?.default));
  } catch {
    return failure();
  }
}
