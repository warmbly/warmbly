# site

Warmbly's public marketing site. Astro 5 + Tailwind v4, separate from the
in-product dashboard at `web/` and the admin app at `admin/`.

## Run it locally

```sh
pnpm install
pnpm dev          # boots on http://localhost:4321
pnpm build        # static output into ./dist
pnpm preview      # serve the production build locally
```

From the repo root you can also use `make site`, which is a shortcut for
`cd site && pnpm dev`. `make app` does **not** start this site — it lives
outside the docker compose stack and ships on its own cadence.

## Layout

```
site/
├── astro.config.mjs   # sitemap integration + Tailwind v4 Vite plugin
├── public/            # static assets served as-is
├── scripts/           # build-time helpers
└── src/               # pages, layouts, components, content
```
