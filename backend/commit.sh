#!/usr/bin/env bash
# Helper to commit only backend changes with a single command.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
service_path="backend"
msg="${1:-"Update ${service_path}"}"

echo "Staging changes under ${service_path}..."
git -C "${repo_root}" add "${service_path}"

if git -C "${repo_root}" diff --cached --quiet; then
  echo "No staged changes for ${service_path}; nothing to commit."
  exit 0
fi

echo "Creating commit: ${msg}"
git -C "${repo_root}" commit -m "${msg}"

echo "Latest status:"
git -C "${repo_root}" status -sb
