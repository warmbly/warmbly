import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  // Fully static site: `next build` writes the whole docs site to `out/`,
  // servable from any static host or CDN. Search runs client-side from the
  // prebuilt index (see app/api/search/route.ts), OG images and the raw
  // Markdown mirror are emitted as real files at build time.
  output: 'export',
  // Emit every page as `<path>/index.html` instead of `<path>.html`, so URLs
  // resolve as directory indexes on any static server (no extensionless-URL
  // support needed). Matches the trailing-slash URLs of the Astro site.
  trailingSlash: true,
  reactStrictMode: true,
  // Static export has no image optimization endpoint. Nothing imports
  // next/image directly today, but fumadocs-ui maps MDX `img` tags to
  // next/image through its framework adapter, so the first image added to a
  // doc would break the export without this.
  images: { unoptimized: true },
  // This app has its own lockfile alongside the repo root one; pin the workspace
  // root to silence Next's "multiple lockfiles" inference warning.
  turbopack: { root: import.meta.dirname },
};

export default withMDX(config);
