FROM golang:1.23-alpine AS builder
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files for dependency caching
COPY go.mod go.sum ./

# Download dependencies and verify
RUN go mod download && go mod verify

# Copy all source code (but excluded large directories via .dockerignore)
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o /out/stargate . && \
    # Clean up build caches to reduce image size (correct command)
    go clean -cache && \
    go clean -modcache && \
    rm -rf /root/.cache /root/go/pkg/mod /tmp/go-build*

# Production stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -u 1000 -s /bin/bash stargate

WORKDIR /app

# Copy binary from builder
COPY --from=builder /out/stargate /usr/local/bin/stargate

# Copy documentation
COPY --from=builder /app/docs ./docs

# Create necessary directories
RUN mkdir -p /app/frontend /app/uploads /app/logs && \
    chown -R stargate:stargate /app

# Set environment variables
ENV STARGATE_STORAGE=filesystem
ENV GIN_MODE=release

EXPOSE 3001

USER stargate

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3001/health || exit 1

ENTRYPOINT ["/usr/local/bin/stargate"]
