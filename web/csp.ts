import { AsyncLocalStorage } from "node:async_hooks";
import { createHash, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import type { Plugin } from "vite";
import { dashboardCsp, dashboardSecurityHeaders, mailPreviewCsp } from "./securityHeaders";

export const noncePlaceholder = "__WARMBLY_DEV_NONCE__";

export function inlineHashes(html: string, tag: "script" | "style") {
    return [...html.matchAll(new RegExp(`<${tag}\\b([^>]*)>([\\s\\S]*?)</${tag}>`, "gi"))]
        .filter((match) => tag !== "script" || !/(?:^|\s)src\s*=/i.test(match[1]))
        .map((match) => `'sha256-${createHash("sha256").update(match[2]).digest("base64")}'`);
}

export function origin(value = "") {
    if (!value || value.startsWith("/")) return "";
    const url = new URL(value);
    if (!['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol)) throw new Error("CSP resources require HTTP or WebSocket URLs");
    if (!/^([a-zA-Z0-9._-]+|\[[a-fA-F0-9:]+\])$/.test(url.hostname)) throw new Error("Invalid CSP resource host");
    return url.origin;
}

export function configuredSources(env: Record<string, string>) {
    const api = origin(env.WARMBLY_API_URL || env.VITE_API_URL);
    const sentry = origin(env.WARMBLY_SENTRY_DSN || env.VITE_SENTRY_DSN);
    const posthog = origin(env.WARMBLY_POSTHOG_HOST || env.VITE_POSTHOG_HOST || "https://us.i.posthog.com");
    const assets = posthog.replace(/\/(us|eu)\.i\.posthog\.com$/, "/$1-assets.i.posthog.com");
    const additional = (env.WARMBLY_CSP_CONNECT_ORIGINS || env.VITE_CSP_CONNECT_ORIGINS || "").split(/\s+/).filter(Boolean).map(origin);
    return {
        connections: [...new Set([api, api.replace(/^http/, "ws"), sentry, ...additional].filter(Boolean))],
        analytics: [...new Set([posthog, assets].filter(Boolean))],
        images: [api].filter(Boolean),
        frames: [api].filter(Boolean),
    };
}

export function dashboardCspPlugin(env: Record<string, string>): Plugin {
    const requests = new AsyncLocalStorage<{ nonce: string }>();
    const sources = configuredSources(env);
    return {
        name: "warmbly:dashboard-csp",
        configureServer(server) {
            const transformHtml = server.transformIndexHtml.bind(server);
            server.transformIndexHtml = async (...args) => {
                const request = requests.getStore();
                const html = await transformHtml(...args);
                // Vite adds nonce attributes after user HTML hooks have finished.
                return request ? html.replaceAll(noncePlaceholder, request.nonce) : html;
            };
            server.middlewares.use((request, response, next) => {
                const nonce = randomBytes(24).toString("base64");
                const preview = request.url?.split("?")[0] === "/mail-preview.html";
                for (const [name, value] of Object.entries(dashboardSecurityHeaders)) response.setHeader(name, value);
                response.setHeader("Content-Security-Policy", preview ? mailPreviewCsp : dashboardCsp({ ...sources, nonce, connections: [...sources.connections, origin(server.resolvedUrls?.local[0]), origin(server.resolvedUrls?.local[0]).replace(/^http/, "ws")] }));
                if (preview) response.setHeader("X-Frame-Options", "SAMEORIGIN");
                requests.run({ nonce }, next);
            });
        },
        generateBundle: { order: "post", handler(_options, bundle) {
            const index = bundle["index.html"];
            if (!index || index.type !== "asset") throw new Error("CSP requires the dashboard HTML asset");
            const html = String(index.source);
            this.emitFile({ type: "asset", fileName: "csp-template.txt", source: dashboardCsp({
                scripts: inlineHashes(html, "script"),
                styles: inlineHashes(html, "style"),
                connections: ["__API_SOURCES__", "__SENTRY_SOURCE__", "__CONNECT_SOURCES__"],
                analytics: ["__ANALYTICS_SOURCES__"],
                images: ["__API_IMAGE_SOURCE__"],
                frames: ["__API_IMAGE_SOURCE__"],
            }) });
            this.emitFile({ type: "asset", fileName: "csp-origins.txt", source: [
                origin(env.VITE_API_URL),
                origin(env.VITE_SENTRY_DSN),
                origin(env.VITE_POSTHOG_HOST),
                (env.VITE_CSP_CONNECT_ORIGINS || "").split(/\s+/).filter(Boolean).map(origin).join(" "),
            ].join("\n") });
        } },
        configurePreviewServer(server) {
            const directory = path.resolve(server.config.root, server.config.build.outDir);
            let policy: string;
            try {
                policy = readFileSync(path.join(directory, "csp-policy.txt"), "utf8").trim();
            } catch {
                const html = readFileSync(path.join(directory, "index.html"), "utf8");
                policy = dashboardCsp({ ...sources, scripts: inlineHashes(html, "script"), styles: inlineHashes(html, "style") });
            }
            server.middlewares.use((request, response, next) => {
                const preview = request.url?.split("?")[0] === "/mail-preview.html";
                for (const [name, value] of Object.entries(dashboardSecurityHeaders)) response.setHeader(name, value);
                response.setHeader("Content-Security-Policy", preview ? mailPreviewCsp : policy);
                if (preview) response.setHeader("X-Frame-Options", "SAMEORIGIN");
                next();
            });
        },
    };
}
