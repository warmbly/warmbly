# Warmbly docs

The documentation site served at [docs.warmbly.com](https://docs.warmbly.com), built with Next.js and [Fumadocs](https://fumadocs.dev). Content lives in `content/docs/` as MDX, split into three sidebar sections:

- `guides/`: how every part of the product works
- `learn/`: cold email and deliverability fundamentals
- `api/`: API reference (authentication, permissions, endpoints, error codes)

## Development

```bash
pnpm install
pnpm dev          # dev server on :3000
pnpm types:check  # mdx + route types + tsc
pnpm lint         # eslint
```

## Static export

`pnpm build` writes the entire site to `out/` as a fully static export, servable from any static host or CDN. Search runs client-side from a prebuilt index (`/api/search`), OG images are generated at build time, and every page has a raw-Markdown mirror under `/llms.mdx/docs/` plus `llms.txt` / `llms-full.txt` indexes.

Serving notes: the export uses trailing-slash URLs (`guides/mailboxes/index.html`), and the custom 404 page is emitted as `404.html`. Configure your host to serve it for unknown paths.

## Conventions

- Frontmatter `title` is the page H1; do not repeat it as a `#` heading in the body. Set a lucide icon per page via the `icon` field.
- A folder becomes a sidebar tab when its `meta.json` sets `"root": true`.
- Avoid em dashes in prose; use a period, comma, colon, or parentheses.
