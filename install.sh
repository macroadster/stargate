#!/usr/bin/env bash
set -euo pipefail

REPO="macroadster/stargate"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [ "$OS" != "linux" ] && [ "$OS" != "darwin" ]; then
  echo "Unsupported OS: $OS" >&2
  exit 1
fi

BINARY="stargate-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"

echo "Downloading stargate for ${OS}/${ARCH}..."
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

if ! curl -fsSL -o "$TMP" "$URL"; then
  echo "Download failed. Check https://github.com/${REPO}/releases for available binaries." >&2
  exit 1
fi

chmod +x "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/stargate"
else
  sudo mv "$TMP" "${INSTALL_DIR}/stargate"
fi

echo "Installed stargate to ${INSTALL_DIR}/stargate"
stargate --version 2>/dev/null || true