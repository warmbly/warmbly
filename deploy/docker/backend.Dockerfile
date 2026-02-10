# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates gcc musl-dev librdkafka-dev

ENV GOCACHE=/tmp/go-cache
ENV GOTMPDIR=/tmp

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -tags musl -ldflags="-s -w" -o /backend ./cmd/backend

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata librdkafka
RUN adduser -D -u 1000 warmbly

COPY --from=builder /backend /app/backend

USER warmbly
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/backend"]
