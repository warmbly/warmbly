// @vitest-environment node

import { createHash } from "node:crypto";
import { execFile, execFileSync } from "node:child_process";
import { createServer as createHttpServer } from "node:http";
import { promisify } from "node:util";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { build, createServer } from "vite";
import { configuredSources, dashboardCspPlugin, inlineHashes, noncePlaceholder, origin } from "../csp";
import { dashboardCsp, mailPreviewCsp } from "../securityHeaders";

const directory = process.cwd();

describe("dashboard CSP", () => {
    it("allows configured images but not arbitrary remote hosts on dashboard routes", () => {
        const images = dashboardCsp({ images: ["https://api.example.test"] }).split("; ").find((directive) => directive.startsWith("img-src "));
        expect(images).toBe("img-src 'self' data: blob: https://api.example.test");
        expect(images).not.toMatch(/(?:^|\s)(?:https?:|\*)\s/);
        expect(mailPreviewCsp).toContain("script-src 'none'");
        expect(mailPreviewCsp).toContain("img-src data: https: http:");
    });
    it("frames the API origin for the captcha bridge and nothing else", () => {
        const frames = dashboardCsp({ frames: ["https://api.example.test"] }).split("; ").find((directive) => directive.startsWith("frame-src "));
        expect(frames).toBe("frame-src 'self' https://challenges.cloudflare.com https://api.example.test");
        expect(dashboardCsp().split("; ")).toContain("frame-src 'self' https://challenges.cloudflare.com");
        expect(configuredSources({ VITE_API_URL: "https://api.example.test/v1" }).frames).toEqual(["https://api.example.test"]);
    });
    it("serves final Vite HTML with a matching fresh nonce for concurrent requests", async () => {
        const temporary = mkdtempSync(path.join(tmpdir(), "warmbly-csp-dev-"));
        const server = await createServer({ root: temporary, configFile: false, html: { cspNonce: noncePlaceholder }, plugins: [dashboardCspPlugin({})], server: { host: "127.0.0.1", port: 0 }, logLevel: "silent" });
        try {
            writeFileSync(path.join(temporary, "index.html"), '<html><head><script>window.theme="dark";</script><style>body { margin: 0; }</style></head><body></body></html>');
            await server.listen();
            const address = server.httpServer!.address();
            if (!address || typeof address === "string") throw new Error("Expected local HTTP port");
            const results = await Promise.all([1, 2].map(async () => {
                const response = await fetch(`http://127.0.0.1:${address.port}/app/contacts`);
                expect(response.status).toBe(200);
                const nonce = response.headers.get("Content-Security-Policy")?.match(/'nonce-([^']+)'/)?.[1];
                const html = await response.text();
                expect(nonce).toBeTruthy();
                expect(html).not.toContain(noncePlaceholder);
                const nonces = [...html.matchAll(/nonce="([^"]+)"/g)].map((match) => match[1]);
                expect(nonces.length).toBeGreaterThanOrEqual(3);
                expect(new Set(nonces)).toEqual(new Set([nonce]));
                return nonce;
            }));
            expect(results[0]).not.toBe(results[1]);
        } finally {
            await server.close();
            rmSync(temporary, { recursive: true, force: true });
        }
    });

    it("generates hashes from Vite's final HTML asset", async () => {
        const temporary = mkdtempSync(path.join(tmpdir(), "warmbly-csp-build-"));
        try {
            writeFileSync(path.join(temporary, "index.html"), '<html><head><script>window.theme="dark";</script><style>body { margin: 0; }</style></head><body></body></html>');
            await build({ root: temporary, configFile: false, plugins: [dashboardCspPlugin({ VITE_API_URL: "https://built-api.test" })], logLevel: "silent" });
            const html = readFileSync(path.join(temporary, "dist/index.html"), "utf8");
            const template = readFileSync(path.join(temporary, "dist/csp-template.txt"), "utf8");
            for (const hash of [...inlineHashes(html, "script"), ...inlineHashes(html, "style")]) expect(template).toContain(hash);
            expect(readFileSync(path.join(temporary, "dist/csp-origins.txt"), "utf8")).toContain("https://built-api.test");
            expect(template).not.toContain(noncePlaceholder);
        } finally {
            rmSync(temporary, { recursive: true, force: true });
        }
    });

    it("hashes exact inline text, not external scripts or attributes", () => {
        const source = "\nwindow.theme = 'dark';\n";
        const hash = `'sha256-${createHash("sha256").update(source).digest("base64")}'`;
        expect(inlineHashes(`<script data-src="example">${source}</script><script src="/app.js"></script>`, "script")).toEqual([hash]);
        expect(inlineHashes(`<style>body{margin:0}</style>`, "style")).toHaveLength(1);
    });

    it("denies unlisted resource types, inline scripts and style elements", () => {
        const policy = dashboardCsp();
        expect(policy).toContain("default-src 'none'");
        expect(policy).toContain("script-src-attr 'none'");
        expect(policy).toContain("style-src 'self';");
        expect(policy).not.toContain("script-src 'unsafe-inline'");
        expect(policy).not.toContain("'unsafe-eval'");
        expect(policy).not.toContain("*");
    });

    it("keeps email markup styling in a separately script-disabled document", () => {
        expect(mailPreviewCsp).toContain("script-src 'none'");
        expect(mailPreviewCsp).toContain("frame-ancestors 'self'");
        expect(mailPreviewCsp).toContain("style-src 'unsafe-inline'");
        expect(readFileSync(path.join(directory, "public/_headers"), "utf8")).toContain(`/mail-preview.html\n  ! Content-Security-Policy\n  Content-Security-Policy: ${mailPreviewCsp}`);
        expect(readFileSync(path.join(directory, "nginx-mail-preview-headers.conf"), "utf8")).toContain(`Content-Security-Policy "${mailPreviewCsp}"`);
    });

    it("removes wildcard CORS from dashboard static responses", () => {
        const headers = readFileSync(path.join(directory, "public/_headers"), "utf8");
        expect(headers).toContain("/*\n  ! Access-Control-Allow-Origin\n");
        expect(headers).not.toMatch(/^\s+Access-Control-Allow-Origin:\s*\*/m);
    });

    it("derives exact resource origins and permits separately configured realtime", () => {
        expect(configuredSources({ VITE_API_URL: "https://api.example.test/v1", VITE_POSTHOG_HOST: "https://eu.i.posthog.com", WARMBLY_CSP_CONNECT_ORIGINS: "wss://realtime.example.test/socket" })).toEqual({
            connections: ["https://api.example.test", "wss://api.example.test", "wss://realtime.example.test"],
            analytics: ["https://eu.i.posthog.com", "https://eu-assets.i.posthog.com"],
            images: ["https://api.example.test"],
            frames: ["https://api.example.test"],
        });
        expect(origin("https://public-key@sentry.example.test/123")).toBe("https://sentry.example.test");
        expect(origin("/ingest")).toBe("");
        expect(() => origin("javascript:alert(1)")).toThrow();
        expect(() => origin("https://example.test;script-src/*")).toThrow();
    });

    it("renders the same hashes and origins into Pages and nginx deployment output", () => {
        const temporary = mkdtempSync(path.join(tmpdir(), "warmbly-csp-"));
        try {
            const html = readFileSync(path.join(directory, "index.html"), "utf8");
            const scripts = inlineHashes(html, "script");
            const styles = inlineHashes(html, "style");
            writeFileSync(path.join(temporary, "csp-template.txt"), dashboardCsp({ scripts, styles, connections: ["__API_SOURCES__", "__SENTRY_SOURCE__", "__CONNECT_SOURCES__"], analytics: ["__ANALYTICS_SOURCES__"], images: ["__API_IMAGE_SOURCE__"] }));
            writeFileSync(path.join(temporary, "csp-origins.txt"), "https://built-api.test\nhttps://built-sentry.test\nhttps://eu.i.posthog.com\nwss://built-realtime.test\n");
            writeFileSync(path.join(temporary, "_headers"), readFileSync(path.join(directory, "public/_headers")));
            const env = { PATH: process.env.PATH, WARMBLY_CONFIG_OUT: path.join(temporary, "config.js"), WARMBLY_API_URL: "https://deployed-api.test", WARMBLY_CSP_CONNECT_ORIGINS: "wss://deployed-realtime.test", WEBSOCKET_URL: "wss://deployed-realtime.test" };
            execFileSync("sh", [path.join(directory, "docker-entrypoint.sh")], { env });
            const policy = readFileSync(path.join(temporary, "csp-policy.txt"), "utf8").trim();
            expect(policy).toContain("https://deployed-api.test wss://deployed-api.test");
            expect(policy).toContain("https://built-sentry.test");
            expect(policy).toContain("https://eu-assets.i.posthog.com");
            expect(policy).toContain("wss://deployed-realtime.test");
            expect(policy).not.toContain("built-api.test");
            expect(policy).not.toContain("__");
            for (const hash of [...scripts, ...styles]) expect(policy).toContain(hash);
            const headers = readFileSync(path.join(temporary, "_headers"), "utf8");
            expect(headers).toContain("/*\n  ! Access-Control-Allow-Origin\n");
            expect(headers).toContain(`Content-Security-Policy: ${policy}`);
            expect(headers).toContain(`Content-Security-Policy: ${mailPreviewCsp}`);
            execFileSync("sh", [path.join(directory, "docker-entrypoint.sh")], { env });
            expect(readFileSync(path.join(temporary, "_headers"), "utf8")).toBe(headers);
            expect(() => execFileSync("sh", [path.join(directory, "docker-entrypoint.sh")], { env: { ...env, WARMBLY_API_URL: "https://example.test;script-src *" }, stdio: "pipe" })).toThrow();
        } finally {
            rmSync(temporary, { recursive: true, force: true });
        }
    });

    it("discovers legacy realtime configuration before rendering static CSP and retries backend readiness", async () => {
        const temporary = mkdtempSync(path.join(tmpdir(), "warmbly-csp-discovery-"));
        let requests = 0;
        let payload = JSON.stringify({ websocket_url: "wss://ws.selfhost.test/socket/websocket?token=never-in-csp" });
        let status = 200;
        const server = createHttpServer((request, response) => {
            expect(request.url).toBe("/v1/auth/config");
            expect(request.headers.authorization).toBeUndefined();
            requests++;
            response.writeHead(requests === 1 ? 503 : status, { "Content-Type": "application/json" });
            response.end(payload);
        });
        try {
            await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
            const address = server.address();
            if (!address || typeof address === "string") throw new Error("Expected local HTTP port");
            const base = `http://127.0.0.1:${address.port}`;
            writeFileSync(path.join(temporary, "csp-template.txt"), dashboardCsp({ connections: ["__API_SOURCES__", "__CONNECT_SOURCES__"] }));
            writeFileSync(path.join(temporary, "_headers"), readFileSync(path.join(directory, "public/_headers")));
            const env = { PATH: process.env.PATH, WARMBLY_CONFIG_OUT: path.join(temporary, "config.js"), WARMBLY_API_URL: `${base}/v1/` };
            const render = (extra = {}) => promisify(execFile)("sh", [path.join(directory, "docker-entrypoint.sh")], { env: { ...env, ...extra } });
            const policy = () => readFileSync(path.join(temporary, "csp-policy.txt"), "utf8");
            await render();
            expect(requests).toBe(2);
            expect(policy()).toContain("wss://ws.selfhost.test");
            expect(policy()).not.toContain("token");
            expect(readFileSync(path.join(temporary, "_headers"), "utf8")).toContain(`Content-Security-Policy: ${policy().trim()}`);
            payload = JSON.stringify({ websocket_url: "wss://changed.selfhost.test/socket" });
            await render({ WARMBLY_API_URL: base });
            expect(policy()).toContain("wss://changed.selfhost.test");
            expect(policy()).not.toContain("wss://ws.selfhost.test");
            for (const invalid of ["wss://host.test;script-src *", "javascript:alert(1)", 42]) {
                payload = JSON.stringify({ websocket_url: invalid });
                const result = await render({ WARMBLY_CSP_CONNECT_ORIGINS: "wss://explicit.test" });
                expect(result.stderr).toMatch(/CSP|realtime|websocket_url/i);
                expect(policy()).toContain("wss://explicit.test");
                expect(policy()).not.toMatch(/script-src \*|javascript:|host\.test/);
            }
            payload = "not JSON";
            await render();
            expect(policy()).not.toContain("changed.selfhost.test");
            payload = "{}";
            expect((await render()).stderr).toBe("");
            status = 404;
            const fallback = await render({ WARMBLY_CSP_CONNECT_ORIGINS: "wss://offline.test" });
            expect(fallback.stderr).toContain("Realtime CSP discovery unavailable");
            expect(policy()).toContain("wss://offline.test");
            const before = requests;
            await render({ WEBSOCKET_URL: "wss://direct.test/socket/websocket" });
            expect(requests).toBe(before);
            expect(policy()).toContain("wss://direct.test");
            expect(policy()).not.toContain("*");
            await render({ WEBSOCKET_URL: "wss://direct.test/socket", WARMBLY_CSP_CONNECT_ORIGINS: "wss://direct.test" });
            expect(policy().match(/wss:\/\/direct\.test/g)).toHaveLength(1);
            status = 200;
            payload = JSON.stringify({ websocket_url: "wss://built-config.test/socket" });
            writeFileSync(path.join(temporary, "csp-origins.txt"), `${base}\n\n\n\n`);
            await render({ WARMBLY_API_URL: "" });
            expect(policy()).toContain("wss://built-config.test");
        } finally {
            await new Promise<void>((resolve) => server.close(() => resolve()));
            rmSync(temporary, { recursive: true, force: true });
        }
    }, 15000);
});
