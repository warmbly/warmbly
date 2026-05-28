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

Once a super-admin exists they can grant the rest through the in-app user
management screen, which goes through the audited `GrantAdminPermissions`
path instead of raw SQL.

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

## What's wired vs. stubbed

**Real data:**

- Overview — `/admin/analytics/overview` plus `/admin/workers/managed` for the fleet card
- Workers list — `/admin/workers/managed`
- Worker detail — `/admin/workers/:id/managed`, `/admin/workers/:id/live-status`, `/admin/workers/:id/logs`, plus the SSH lifecycle mutations (`test`, `install`, `restart`, `uninstall`)
- Egresses — wired to `/admin/workers/managed` re-framed as sending identities (TODO when `/admin/egresses` exists)
- Audit Log — `/admin/audit-logs`
- Settings (Encryption, Storage, Messaging, Cache, Transports) — `/admin/settings/backends` with `kind` filter; renders an "endpoint pending" placeholder when the registry isn't wired yet

**Stubs (page exists, no backend wire-up yet):**

- Mailboxes
- Users
- Organizations
- Plans & Billing
- Warmup pools
- Campaigns
- Analytics (cross-platform charts; the Overview page already feeds from the same family of endpoints)

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
    │   ├── dashboard/       # Overview, Workers, Egresses, Audit, stubs
    │   └── settings/        # Encryption/Storage/Messaging/Cache/Transports
    ├── components/
    │   ├── layout/          # AppShell, Sidebar, Topbar, AdminBadge, EnvPill, …
    │   └── ui/              # shadcn primitives copied from web/src/components/ui
    ├── hooks/
    │   └── useMe.ts
    └── lib/
        ├── env.ts
        ├── utils.ts
        ├── auth/storage.ts  # Bearer token persistence
        └── api/
            ├── client.ts    # axios instance + Request<T>
            ├── client/
            │   ├── auth/    # login, getMe, logout
            │   └── admin/   # workers, audit, analytics, settings
            └── models/
```
