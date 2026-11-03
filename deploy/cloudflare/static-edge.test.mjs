import assert from "node:assert/strict";
import test from "node:test";
import worker, { serveStatic } from "./static-edge.mjs";

const origins = { "stage.warmbly.com": "https://example.pages.dev" };
const request = (path = "/", options) => new Request("https://stage.warmbly.com" + path, options);

test("static paths, query strings and headers retain their public response", async () => {
  const response = await serveStatic(request("/assets/app.js?v=74", {
    headers: { Authorization: "Bearer fixture", Cookie: "session=fixture", "If-None-Match": "fixture-etag" },
  }), origins, async (upstream) => {
    assert.equal(upstream.url, "https://example.pages.dev/assets/app.js?v=74");
    assert.equal(upstream.redirect, "manual");
    assert.equal(upstream.headers.get("Authorization"), null);
    assert.equal(upstream.headers.get("Cookie"), null);
    assert.equal(upstream.headers.get("If-None-Match"), "fixture-etag");
    return new Response("asset", { headers: {
      "Content-Security-Policy": "default-src 'self'", "X-Content-Type-Options": "nosniff",
      "Cache-Control": "public, max-age=300", ETag: "fixture-etag",
    } });
  });
  assert.equal(await response.text(), "asset");
  assert.equal(response.headers.get("Content-Security-Policy"), "default-src 'self'");
  assert.equal(response.headers.get("X-Content-Type-Options"), "nosniff");
  assert.equal(response.headers.get("ETag"), "fixture-etag");
});

test("an unknown host cannot select an origin", async () => {
  const r = await serveStatic(new Request("https://unlisted.warmbly.com/?origin=https://attacker.example"), origins,
    () => assert.fail("unknown host fetched"));
  assert.equal(r.status, 404);
});

test("write methods are refused before any origin request", async () => {
  for (const method of ["POST", "PUT", "PATCH", "DELETE"]) {
    const r = await serveStatic(request("/", { method }), origins, () => assert.fail("write fetched"));
    assert.equal(r.status, 405);
    assert.equal(r.headers.get("Allow"), "GET, HEAD, OPTIONS");
  }
});

test("HEAD and OPTIONS retain their methods", async () => {
  for (const method of ["HEAD", "OPTIONS"]) {
    const r = await serveStatic(request("/", { method }), origins, async (upstream) => {
      assert.equal(upstream.method, method);
      return new Response(null, { status: 204 });
    });
    assert.equal(r.status, 204);
  }
});

test("configuration admits only HTTPS Pages origins with no credentials or path", async () => {
  for (const target of ["http://example.pages.dev", "https://example.com", "https://example.pages.dev.attacker.example",
    "https://user:pass@example.pages.dev", "https://example.pages.dev/path", "https://example.pages.dev?q=1",
    "https://example.pages.dev#fragment", "https://example.pages.dev:8443"]) {
    const r = await serveStatic(request(), { "stage.warmbly.com": target }, () => assert.fail("invalid origin fetched"));
    assert.equal(r.status, 503, target);
  }
});

test("Pages canonical redirects keep the visitor on the public hostname", async () => {
  for (const location of ["https://example.pages.dev/docs/?q=1", "/docs/?q=1"]) {
    const r = await serveStatic(request(), origins, async () => new Response(null, { status: 308, headers: { Location: location } }));
    assert.equal(r.status, 308);
    assert.equal(r.headers.get("Location"), "https://stage.warmbly.com/docs/?q=1");
  }
});

test("external redirects and missing pages preserve the origin response", async () => {
  const redirect = await serveStatic(request(), origins, async () => new Response(null, { status: 302, headers: { Location: "https://warmbly.com/terms/" } }));
  assert.equal(redirect.headers.get("Location"), "https://warmbly.com/terms/");
  const missing = await serveStatic(request("/missing"), origins, async () => new Response("not found", { status: 404 }));
  assert.equal(missing.status, 404);
  assert.equal(await missing.text(), "not found");
});

test("configuration and network failures return one generic response", async (t) => {
  t.mock.method(globalThis, "fetch", async () => { throw Error("private origin exception"); });
  for (const value of [undefined, "not json", "null", JSON.stringify(origins)]) {
    const r = await worker.fetch(request(), { PAGES_ORIGINS: value });
    assert.equal(r.status, 503);
    assert.equal(await r.text(), "Service temporarily unavailable.");
    assert.equal(r.headers.get("Cache-Control"), "no-store");
  }
});
