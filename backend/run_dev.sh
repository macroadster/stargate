#!/usr/bin/env bash
set -euo pipefail

# Dev runner for Stargate backend without containers.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

export STARGATE_STORAGE="${STARGATE_STORAGE:-filesystem}"
export BLOCKS_DIR="${BLOCKS_DIR:-$ROOT_DIR/blocks}"
export UPLOADS_DIR="${UPLOADS_DIR:-$ROOT_DIR/uploads}"
export STARGATE_PROXY_BASE="${STARGATE_PROXY_BASE:-http://localhost:8080}"
export STARGATE_API_KEY="${STARGATE_API_KEY:-demo-api-key}"
export ALLOW_ORIGINS="${ALLOW_ORIGINS:-*}"

mkdir -p "${BLOCKS_DIR}" "${UPLOADS_DIR}"

echo "Starting Stargate backend (storage=${STARGATE_STORAGE}, proxy=${STARGATE_PROXY_BASE})"
go run .
