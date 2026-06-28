# Multi-stage Dockerfile for single binary project (frontend + backend)

# Stage 1: Build the React frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
# Copy package files first for better layer caching
COPY frontend/package.json frontend/package-lock.json ./
RUN mkdir -p public && npm ci && npm cache clean --force
# Copy frontend source code
COPY frontend/ ./
# Build the application
RUN npm run build

# Stage 2: Build the Go backend binary
# CGO disabled; we use modernc.org/sqlite (pure Go) so no C compiler or libc
# dependencies are needed in the builder. This keeps the image small and
# portable.
FROM golang:1.25-alpine AS backend-builder
WORKDIR /app/backend
# Install build dependencies (no CGO needed for modernc.org/sqlite)
RUN apk add --no-cache git ca-certificates tzdata
# Copy go mod files for dependency caching
COPY backend/go.mod backend/go.sum ./
RUN go mod download
# Copy backend source code
COPY backend/ ./
# Copy built frontend assets from Stage 1 into backend/assets/frontend
COPY --from=frontend-builder /app/frontend/build ./assets/frontend
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s' \
    -o /out/stargate .

# Stage 3: Final lightweight image
FROM debian:bookworm-slim

# Install runtime dependencies, Node.js (required by opencode), and bundle opencode CLI.
# This enables the built-in agent (AutoDetectExecutor) to drive real AI coding tools
# when STARGATE_AGENT_EXECUTOR=opencode (or auto-detected). No opencode.json is bundled.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    wget \
    curl \
    gnupg \
    git \
    build-essential \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y nodejs \
    && npm install -g npm@latest \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

# Install OpenCode CLI into /opt/opencode and expose via /usr/local/bin
RUN curl -fsSL https://opencode.ai/install | bash \
    && mv /root/.opencode /opt/opencode \
    && ln -s /opt/opencode/bin/opencode /usr/local/bin/opencode \
    && chmod -R 755 /opt/opencode

# Create non-root runtime user
RUN useradd -m -u 1000 -s /bin/bash stargate

WORKDIR /app

# Copy binary from builder
COPY --from=backend-builder /out/stargate /usr/local/bin/stargate

# Copy documentation and assets
COPY --from=backend-builder /app/backend/docs ./docs
COPY --from=backend-builder /app/backend/assets ./assets

# Create necessary directories and set ownership (include opencode and home for the user)
RUN mkdir -p /app/uploads /app/logs /app/ipfs_objects /app/ipfs_repo && \
    chown -R stargate:stargate /app /opt/opencode /home/stargate

# Set environment variables
# sqlite is the recommended durable default for single-binary / container deployments.
# memory is available when you want a completely ephemeral process (debugging / tests).
ENV STARGATE_STORAGE=sqlite
# HOME is set so opencode (and similar tools) can locate user config and state if needed.
ENV HOME=/home/stargate
# GIN_MODE removed: the project uses net/http + http.ServeMux (no Gin framework)

EXPOSE 3001

USER stargate

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3001/api/health || exit 1

ENTRYPOINT ["/usr/local/bin/stargate"]
