# syntax=docker/dockerfile:1.7
#
# Cache mounts share `id=warmbly-go*` with backend/consumer so all
# three services hit the same Go module + build caches.

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates gcc musl-dev librdkafka-dev

ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/root/.cache/go-build
ENV GOTMPDIR=/tmp

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,id=warmbly-gomodcache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,id=warmbly-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=warmbly-gocache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -tags musl -ldflags="-s -w" -o /worker ./cmd/worker

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata librdkafka
RUN adduser -D -u 1000 warmbly

COPY --from=builder /worker /app/worker

USER warmbly

ENTRYPOINT ["/app/worker"]
