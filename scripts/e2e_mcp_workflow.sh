#!/usr/bin/env bash
set -euo pipefail

# End-to-end smoke for MCP task workflow (list -> claim -> submit -> verify).
# Requirements: curl, jq; cluster ingress reachable at https://starlight.local.

MCP_BASE=${MCP_BASE:-https://starlight.local/mcp/v1}
AI_ID=${AI_ID:-codex-e2e}
CONTRACT_ID=${CONTRACT_ID:-}
PROPOSAL_ID=${PROPOSAL_ID:-}
INSCRIBE_TEXT=${INSCRIBE_TEXT:-}
INSCRIBE_PRICE=${INSCRIBE_PRICE:-0}
INSCRIBE_ADDRESS=${INSCRIBE_ADDRESS:-}
STARGATE_BASE=${STARGATE_BASE:-https://starlight.local}

fail() { echo "ERROR: $*" >&2; exit 1; }

if ! command -v jq >/dev/null; then
  fail "jq is required"
fi

echo "MCP base: $MCP_BASE"
echo "AI id: $AI_ID"
echo "Contract filter: ${CONTRACT_ID:-<none>}"
echo "Proposal filter: ${PROPOSAL_ID:-<none>}"
if [[ -n "$INSCRIBE_TEXT" ]]; then
  echo "Inscribe message: $INSCRIBE_TEXT"
fi

if [[ -n "$INSCRIBE_TEXT" ]]; then
  echo "0) Create inscription via frontend API..."
  inscribe_resp=$(curl -sk -X POST "${STARGATE_BASE}/api/inscribe" \
    -F "message=${INSCRIBE_TEXT}" \
    -F "price=${INSCRIBE_PRICE}" \
    ${INSCRIBE_ADDRESS:+-F "address=${INSCRIBE_ADDRESS}"} \
    -F "method=commit-reveal")
  inscribe_id=$(echo "$inscribe_resp" | jq -r '.id // .inscription_id // .data.id // empty')
  if [[ -z "$inscribe_id" || "$inscribe_id" == "null" ]]; then
    fail "inscribe failed: $inscribe_resp"
  fi
  echo "Inscribed: $inscribe_id"
  if [[ -z "$CONTRACT_ID" ]]; then
    CONTRACT_ID="$inscribe_id"
  fi
  echo "Waiting for proposal ingestion for $CONTRACT_ID ..."
  attempts=0
  while [[ $attempts -lt 24 ]]; do
    cid="${CONTRACT_ID}"
    props_json=$(curl -sk "${MCP_BASE}/proposals?contract_id=${cid}")
    pid=$(echo "$props_json" | jq -r '.proposals[0].id // empty')
    if [[ -z "$pid" ]]; then
      alt="wish-${CONTRACT_ID#wish-}"
      props_json=$(curl -sk "${MCP_BASE}/proposals?contract_id=${alt}")
      pid=$(echo "$props_json" | jq -r '.proposals[0].id // empty')
    fi
    if [[ -n "$pid" ]]; then
      PROPOSAL_ID=${PROPOSAL_ID:-$pid}
      echo "Found proposal: $pid"
      break
    fi
    attempts=$((attempts+1))
    sleep 5
  done
  if [[ -z "$PROPOSAL_ID" ]]; then
    fail "proposal not ingested for contract $CONTRACT_ID"
  fi
fi

echo "1) Fetch proposals..."
props_json=$(curl -sk "${MCP_BASE}/proposals")
[[ -n "$props_json" ]] || fail "no proposals returned"

# Choose proposal
if [[ -n "$PROPOSAL_ID" ]]; then
  proposal=$(echo "$props_json" | jq -c --arg id "$PROPOSAL_ID" '.proposals[] | select(.id==$id)')
else
  proposal=$(echo "$props_json" | jq -c '.proposals[0]')
fi
[[ -n "$proposal" && "$proposal" != "null" ]] || fail "no proposal found"
proposal_id=$(echo "$proposal" | jq -r '.id')
echo "Using proposal: $proposal_id"
proposal_status=$(echo "$proposal" | jq -r '.status')
if [[ "$proposal_status" == "pending" ]]; then
  echo "1b) Auto-approving ingested proposal..."
  approve_resp=$(curl -sk -X POST "${MCP_BASE}/proposals/${proposal_id}/approve")
  echo "$approve_resp" | jq '.'
  proposal_status="approved"
  # refetch proposal after approval
  proposal=$(curl -sk "${MCP_BASE}/proposals/${proposal_id}")
fi

echo "2) Select an available task..."
task=$(echo "$proposal" | jq -c --arg cid "$CONTRACT_ID" '(.tasks // []) | map(select((.status|ascii_downcase)=="available")) | map(select(($cid=="" ) or (.contract_id==$cid))) | first')
[[ -n "$task" && "$task" != "null" ]] || fail "no available task on proposal"
task_id=$(echo "$task" | jq -r '.task_id')
echo "Task: $task_id"

echo "3) Claim task..."
claim_resp=$(curl -sk -X POST "${MCP_BASE}/tasks/${task_id}/claim" \
  -H "Content-Type: application/json" \
  -d "{\"ai_identifier\":\"${AI_ID}\"}")
echo "$claim_resp" | jq '.'
claim_id=$(echo "$claim_resp" | jq -r '.claim_id')
[[ "$claim_id" != "null" ]] || fail "claim failed"

echo "4) Submit work..."
submit_resp=$(curl -sk -X POST "${MCP_BASE}/claims/${claim_id}/submit" \
  -H "Content-Type: application/json" \
  -d "{\"deliverables\":{\"notes\":\"e2e submission\"},\"completion_proof\":{\"link\":\"https://example.com/proof\"}}")
echo "$submit_resp" | jq '.'

echo "5) Verify task status..."
task_status=$(curl -sk "${MCP_BASE}/tasks/${task_id}/status")
echo "$task_status" | jq '.'

echo "6) Verify proposals reflect submission..."
post_props=$(curl -sk "${MCP_BASE}/proposals/${proposal_id}")
echo "$post_props" | jq '.tasks'

echo "E2E workflow completed for task ${task_id} (claim ${claim_id})."
