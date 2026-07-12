#!/usr/bin/env bash
set -euo pipefail

REPO="macroadster/stargate"
# User-local install by default.
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"
BINARY_NAME="stargate"

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
DEST="${INSTALL_DIR}/${BINARY_NAME}"

echo "Downloading stargate for ${OS}/${ARCH}..."
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

if ! curl -fsSL -o "$TMP" "$URL"; then
  echo "Download failed. Check https://github.com/${REPO}/releases for available binaries." >&2
  exit 1
fi

chmod +x "$TMP"

# Ensure install directory exists.
if [ ! -d "$INSTALL_DIR" ]; then
  echo "Creating install directory: $INSTALL_DIR"
  mkdir -p "$INSTALL_DIR"
fi

if [ ! -w "$INSTALL_DIR" ]; then
  echo "Install directory is not writable: $INSTALL_DIR" >&2
  echo "Set INSTALL_DIR to a user-writable path, e.g.:" >&2
  echo "  INSTALL_DIR=\"\$HOME/.local/bin\" curl -fsSL ... | bash" >&2
  exit 1
fi

mv "$TMP" "$DEST"
trap - EXIT

echo "Installed stargate to ${DEST}"

# --- PATH setup ----------------------------------------------------------------
# Strip trailing slash for consistent PATH matching.
INSTALL_DIR="${INSTALL_DIR%/}"

path_has_dir() {
  case ":${PATH}:" in
    *":${1}:"*) return 0 ;;
    *) return 1 ;;
  esac
}

detect_profile() {
  local shell_name
  shell_name=$(basename "${SHELL:-sh}")
  case "$shell_name" in
    zsh)
      # Interactive zsh reads .zshrc; create it if missing.
      printf '%s' "${HOME}/.zshrc"
      ;;
    bash)
      if [ -f "${HOME}/.bashrc" ] || [ ! -f "${HOME}/.bash_profile" ]; then
        printf '%s' "${HOME}/.bashrc"
      else
        printf '%s' "${HOME}/.bash_profile"
      fi
      ;;
    fish)
      mkdir -p "${HOME}/.config/fish"
      printf '%s' "${HOME}/.config/fish/config.fish"
      ;;
    *)
      printf '%s' "${HOME}/.profile"
      ;;
  esac
}

# Portable form for the profile snippet (survives home-dir moves / multi-user copy).
dir_for_profile() {
  case "$1" in
    "${HOME}"/*) printf '$HOME/%s' "${1#"${HOME}/"}" ;;
    *)           printf '%s' "$1" ;;
  esac
}

ensure_path_in_profile() {
  local dir="$1"
  local profile marker dir_display line

  profile=$(detect_profile)
  marker="# stargate: ensure user local bin is on PATH"
  dir_display=$(dir_for_profile "$dir")

  if [ -f "$profile" ] && grep -qF "$marker" "$profile" 2>/dev/null; then
    return 0
  fi

  # Also skip if the absolute path (or $HOME form) is already exported there.
  if [ -f "$profile" ] && grep -qE "(export PATH=|fish_add_path ).*$(printf '%s' "$dir" | sed 's/[.[\*^$()+?{|]/\\&/g')" "$profile" 2>/dev/null; then
    return 0
  fi
  if [ -f "$profile" ] && grep -qE '(export PATH=|fish_add_path ).*\$HOME/\.local/bin' "$profile" 2>/dev/null; then
    case "$dir" in
      "${HOME}/.local/bin") return 0 ;;
    esac
  fi

  if [ "$(basename "${SHELL:-}")" = "fish" ]; then
    line="fish_add_path ${dir_display}"
  else
    line="export PATH=\"${dir_display}:\$PATH\""
  fi

  {
    printf '\n'
    printf '%s\n' "$marker"
    printf '%s\n' "$line"
  } >> "$profile"

  echo "Added ${dir} to PATH in ${profile}"
  echo "Restart your shell, or run:  source ${profile}"
}

if path_has_dir "$INSTALL_DIR"; then
  echo "${INSTALL_DIR} is already on PATH"
else
  echo "${INSTALL_DIR} is not on PATH; updating shell profile..."
  ensure_path_in_profile "$INSTALL_DIR"
  # Available in this session for the version check below.
  export PATH="${INSTALL_DIR}:${PATH}"
fi

if "${DEST}" --version 2>/dev/null; then
  echo "Start the server:  stargate serve   # or just: stargate"
  echo "Help:              stargate help"
elif command -v stargate >/dev/null 2>&1; then
  stargate --version 2>/dev/null || true
  echo "Start the server:  stargate serve   # or just: stargate"
  echo "Help:              stargate help"
else
  echo "Run:  ${DEST} serve"
  echo "Help: ${DEST} help"
  echo "(or open a new shell so PATH picks up ${INSTALL_DIR})"
fi
