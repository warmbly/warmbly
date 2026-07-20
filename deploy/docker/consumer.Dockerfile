# syntax=docker/dockerfile:1.7
#
# CGO-free by default (NATS + JSON). Build with --build-arg GO_TAGS=kafka to
# include the Kafka backend (adds librdkafka + CGO). See backend.Dockerfile.
FROM golang:1.25-alpine AS builder

ARG GO_TAGS=""
RUN apk add --no-cache git ca-certificates && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache gcc musl-dev librdkafka-dev; fi

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    if echo "$GO_TAGS" | grep -qw kafka; then CGO=1; TAGS="musl kafka"; else CGO=0; TAGS=""; fi; \
    CGO_ENABLED=$CGO GOOS=linux go build -tags "$TAGS" -ldflags="-s -w" -o /out/consumer ./cmd/consumer

# Runtime stage
FROM alpine:3.23

ARG GO_TAGS=""
RUN apk add --no-cache ca-certificates tzdata && \
    if echo "$GO_TAGS" | grep -qw kafka; then apk add --no-cache librdkafka; fi && \
    adduser -D -u 1000 warmbly

COPY --from=builder /out/consumer /app/consumer

USER warmbly

ENTRYPOINT ["/app/consumer"]
