import { readFileSync } from "node:fs";
import { join } from "node:path";

import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// The portal service (portal/worker) serves dist/ as its page bundle: everything under /_portal/, so on an
// agency portal domain none of it shadows the dashboard's own /assets/.
const outDir = "dist/_portal";
const PLACEHOLDER = '<meta name="wf-token" content="" />';

// The service stamps the render token into this tag; a shell without it would answer every page with 503.
function requireTokenPlaceholder(): Plugin {
    return {
        name: "require-token-placeholder",
        apply: "build",
        closeBundle() {
            if (!readFileSync(join(outDir, "index.html"), "utf8").includes(PLACEHOLDER)) {
                throw new Error("the built shell has no wf-token placeholder");
            }
        },
    };
}

// `vite dev` serves from the root, so /connect/<token> and /r/<token> open as they do on the service.
export default defineConfig(({ command }) => ({
    base: command === "build" ? "/_portal/" : "/",
    plugins: [react(), requireTokenPlaceholder()],
    build: { outDir, emptyOutDir: true },
    server: {
        port: 5176,
        proxy: {
            "/api": {
                target: process.env.PORTAL_API_URL ?? "http://localhost:8787",
                changeOrigin: true,
            },
        },
    },
}));
