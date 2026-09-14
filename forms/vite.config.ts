import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Source maps are emitted only when the image build is going to upload them to
// PostHog (both CLI variables set), the same rule as the dashboard: a fork or
// a self-host build sets neither, needs no account anywhere, and ships a
// bundle without them. The upload itself is the `sourcemaps:posthog` script,
// which also deletes the .map files it sent.
const uploadSourceMaps = Boolean(process.env.POSTHOG_CLI_API_KEY && process.env.POSTHOG_CLI_PROJECT_ID);

// The built app is served by the Go forms service (cmd/forms), which owns
// /f/<publicId> shells, per-form CSP headers and the same-origin /api. The
// dev server proxies /api to a locally running `make forms` service.
export default defineConfig({
    plugins: [react()],
    build: {
        sourcemap: uploadSourceMaps,
    },
    server: {
        port: 5175,
        proxy: {
            "/api": {
                target: process.env.FORMS_API_URL ?? "http://localhost:8090",
                changeOrigin: true,
            },
        },
    },
});
