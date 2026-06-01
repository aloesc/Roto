# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install ca-certificates for HTTPS support
RUN apk add --no-cache ca-certificates git

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Copy source code
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=1.0.0" \
    -o /build/proxy-gateway \
    ./cmd/proxy-gateway

# Runtime stage - minimal image
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS support
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN addgroup -g 1000 proxygroup && \
    adduser -D -u 1000 -G proxygroup proxyuser

# Copy binary from builder
COPY --from=builder /build/proxy-gateway /app/proxy-gateway

# Copy example env file
COPY .env.example /app/.env.example

# Create directory for optional proxy list
RUN mkdir -p /app/data && chown -R proxyuser:proxygroup /app

# Switch to non-root user
USER proxyuser

# Expose proxy port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/health || exit 1

# Run the proxy gateway
ENTRYPOINT ["/app/proxy-gateway"]
