// @ts-check
import { defineConfig } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';
import sitemap from '@astrojs/sitemap';
import mdx from '@astrojs/mdx';

// https://astro.build/config
export default defineConfig({
  site: 'https://warmbly.com',
  integrations: [
    mdx(),
    sitemap({
      // The 404 page is noindex and should never appear in the sitemap.
      filter: (page) => !page.includes('/404'),
    }),
  ],
  vite: {
    plugins: [tailwindcss()],
    server: {
      // Allow Tailscale MagicDNS names when `make site PUBLIC_HOST=…` exposes
      // the dev server with --host. IPs + localhost are always allowed.
      allowedHosts: ['.ts.net'],
    },
  },
});
