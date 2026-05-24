# Build stage
FROM rust:1.93-alpine AS builder

# Install build dependencies
RUN apk add --no-cache musl-dev cmake make gcc g++ pkgconfig openssl-dev openssl-libs-static curl-dev zlib-static

WORKDIR /app

# Copy manifest files
COPY Cargo.toml Cargo.lock* ./

# Create dummy main.rs to cache dependencies
RUN mkdir src && echo "fn main() {}" > src/main.rs

# Build dependencies only
RUN cargo build --release

# Remove dummy files
RUN rm -rf src

# Copy actual source code
COPY src ./src

# Build the actual application
RUN touch src/main.rs && cargo build --release

# Runtime stage
FROM alpine:3.23

# Install runtime dependencies
RUN apk add --no-cache ca-certificates libgcc

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/target/release/tracking /app/tracking

# Create non-root user
RUN addgroup -g 1000 tracking && \
    adduser -u 1000 -G tracking -s /bin/sh -D tracking

USER tracking

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/health || exit 1

# Run the binary
CMD ["/app/tracking"]
