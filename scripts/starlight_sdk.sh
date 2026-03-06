#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
exec bash "${script_dir}/../backend/mcp/assets/starlight_sdk.sh" "$@"
