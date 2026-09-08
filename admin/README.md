# admin

Warmbly's internal admin control plane. Separate Vite + React app, parallel to `web/`, that drives the `/admin/*` endpoints on the same backend.

## Why a separate app

The dashboard at `web/` is the product surface for customers. The admin app is the surface for the Warmbly team running the platform. Splitting them gives us:

- a smaller, faster admin bundle (no tiptap, no marketing chrome, no onboarding flow)
- independent deployment cadence (admin can ship without touching customer code)
- different origin in production, so a stolen dashboard session can't quietly use admin endpoints
- a clear visual marker (the amber ADMIN badge + stripe + sidebar tint) so anyone with both tabs open knows which one is which

Both apps share the same backend, the same Bearer-token auth shape, and the same shadcn primitives.

## Run it locally

```sh
pnpm install
pnpm dev          # boots on http://localhost:5174
pnpm build        # production bundle into ./dist
pnpm typecheck    # tsc -b
pnpm lint
```

From the repo root you can also use `make admin`, which is a shortcut for
`cd admin && pnpm dev`. `make app` does **not** start this app — admin lives
outside the docker compose stack so it can ship on its own cadence.

The dev server defaults to port `5174` so it coexists with the dashboard's `5173`.

## First admin (local dev)

Admin access is gated by `users.admin_permissions` (bitmask) on the backend.
Nothing in the codebase seeds the first admin — sign up through the dashboard
as normal, then promote yourself from the repo root:

```sh
make grant-admin EMAIL=you@example.com               # super-admin
make grant-admin EMAIL=you@example.com ROLE=support  # or ops, analyst
make revoke-admin EMAIL=you@example.com              # drop back to 0
```

Role bitmasks mirror `AdminRolePermissions` in
`internal/models/admin_permission.go`. For one-off permission combinations,
pass a raw `BITMASK=N` instead of `ROLE`.

Once a super-admin exists they can grant the rest from **Accounts > Admins**,
which goes through the audited `GrantAdminPermissions` path instead of raw SQL.

Set up `.env.local` from `.env.example`:

```sh
cp .env.example .env.local
```

| Variable | Purpose |
| --- | --- |
| `VITE_API_URL` | Same Warmbly backend the dashboard talks to. Reuses `/admin/*`. |
| `VITE_ENV_LABEL` | Drives the `Production` / `Staging` / `Development` pill in the topbar. |
| `VITE_DASHBOARD_URL` | Used by the "Open dashboard" link in the user menu. |

## Visual differentiation (do not strip)

This app is intentionally tinted differently from the dashboard. If you find yourself "cleaning up" the amber accent, stop and read this section first.

- **ADMIN badge** in the sidebar header and on the login card. Amber pill, `ShieldAlert` icon. Always visible.
- **3px stripe** along the top of the app shell (`admin-stripe` utility). First thing the eye lands on.
- **Sidebar tint** (`--sidebar` shifted warm + faint diagonal pattern via `admin-sidebar-pattern`) so the rail reads as a different surface than the dashboard's near-white sidebar.
- **Amber active-nav state** instead of the dashboard's blue.
- **Env pill** in the topbar — different colour per environment.
- **Title prefix**: `index.html` ships `<title>Admin · Warmbly</title>` and the favicon is an amber-bordered shield (`public/admin-icon.svg`).

These signals are layered on purpose. A single one (e.g. just the badge) is easy to overlook in a tab strip. Stacked, they make it obvious that the user is in the privileged surface.

## What is in it

Every nav entry is backed by real endpoints under `/admin/*`; there are no stub pages.

| Group | Pages |
| --- | --- |
| Overview | counters, trends, signups by channel, the instance problems strip |
| Operations | Workers, Fleet (capacity, decision log, dedicated bindings), Mailboxes, Sync (backfill and fair-use throttle per mailbox), Warmup (pools, abuse signals, action history), Warmup Appeals, Warmup Content, Campaigns, Sends (in-flight reservations, dead letters, task failures, webhook delivery health) |
| Accounts | Users, Organizations (with API keys, webhooks and transfer tabs), Limit requests, Outreach, Admins |
| Insight | Live Events (the `admin:platform` socket firehose), Audit Log, Jobs (every background loop with last run, next run and "run now") |
| Instance | Setup and health (findings and service probes), Configuration (settings, notifications, environment, effective limits), Transfers (workspace export and import) |

Cmd/Ctrl K opens a command palette that jumps to any page and searches users, organizations, mailboxes and workers. Lists stay live through the realtime invalidation spine in `src/lib/realtime/RealtimeManager.tsx`; only pages whose data has no event (service probes, jobs, in-flight sends, capacity) poll.

The customer docs describe the panel page by page at `docs/content/docs/development/admin-panel.mdx`.

## Layout

```
admin/
├── index.html
├── package.json
├── vite.config.ts
├── tsconfig*.json
├── eslint.config.js
├── components.json          # shadcn config, mirrors web/
├── public/
│   └── admin-icon.svg       # amber-stroked shield favicon
└── src/
    ├── main.tsx             # router + query client + providers
    ├── global.css           # design tokens (mirror of web/) + admin-only tokens
    ├── app/
    │   ├── auth/LoginPage.tsx
    │   └── dashboard/       # one file per page, tab bodies in subfolders
    ├── components/
    │   ├── data/            # DataTable, Explorer facet rail
    │   ├── layout/          # AppShell, Sidebar, MobileNav, Topbar, CommandPalette, PageTabs, …
    │   └── ui/              # shadcn primitives copied from web/src/components/ui
    ├── hooks/
    │   └── useMe.ts, useDocumentTitle.ts, useInstanceHealth.ts, …
    └── lib/
        ├── env.ts
        ├── utils.ts
        ├── auth/storage.ts  # Bearer token persistence
        └── api/
            ├── client.ts    # axios instance + Request<T>
            ├── client/
            │   ├── auth/    # login, getMe, logout
            │   └── admin/   # one module per backend area (workers, sync, sends, jobs, fleet, …)
            └── models/
```
