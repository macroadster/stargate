FROM golang:1.23 AS builder
WORKDIR /app

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/stargate .

FROM debian:bookworm-slim
RUN useradd -m -u 1000 stargate && apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /out/stargate /usr/local/bin/stargate
# Frontend assets (optional). Create placeholders to satisfy static paths.
RUN mkdir -p /app/frontend /app/uploads

ENV STARGATE_STORAGE=filesystem
EXPOSE 3001
USER stargate
ENTRYPOINT ["/usr/local/bin/stargate"]
