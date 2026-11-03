import assert from "node:assert/strict";
import { cpSync, existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { staticAssetsMatrix, prepareStaticAssets } from "./pages-static-assets.mjs";

const projects = JSON.stringify([{ app: "web", project: "dashboard" }, { app: "admin", project: "admin" }, { app: "docs", project: "docs" }]);
const configured = [{ project: "dashboard", worker: "warmbly-dashboard", domains: ["stage.warmbly.com"] }];
const sha = "a".repeat(40);

test("static assets are disabled unless exact Pages projects are configured", () => {
  assert.deepEqual(staticAssetsMatrix(projects), { include: [] });
  const target = staticAssetsMatrix(projects, JSON.stringify(configured)).include[0];
  assert.equal(target.output_dir, "web/dist");
  assert.equal(target.environment, "dashboard-production");
  assert.equal(target.worker, "warmbly-dashboard");
  assert.deepEqual(target.domains, ["stage.warmbly.com"]);
});

test("rejects unknown projects, paths, domains, duplicate mappings and overrides", () => {
  for (const value of [null, {}, [null], [{ ...configured[0], project: "unknown" }], [{ ...configured[0], output_dir: "/tmp" }], [{ ...configured[0], worker: "../worker" }], [{ ...configured[0], domains: ["*.warmbly.com"] }], [{ ...configured[0], domains: ["stage.warmbly.com/path"] }], [{ ...configured[0], domains: ["stage.warmbly.com", "stage.warmbly.com"] }], [configured[0], configured[0]], [configured[0], { project: "admin", worker: "admin", domains: ["stage.warmbly.com"] }]]) {
    assert.throws(() => staticAssetsMatrix(projects, JSON.stringify(value)));
  }
});

function fixture(app, fn) {
  const root = mkdtempSync(join(tmpdir(), "static-assets-"));
  try {
    const target = staticAssetsMatrix(projects, JSON.stringify([{ project: app === "web" ? "dashboard" : app, worker: "fixture", domains: [] }])).include[0];
    const output = join(root, target.output_dir);
    mkdirSync(output, { recursive: true });
    writeFileSync(join(output, "index.html"), "<h1>Warmbly fixture</h1>");
    for (const name of ["_headers", "_redirects", "404.html"]) {
      try { cpSync(new URL(`../../${app}/public/${name}`, import.meta.url), join(output, name)); } catch (error) { if (error.code !== "ENOENT") throw error; }
    }
    fn({ root, target, output, destination: join(root, "artifact") });
  } finally { rmSync(root, { recursive: true, force: true }); }
}

test("publishes only assets and preserves exact route/header/404 files and release identity", () => {
  fixture("web", ({ root, target, output, destination }) => {
    const config = prepareStaticAssets({ target, sourceRoot: root, destination, sourceSha: sha, release: "v0.6.74" });
    assert.equal(config.main, undefined);
    assert.equal(config.workers_dev, false);
    assert.equal(config.preview_urls, false);
    assert.equal(config.assets.not_found_handling, "404-page");
    assert.deepEqual(config.routes, []);
    for (const name of ["index.html", "_headers", "_redirects", "404.html"]) assert.deepEqual(readFileSync(join(output, name)), readFileSync(join(destination, "assets", name)));
    assert.deepEqual(JSON.parse(readFileSync(join(destination, "assets/.well-known/warmbly-release.json"))), { release: "v0.6.74", sourceSha: sha, app: "web", project: "dashboard" });
    assert.throws(() => prepareStaticAssets({ target, sourceRoot: root, destination, sourceSha: sha, release: "v0.6.74" }));
  });
});

test("admin preserves its SPA fallback and domains are explicit custom-domain bindings", () => {
  fixture("admin", ({ root, target, destination }) => {
    const config = prepareStaticAssets({ target: { ...target, domains: ["admin-stage.warmbly.com"] }, sourceRoot: root, destination, sourceSha: sha, release: "v0.6.74" });
    assert.equal(config.assets.not_found_handling, "single-page-application");
    assert.equal(existsSync(join(destination, "assets", "_redirects")), false);
    assert.deepEqual(config.routes, [{ pattern: "admin-stage.warmbly.com", custom_domain: true }]);
  });
});

test("refuses unpinned releases, executable Pages functions and symlink artifacts", () => {
  for (const kind of ["sha", "tag", "function", "symlink"]) fixture("web", ({ root, target, output, destination }) => {
    if (kind === "function") writeFileSync(join(output, "_worker.js"), "export default {}");
    if (kind === "symlink") symlinkSync("/etc/hosts", join(output, "outside"));
    assert.throws(() => prepareStaticAssets({ target, sourceRoot: root, destination, sourceSha: kind === "sha" ? "main" : sha, release: kind === "tag" ? "latest" : "v0.6.74" }));
  });
});

test("changed admin redirect behavior requires explicit review", () => {
  fixture("admin", ({ root, target, output, destination }) => {
    writeFileSync(join(output, "_redirects"), "/old /new 301\n");
    assert.throws(() => prepareStaticAssets({ target, sourceRoot: root, destination, sourceSha: sha, release: "v0.6.74" }));
  });
});

test("requires regular frontend entrypoint and response header files", () => {
  for (const name of ["index.html", "_headers"]) fixture("web", ({ root, target, output, destination }) => {
    rmSync(join(output, name));
    mkdirSync(join(output, name));
    assert.throws(() => prepareStaticAssets({ target, sourceRoot: root, destination, sourceSha: sha, release: "v0.6.74" }), /Frontend index and response headers required/);
    assert.equal(existsSync(destination), false);
  });
});
