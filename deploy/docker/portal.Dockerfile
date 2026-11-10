# syntax=docker/dockerfile:1.7
#
# The portal service: client connect pages, shared reports and the embed loader
# (portal/worker), run on workerd, the open-source runtime Cloudflare Workers
# use. Warmbly Cloud deploys the same two modules as a Worker. No database: the
# backend's internal API is its only dependency.
#
# Every RUN executes on $BUILDPLATFORM; the runtime stage only copies, so both
# arches build on one runner without QEMU.
FROM --platform=$BUILDPLATFORM node:22-alpine AS appbuilder
WORKDIR /app
# No TTY in a build: CI=true makes pnpm reinstall instead of prompting.
ENV CI=true
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY portal/package.json portal/pnpm-lock.yaml portal/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY portal/ .
RUN pnpm build

# workerd is Cloudflare's npm build for the target arch, checked against the
# registry's sha512 before it is unpacked. Bumping the version means bumping
# both digests and the compatibilityDate in deploy/docker/portal/workerd.capnp.
FROM --platform=$BUILDPLATFORM alpine:3.23 AS workerd
ARG TARGETARCH
ARG WORKERD_VERSION=1.20261026.1
ARG WORKERD_SHA512_AMD64=4bae66a20fb52caa79293deef90ddb5a0ed5421aaa7cb2fc68cda8da07dc85b5f028e5f5dd9296b82ab86714c0c45846eccdf52df10bd6257a2be9b21698e168
ARG WORKERD_SHA512_ARM64=b0cfabb1a80b59d4866fc18fe50cf8d279ae7dbc7c11ac110fe8f0fb24b3762bf7a302ce2e53f4675373978b3982d67a38f087495b10c5d58d0837e724b6e254
RUN set -eu; \
    case "$TARGETARCH" in \
        amd64) pkg=linux-64; sum=$WORKERD_SHA512_AMD64 ;; \
        arm64) pkg=linux-arm64; sum=$WORKERD_SHA512_ARM64 ;; \
        *) echo "workerd has no build for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    wget -qO /tmp/workerd.tgz "https://registry.npmjs.org/@cloudflare/workerd-$pkg/-/workerd-$pkg-$WORKERD_VERSION.tgz"; \
    echo "$sum  /tmp/workerd.tgz" | sha512sum -c -; \
    tar -xzf /tmp/workerd.tgz -C /tmp package/bin/workerd; \
    install -D -m 0755 /tmp/package/bin/workerd /out/workerd

# workerd links only glibc; busybox's glibc build adds the shell the entrypoint
# needs and the wget the health check uses, and nothing else.
FROM busybox:1.37.0-glibc

COPY --from=workerd /out/workerd /app/workerd
# workerd refuses to start without a system CA store, even when API_ORIGIN is plain HTTP.
COPY --from=workerd /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=appbuilder /app/dist /app/dist
COPY portal/worker/index.mjs portal/worker/portal.mjs deploy/docker/portal/workerd.capnp /app/
COPY --chmod=0755 deploy/docker/portal/entrypoint.sh /app/entrypoint.sh

USER 1000:1000
EXPOSE 8095

# A loopback-only socket, so the check never answers on the portal's own hosts.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -q -O /dev/null http://127.0.0.1:8096/ || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
