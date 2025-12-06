#!/usr/bin/env bash
set -euo pipefail

# Simple e2e: push a tiny ingestion record into Stargate and verify MCP sees a proposal.
# Requirements: curl, base64; env vars optional overrides.
BACKEND_BASE="${BACKEND_BASE:-http://localhost:3001}"
MCP_BASE="${MCP_BASE:-http://localhost:3002}"
INGEST_TOKEN="${INGEST_TOKEN:-$(kubectl get secret stargate-stack-secrets -o jsonpath='{.data.starlight-ingest-token}' 2>/dev/null | base64 -d || true)}"

if [[ -z "${INGEST_TOKEN}" ]]; then
  echo "INGEST_TOKEN not set and could not fetch from secret" >&2
  exit 1
fi

ID="e2e-$(date +%s)"
FILENAME="${ID}.png"
MSG="E2E test message for ${ID}"
IMG_B64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/Ptq4YQAAAABJRU5ErkJggg=="

echo "Posting ingestion ${ID}..."
curl -s -o /tmp/e2e_ingest.json -w "\nHTTP %{http_code}\n" \
  -H "X-Ingest-Token: ${INGEST_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"id\":\"${ID}\",\"filename\":\"${FILENAME}\",\"method\":\"alpha\",\"message_length\":${#MSG},\"image_base64\":\"${IMG_B64}\",\"metadata\":{\"embedded_message\":\"${MSG}\"}}" \
  "${BACKEND_BASE}/api/ingest-inscription"

echo "Waiting for MCP sync..."
sleep 5

echo "Fetching MCP proposals..."
curl -s "${MCP_BASE}/mcp/v1/proposals?status=pending" | tee /tmp/e2e_proposals.json

echo "Done. Look for id wish-${ID} in the proposals response."

