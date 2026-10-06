#!/usr/bin/env bash
set -euo pipefail

MCP_BASE=${MCP_BASE:-"{{MCP_BASE}}"}
STARGATE_BASE=${STARGATE_BASE:-"{{BASE_URL}}"}
STARLIGHT_TIMEOUT=${STARLIGHT_TIMEOUT:-120}
STARLIGHT_INSECURE=${STARLIGHT_INSECURE:-0}

usage() {
  cat <<'EOF'
Usage:
  starlight_sdk.sh create-wish [--api-key KEY] --message TEXT [options]
  starlight_sdk.sh submit-work [--api-key KEY] --claim-id ID --notes TEXT [options]
  starlight_sdk.sh call [--api-key KEY] --tool TOOL [--args-json JSON]

Commands:
  create-wish
    Bridge local file paths into create_wish/image_base64 payloads.
    Options:
      --api-key KEY
      --message TEXT
      --message-file PATH
      --image PATH
      --price VALUE
      --price-unit btc|sats
      --funding-mode payout|raise_fund
      --address BTC_ADDRESS

  submit-work
    Bridge local file paths into submit_work/deliverables.artifacts payloads.
    Options:
      --api-key KEY
      --claim-id ID
      --notes TEXT
      --notes-file PATH
      --artifact PATH        (required, repeatable)
      --artifact-root PATH   (optional root for relative artifact names)

  call
    Generic MCP call wrapper.
    Options:
      --api-key KEY
      --tool TOOL
      --args-json JSON

Environment:
  MCP_BASE={{MCP_BASE}}
  STARGATE_BASE={{BASE_URL}}
  STARLIGHT_API_KEY    used when --api-key is omitted (keeps the key out of ps and shell history)
  STARLIGHT_TIMEOUT    max seconds per request (default 120)
  STARLIGHT_INSECURE=1 skip TLS certificate checks (self-signed dev clusters only)

The response body is always printed. Exit status is 1 when the response has "success": false
(tool errors, including a bad API key, come back as HTTP 200), and curl's code on HTTP or
network errors.

Examples:
  ./scripts/starlight_sdk.sh create-wish \
    --api-key "$API_KEY" \
    --message-file docs/wish.md \
    --image assets/wish.png

  ./scripts/starlight_sdk.sh submit-work \
    --api-key "$API_KEY" \
    --claim-id claim-123 \
    --notes-file docs/report.md \
    --artifact dist/index.html \
    --artifact dist/screenshot.png \
    --artifact-root dist
EOF
}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

require_cmd() {
  local cmd=$1
  command -v "$cmd" >/dev/null 2>&1 || fail "missing required command: $cmd"
}

mime_type_for() {
  local path=$1
  if command -v file >/dev/null 2>&1; then
    file --mime-type -b "$path"
    return
  fi
  case "${path##*.}" in
    png) echo "image/png" ;;
    jpg|jpeg) echo "image/jpeg" ;;
    gif) echo "image/gif" ;;
    webp) echo "image/webp" ;;
    svg) echo "image/svg+xml" ;;
    html) echo "text/html" ;;
    css) echo "text/css" ;;
    js|mjs|cjs) echo "application/javascript" ;;
    json) echo "application/json" ;;
    md) echo "text/markdown" ;;
    txt) echo "text/plain" ;;
    pdf) echo "application/pdf" ;;
    *) echo "application/octet-stream" ;;
  esac
}

