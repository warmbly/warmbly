# Warmbly Web App

React + TypeScript frontend for Warmbly.

## Stack

- React 19
- Vite (Rolldown)
- TypeScript
- TanStack Query
- Zustand
- Tailwind CSS 4

## Prerequisites

- Node.js 20+
- pnpm 10+

## Setup

```bash
cd web
pnpm install
cp .env.example .env
```

## Run

```bash
pnpm dev
```

Default dev URL: `http://localhost:5173`

## Quality Checks

```bash
pnpm lint
pnpm test:run
pnpm build
```

## Project Layout

- `src/app` route pages and layouts
- `src/components` UI and feature components
- `src/lib/api` API clients, hooks, and models
- `src/stores` Zustand store slices

