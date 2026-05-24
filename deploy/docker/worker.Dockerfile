# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates gcc musl-dev librdkafka-dev

ENV GOCACHE=/tmp/go-cache
ENV GOTMPDIR=/tmp

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -tags musl -ldflags="-s -w" -o /worker ./cmd/worker

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata librdkafka
RUN adduser -D -u 1000 warmbly

COPY --from=builder /worker /app/worker

USER warmbly

ENTRYPOINT ["/app/worker"]
