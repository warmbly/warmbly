import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
    plugins: [
        react(),
        tailwindcss(),
    ],
    resolve: {
        alias: {
            "@": path.resolve(__dirname, "./src"),
        },
    },
    // Pre-bundle the heaviest deps so the first request doesn't pay
    // the cold-compile cost. Same intent as web/, lighter list because
    // the admin app doesn't pull in tiptap/dnd-kit/etc.
    optimizeDeps: {
        include: [
            "react",
            "react-dom",
            "react-router-dom",
            "axios",
            "@tanstack/react-query",
            "@tanstack/react-query-devtools",
            "lucide-react",
        ],
    },
    server: {
        // Run on a non-default port so it coexists with the dashboard
        // dev server (5173) without a port collision when both are up.
        port: 5174,
        strictPort: false,
        // Permit Tailscale MagicDNS names (and extras via VITE_ALLOWED_HOSTS)
        // when exposed with --host. IPs + localhost are always allowed, so
        // this is inert for normal local dev.
        allowedHosts: [".ts.net", ...(process.env.VITE_ALLOWED_HOSTS?.split(",").filter(Boolean) ?? [])],
    },
});
