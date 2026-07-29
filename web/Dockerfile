# syntax=docker/dockerfile:1

# Stage 1: build the static dashboard once on the build platform. The output is
# plain JS/CSS/HTML (arch-independent), so both target arches reuse it and the
# heavy pnpm build runs only once.
FROM --platform=$BUILDPLATFORM node:22-alpine AS build
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
# No VITE_* is baked here on purpose: URLs come from the runtime config below,
# so this one image works for any deployment.
RUN pnpm build

# Stage 2: serve the built SPA from nginx with a history fallback. The
# entrypoint renders /config.js from container env at startup.
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
RUN nginx -t
COPY --from=build /app/dist /usr/share/nginx/html
COPY docker-entrypoint.sh /docker-entrypoint.d/40-warmbly-config.sh
RUN chmod +x /docker-entrypoint.d/40-warmbly-config.sh
EXPOSE 80
