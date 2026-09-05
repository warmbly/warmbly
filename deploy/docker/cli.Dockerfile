# The `warmbly` CLI as an image, for CI jobs and anywhere installing a binary
# is more trouble than pulling one:
#
#   docker run --rm -e WARMBLY_TOKEN ghcr.io/warmbly/warmbly/cli campaign list
#
# Distroless-style: the CLI is a static binary that talks to one HTTPS API, so
# the runtime needs certificates, timezone data and nothing else.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=""
ARG COMMIT=""
ARG BUILT_AT=""

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w \
        -X github.com/warmbly/warmbly/internal/version.Version=$VERSION \
        -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT \
        -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" \
      -o /out/warmbly ./cmd/cli

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 warmbly

COPY --from=builder /out/warmbly /usr/local/bin/warmbly

# A container has no browser, so `warmbly auth login` cannot finish here.
# WARMBLY_TOKEN is the documented way in, and the config directory is a volume
# mount point for anyone who would rather bring their hosts.yml.
ENV WARMBLY_CONFIG_DIR=/home/warmbly/.config/warmbly \
    WARMBLY_NO_UPDATE_CHECK=1

USER warmbly
WORKDIR /home/warmbly

ENTRYPOINT ["warmbly"]
CMD ["--help"]
