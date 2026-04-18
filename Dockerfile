# ===========================================================================
# Orchestra — Multi-Agent AI Orchestration Engine
# Multi-stage Dockerfile
# ===========================================================================

# ---------------------------------------------------------------------------
# Stage 1: Build
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
# CGO_ENABLED=0 for static linking
# -ldflags="-s -w" to strip debug info and reduce binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /build/bin/orchestra ./cmd/orchestra

# ---------------------------------------------------------------------------
# Stage 2: Runtime
# ---------------------------------------------------------------------------
FROM alpine:3.20 AS runtime

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    && rm -rf /var/cache/apk/*

# Create a non-root user for security
RUN addgroup -S orchestra && \
    adduser -S -G orchestra -H -h /app orchestra

# Set working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /build/bin/orchestra /app/orchestra

# Copy default configuration
COPY --from=builder /build/configs/orchestra.yaml /app/configs/orchestra.yaml

# Ensure the config directory is writable
RUN mkdir -p /app/configs && \
    chown -R orchestra:orchestra /app

# Switch to non-root user
USER orchestra

# Expose default ports
# 8080: HTTP API server
# 4317: OTLP gRPC (tracing)
EXPOSE 8080 4317

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/orchestra", "healthcheck"]

# Set default environment variables
ENV ORCHESTRA_LOGGING_LEVEL=info \
    ORCHESTRA_LOGGING_FORMAT=json

# Entry point
ENTRYPOINT ["/app/orchestra"]
CMD ["serve", "--config", "/app/configs/orchestra.yaml"]
