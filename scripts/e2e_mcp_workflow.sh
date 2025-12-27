#!/usr/bin/env bash
set -euo pipefail

# End-to-end smoke for MCP task workflow using current MCP /mcp/call API.
# Verifies both payout-to-contractors and fund-raising flows.
# Requirements: curl, jq; cluster ingress reachable at https://starlight.local.

MCP_BASE=${MCP_BASE:-https://starlight.local/mcp}
STARGATE_BASE=${STARGATE_BASE:-https://starlight.local}
AI_ID=${AI_ID:-codex-e2e}
FUNDER_API_KEY=${FUNDER_API_KEY:-}
CONTRACTOR_API_KEY=${CONTRACTOR_API_KEY:-}
FUNDRAISER_PAYOUT_ADDRESS=${FUNDRAISER_PAYOUT_ADDRESS:-}
PAYOUT_CONTRACT_ID=${PAYOUT_CONTRACT_ID:-}
RAISE_FUND_CONTRACT_ID=${RAISE_FUND_CONTRACT_ID:-}

fail() { echo "ERROR: $*" >&2; exit 1; }

if ! command -v jq >/dev/null; then
  fail "jq is required"
fi

if [[ -z "$FUNDER_API_KEY" ]]; then
  fail "FUNDER_API_KEY is required"
fi
if [[ -z "$CONTRACTOR_API_KEY" ]]; then
  fail "CONTRACTOR_API_KEY is required"
fi

random_hash() {
  if command -v shasum >/dev/null; then
    printf "%s" "$(date +%s%N)-$RANDOM" | shasum -a 256 | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null; then
    printf "%s" "$(date +%s%N)-$RANDOM" | openssl dgst -sha256 -r | awk '{print $1}'
    return
  fi
  fail "need shasum or openssl to generate a contract id"
}

call_mcp() {
  local api_key=$1
  local tool=$2
  local args_json=$3
  curl -sk -H "X-API-Key: ${api_key}" -H "Content-Type: application/json" \
    "${MCP_BASE}/call" \
    -d "$(jq -n --arg tool "${tool}" --argjson args "${args_json}" '{tool: $tool, arguments: $args}')"
}

ensure_success() {
  local resp=$1
  local context=$2
  local ok
  ok=$(echo "$resp" | jq -r '.success // empty')
  if [[ "$ok" != "true" ]]; then
    echo "$resp" | jq '.' >&2
    fail "${context} failed"
  fi
}

fetch_tasks_with_retry() {
  local contract_id=$1
  local attempts=0
  local resp
  while [[ $attempts -lt 10 ]]; do
    resp=$(call_mcp "${FUNDER_API_KEY}" "list_tasks" "$(jq -n --arg cid "${contract_id}" '{contract_id: $cid, status: "available", limit: 20}')")
    if [[ "$(echo "$resp" | jq -r '.success // empty')" == "true" ]]; then
      if [[ "$(echo "$resp" | jq '.result.tasks | length')" -gt 0 ]]; then
        echo "$resp"
        return
      fi
    fi
    attempts=$((attempts+1))
    sleep 2
  done
  echo "$resp"
}

approve_proposal() {
  local proposal_id=$1
  curl -sk -X POST -H "X-API-Key: ${FUNDER_API_KEY}" \
    "${STARGATE_BASE}/api/smart_contract/proposals/${proposal_id}/approve"
}

echo "MCP base: $MCP_BASE"
echo "AI id: $AI_ID"

PAYOUT_CONTRACT_ID=${PAYOUT_CONTRACT_ID:-$(random_hash)}
RAISE_FUND_CONTRACT_ID=${RAISE_FUND_CONTRACT_ID:-$(random_hash)}

if [[ -z "$FUNDRAISER_PAYOUT_ADDRESS" ]]; then
  fail "FUNDRAISER_PAYOUT_ADDRESS is required for raise-fund flow"
fi

echo "Payout contract: ${PAYOUT_CONTRACT_ID}"
echo "Raise-fund contract: ${RAISE_FUND_CONTRACT_ID}"

echo "== Payout-to-contractors flow =="
create_payout_resp=$(call_mcp "${FUNDER_API_KEY}" "create_proposal" "$(jq -n --arg cid "${PAYOUT_CONTRACT_ID}" '{
  title: "Payout E2E",
  description_md: "Payout flow e2e proposal.",
  contract_id: $cid,
  visible_pixel_hash: $cid,
  budget_sats: 25,
  metadata: {embedded_message: "* Task A\n* Task B"}
}')")
echo "$create_payout_resp" | jq '.'
ensure_success "$create_payout_resp" "create payout proposal"
payout_proposal_id=$(echo "$create_payout_resp" | jq -r '.result.proposal_id // empty')
[[ -n "$payout_proposal_id" ]] || fail "payout proposal create failed"
payout_contract_ref="$payout_proposal_id"

approve_proposal "$payout_proposal_id" | jq '.'

payout_tasks=$(fetch_tasks_with_retry "${payout_contract_ref}")
ensure_success "$payout_tasks" "list payout tasks"
echo "$payout_tasks" | jq '.result.tasks'

for task_id in $(echo "$payout_tasks" | jq -r '.result.tasks[].task_id'); do
  claim=$(call_mcp "${CONTRACTOR_API_KEY}" "claim_task" "$(jq -n --arg id "${task_id}" --arg ai "${AI_ID}" '{task_id: $id, ai_identifier: $ai}')")
  echo "$claim" | jq '.'
  ensure_success "$claim" "claim payout task"
  claim_id=$(echo "$claim" | jq -r '.result.claim_id // empty')
  [[ -n "$claim_id" ]] || fail "claim failed for $task_id"
  submit=$(call_mcp "${CONTRACTOR_API_KEY}" "submit_work" "$(jq -n --arg cid "${claim_id}" '{claim_id: $cid, deliverables: {notes: "e2e payout submission"}}')")
  echo "$submit" | jq '.'
  ensure_success "$submit" "submit payout task"
done

tasks_with_wallets=$(curl -sk -H "X-API-Key: ${FUNDER_API_KEY}" \
  "${STARGATE_BASE}/api/smart_contract/tasks?contract_id=${payout_contract_ref}")
payouts=$(echo "$tasks_with_wallets" | jq -c '[.tasks[] | select(.contractor_wallet != null and .contractor_wallet != "")] | group_by(.contractor_wallet) | map({address: .[0].contractor_wallet, amount_sats: (map(.budget_sats)|add)})')
[[ "$payouts" != "[]" ]] || fail "no payout addresses found for payout flow"

psbt_payout=$(curl -sk -X POST -H "X-API-Key: ${FUNDER_API_KEY}" -H "Content-Type: application/json" \
  "${STARGATE_BASE}/api/smart_contract/contracts/${payout_contract_ref}/psbt" \
  -d "$(jq -n --argjson payouts "${payouts}" --arg pixel "${PAYOUT_CONTRACT_ID}" '{payouts: $payouts, pixel_hash: $pixel, fee_rate_sats_vb: 1}')")
echo "$psbt_payout" | jq '.'
echo "$psbt_payout" | jq -e '.payout_scripts | length > 0' >/dev/null || fail "payout PSBT missing payout scripts"

echo "== Raise-fund flow =="
create_raise_resp=$(call_mcp "${FUNDER_API_KEY}" "create_proposal" "$(jq -n --arg cid "${RAISE_FUND_CONTRACT_ID}" --arg pay "${FUNDRAISER_PAYOUT_ADDRESS}" '{
  title: "Fund raising for space program",
  description_md: "Fund raising for space program",
  contract_id: $cid,
  visible_pixel_hash: $cid,
  budget_sats: 10,
  metadata: {
    funding_mode: "raise_fund",
    payout_address: $pay,
    funding_address: $pay,
    embedded_message: "* Build starship\n* Launch"
  }
}')")
echo "$create_raise_resp" | jq '.'
ensure_success "$create_raise_resp" "create raise-fund proposal"
raise_proposal_id=$(echo "$create_raise_resp" | jq -r '.result.proposal_id // empty')
[[ -n "$raise_proposal_id" ]] || fail "raise-fund proposal create failed"
raise_contract_ref="$raise_proposal_id"

approve_proposal "$raise_proposal_id" | jq '.'

raise_tasks=$(fetch_tasks_with_retry "${raise_contract_ref}")
ensure_success "$raise_tasks" "list raise-fund tasks"
echo "$raise_tasks" | jq '.result.tasks'
raise_meta=$(curl -sk -H "X-API-Key: ${FUNDER_API_KEY}" "${STARGATE_BASE}/api/smart_contract/proposals/${raise_proposal_id}")
echo "$raise_meta" | jq '.metadata'
echo "$raise_meta" | jq -e '.metadata.funding_mode == "raise_fund"' >/dev/null || fail "raise-fund metadata missing funding_mode"

for task_id in $(echo "$raise_tasks" | jq -r '.result.tasks[].task_id'); do
  claim=$(call_mcp "${CONTRACTOR_API_KEY}" "claim_task" "$(jq -n --arg id "${task_id}" --arg ai "${AI_ID}" '{task_id: $id, ai_identifier: $ai}')")
  echo "$claim" | jq '.'
  ensure_success "$claim" "claim raise-fund task"
  claim_id=$(echo "$claim" | jq -r '.result.claim_id // empty')
  [[ -n "$claim_id" ]] || fail "claim failed for $task_id"
done

psbt_raise=$(curl -sk -X POST -H "X-API-Key: ${FUNDER_API_KEY}" -H "Content-Type: application/json" \
  "${STARGATE_BASE}/api/smart_contract/contracts/${raise_contract_ref}/psbt" \
  -d "$(jq -n --arg pixel "${RAISE_FUND_CONTRACT_ID}" '{fee_rate_sats_vb: 1, split_psbt: true, pixel_hash: $pixel}')")
echo "$psbt_raise" | jq '.'
if echo "$psbt_raise" | jq -e '.funding_mode == "raise_fund" and (.psbts | length > 0)' >/dev/null; then
  echo "Raise-fund PSBT built successfully."
else
  raise_error=$(echo "$psbt_raise" | jq -r '.error // empty')
  if [[ "$raise_error" == *"no confirmed utxos"* ]]; then
    echo "WARN: raise-fund PSBT could not build due to missing UTXOs for contractor wallets."
  else
    fail "raise-fund PSBT did not report funding_mode=raise_fund"
  fi
fi

echo "E2E MCP workflow completed."
