# CGO disabled; using modernc.org/sqlite (pure Go implementation).
# No gcc or libc-dev needed in the builder.
# Pin the patch toolchain. Go 1.27.3 includes the crypto/x509 root-store
# fix for CVE-2026-111943 and matches the btcd v0.27.0 release toolchain.
FROM golang:1.27.3-alpine AS builder
WORKDIR /app

# Install build dependencies (pure-Go SQLite, no CGO)
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files for dependency caching
COPY go.mod go.sum ./

# Download dependencies and verify
RUN go mod download && go mod verify

# Copy all source code (but excluded large directories via .dockerignore)
COPY . .

# Build btcd, embed it, then build stargate. Keep this version equal to
# BTCD_VERSION in scripts/build-btcd.sh.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=go1.27.3 go install -trimpath github.com/btcsuite/btcd@v0.27.0 && \
    cp "$(go env GOPATH)/bin/btcd" ./bitcoin/btcd.embed && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embedbtcd \
    -ldflags='-w -s' \
    -o /out/stargate . && \
    rm -f ./bitcoin/btcd.embed && \
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

# Copy documentation and assets
COPY --from=builder /app/docs ./docs
COPY --from=builder /app/assets ./assets
# Create necessary directories
RUN mkdir -p /app/frontend /app/uploads /app/logs && \
    chown -R stargate:stargate /app

# Set environment variables
# sqlite is the recommended durable default for single-binary / container deployments.
# memory is available when you want a completely ephemeral process (debugging / tests).
# sqlite is the recommended durable default for single-binary / container deployments.
# memory is available when you want a completely ephemeral process (debugging / tests).
ENV STARGATE_STORAGE=sqlite
# GIN_MODE removed: the project uses net/http + http.ServeMux (no Gin framework)

EXPOSE 3001

USER stargate

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3001/api/health || exit 1

ENTRYPOINT ["/usr/local/bin/stargate"]