relative_name() {
  local path=$1
  local root=${2:-}
  if [[ -n "$root" ]]; then
    local abs_path abs_root
    abs_path=$(cd "$(dirname "$path")" && pwd)/$(basename "$path")
    abs_root=$(cd "$root" && pwd)
    case "$abs_path" in
      "$abs_root"/*)
        printf '%s\n' "${abs_path#"$abs_root"/}"
        return
        ;;
    esac
  fi
  printf '%s\n' "$(basename "$path")"
}

call_mcp() {
  local api_key=$1
  local tool=$2
  local args_json=$3

  local -a curl_opts=(-sS --fail-with-body --max-time "$STARLIGHT_TIMEOUT")
  if [[ "$STARLIGHT_INSECURE" == "1" ]]; then
    echo "WARNING: STARLIGHT_INSECURE=1, TLS certificate checks are disabled" >&2
    curl_opts+=(-k)
  fi

  # Use a pipe and curl -d @- to avoid ARG_MAX issues with large payloads.
  # The key goes through a header file so it never appears in curl's argv.
  local resp rc=0
  resp=$(jq -n --arg tool "$tool" --slurpfile args <(printf "%s" "$args_json") \
    '{tool: $tool, arguments: $args[0]}' | \
  curl "${curl_opts[@]}" \
    -H @<(printf 'X-API-Key: %s\n' "$api_key") \
    -H "Content-Type: application/json" \
    "${MCP_BASE}/call" \
    -d @-) || rc=$?
  [[ -z "$resp" ]] || printf '%s\n' "$resp"
  [[ $rc -eq 0 ]] || return "$rc"

  if ! jq -e 'type == "object"' >/dev/null 2>&1 <<<"$resp"; then
    echo "ERROR: ${tool} returned a non-JSON response" >&2
    return 1
  fi
  # /mcp/call reports tool failures (including a bad API key) as HTTP 200 with success=false.
  if jq -e '.success == false' >/dev/null <<<"$resp"; then
    local code
    code=$(jq -r '.error_code // .code // "unknown"' <<<"$resp" 2>/dev/null || true)
    echo "ERROR: ${tool} failed (${code})" >&2
    return 1
  fi
}

create_wish() {
  local api_key="${STARLIGHT_API_KEY:-}" message="" message_file="" image="" price="" price_unit="" funding_mode="" address=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --api-key) api_key=${2:-}; shift 2 ;;
      --message) message=${2:-}; shift 2 ;;
      --message-file) message_file=${2:-}; shift 2 ;;
      --image) image=${2:-}; shift 2 ;;
      --price) price=${2:-}; shift 2 ;;
      --price-unit) price_unit=${2:-}; shift 2 ;;
      --funding-mode) funding_mode=${2:-}; shift 2 ;;
      --address) address=${2:-}; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) fail "unknown create-wish option: $1" ;;
    esac
  done

  [[ -n "$api_key" ]] || fail "--api-key or STARLIGHT_API_KEY is required"
  
  local args_json
  if [[ -n "$message_file" ]]; then
    args_json=$(jq -Rs '{message: .}' "$message_file")
  else
    [[ -n "$message" ]] || fail "--message or --message-file is required"
    args_json=$(printf "%s" "$message" | jq -Rs '{message: .}')
  fi

  if [[ -n "$image" ]]; then
    args_json=$(jq --slurpfile img <(base64 <"$image" | tr -d '\n' | jq -Rs .) \
      '. + {image_base64: $img[0]}' <<<"$args_json")
  fi
  if [[ -n "$price" ]]; then
    args_json=$(jq --arg price "$price" '. + {price: $price}' <<<"$args_json")
  fi
  if [[ -n "$price_unit" ]]; then
    args_json=$(jq --arg price_unit "$price_unit" '. + {price_unit: $price_unit}' <<<"$args_json")
  fi
  if [[ -n "$funding_mode" ]]; then
    args_json=$(jq --arg funding_mode "$funding_mode" '. + {funding_mode: $funding_mode}' <<<"$args_json")
  fi
  if [[ -n "$address" ]]; then
    args_json=$(jq --arg address "$address" '. + {address: $address}' <<<"$args_json")
  fi

  call_mcp "$api_key" "create_wish" "$args_json"
}

submit_work() {
  local api_key="${STARLIGHT_API_KEY:-}" claim_id="" notes="" notes_file="" artifact_root=""
  local -a artifacts=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --api-key) api_key=${2:-}; shift 2 ;;
      --claim-id) claim_id=${2:-}; shift 2 ;;
      --notes) notes=${2:-}; shift 2 ;;
      --notes-file) notes_file=${2:-}; shift 2 ;;
      --artifact) artifacts+=("${2:-}"); shift 2 ;;
      --artifact-root) artifact_root=${2:-}; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) fail "unknown submit-work option: $1" ;;
    esac
  done

  [[ -n "$api_key" ]] || fail "--api-key or STARLIGHT_API_KEY is required"
  [[ -n "$claim_id" ]] || fail "--claim-id is required"
  
  local deliverables_json
  if [[ -n "$notes_file" ]]; then
    deliverables_json=$(jq -Rs '{notes: .}' "$notes_file")
  else
    [[ -n "$notes" ]] || fail "--notes or --notes-file is required"
    deliverables_json=$(printf "%s" "$notes" | jq -Rs '{notes: .}')
  fi
  
  [[ ${#artifacts[@]} -gt 0 ]] || fail "--artifact is required (at least one)"

  local artifacts_json
  artifacts_json=$(
    for path in "${artifacts[@]}"; do
      [[ -f "$path" ]] || fail "artifact not found: $path"
      local filename=$(relative_name "$path" "$artifact_root")
      local content_type=$(mime_type_for "$path")
      base64 <"$path" | tr -d '\n' | jq -Rs \
        --arg filename "$filename" \
        --arg content_type "$content_type" \
        '{filename: $filename, content: ., content_type: $content_type}'
    done | jq -s '.'
  )

  deliverables_json=$(jq --slurpfile arts <(printf "%s" "$artifacts_json") \
    '. + {artifacts: $arts[0]}' <<<"$deliverables_json")

  call_mcp "$api_key" "submit_work" "$(jq -n \
    --arg claim_id "$claim_id" \
    --slurpfile deliv <(printf "%s" "$deliverables_json") \
    '{claim_id: $claim_id, deliverables: $deliv[0]}')"
}

generic_call() {
  local api_key="${STARLIGHT_API_KEY:-}" tool="" args_json="{}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --api-key) api_key=${2:-}; shift 2 ;;
      --tool) tool=${2:-}; shift 2 ;;
      --args-json) args_json=${2:-}; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) fail "unknown call option: $1" ;;
    esac
  done

  [[ -n "$api_key" ]] || fail "--api-key or STARLIGHT_API_KEY is required"
  [[ -n "$tool" ]] || fail "--tool is required"
  call_mcp "$api_key" "$tool" "$args_json"
}

main() {
  require_cmd curl
  require_cmd jq
  require_cmd base64
  # Placeholders are only filled in when the server serves this file; the repo copy needs MCP_BASE.
  [[ "$MCP_BASE" != *"{{"* ]] || fail "MCP_BASE is not set (e.g. export MCP_BASE=https://starlight.local/mcp)"

  local cmd=${1:-}
  [[ -n "$cmd" ]] || { usage; exit 1; }
  shift

  case "$cmd" in
    create-wish) create_wish "$@" ;;
    submit-work) submit_work "$@" ;;
    call) generic_call "$@" ;;
    -h|--help) usage ;;
    *) fail "unknown command: $cmd" ;;
  esac
}

main "$@"
