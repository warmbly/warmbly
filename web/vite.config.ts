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
    // Pre-bundle heavy deps so the first request to the dev server
    // doesn't trigger a cold compile of axios/framer-motion/etc. This
    // shaves ~300-500ms off the first paint locally.
    optimizeDeps: {
        include: [
            "react",
            "react-dom",
            "react-router-dom",
            "axios",
            "framer-motion",
            "@tanstack/react-query",
            "@tanstack/react-query-devtools",
            "react-hot-toast",
            "lucide-react",
            "@remixicon/react",
        ],
    },
    server: {
        // Warm up the most-mounted entry points before the user
        // clicks them so navigation doesn't trigger a cold compile.
        warmup: {
            clientFiles: [
                "./src/main.tsx",
                "./src/app/app/layout.tsx",
                "./src/app/app/emails/page.tsx",
                "./src/app/app/campaigns/page.tsx",
                "./src/app/app/contacts/page.tsx",
            ],
        },
    },
});
