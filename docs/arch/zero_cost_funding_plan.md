# Zero-Cost Funding Reliability Plan

## Objective
Enable a highly reliable "No Donation" funding workflow without requiring users to pay for an extra "dust" commitment output.

## Problem Statement
Currently, when a user opts out of a donation, the system sets the commitment amount to 0 sats. This prevents the creation of a "commitment anchor" output. Without this anchor, the `block_monitor` relies on fallback mechanisms (matching payout script hashes) to identify the funding transaction. This fallback is less reliable and can lead to missed reconciliations.

## The Solution: SegWit Stable TxIDs
Instead of forcing a physical on-chain anchor (which costs money), we will use a "virtual anchor" by pre-calculating the **Transaction ID (TxID)** during PSBT construction.

Because the vast majority of modern Bitcoin transactions use SegWit (P2WPKH) inputs, the `TxID` is **non-malleable**. It does not change when signatures are applied. This allows us to know the final TxID *before* the transaction is signed or broadcast.

## Technical Implementation

### 1. Enhance PSBT Builder (`backend/bitcoin/psbt_builder.go`)
Modify `BuildFundingPSBT` and `BuildRaiseFundPSBT` to calculate and return the TxID of the unsigned transaction.

**Current Behavior:**
```go
FundingTxID: "", // Don't set until transaction is actually broadcast
```

**New Behavior:**
```go
// Calculate TxID of the unsigned transaction.
// For SegWit inputs, this ID is stable and will match the final mined TxID.
FundingTxID: tx.TxHash().String(),
```

### 2. Update Server Persistence (`backend/middleware/smart_contract/server.go`)
Ensure the server captures this pre-calculated ID and persists it to the Ingestion Record immediately upon PSBT generation.

**Logic:**
1.  User requests PSBT (No Donation).
2.  Server calls `BuildFundingPSBT`.
3.  Builder returns `PSBTResult` with `FundingTxID` populated.
4.  Server calls `publishIngestUpdate` to save this `FundingTxID` to the record's metadata.

### 3. Reliability & Fallback
*   **SegWit Users (Standard):** The pre-calculated TxID matches the final TxID. The `block_monitor` finds the transaction immediately by ID. Reliability: **High**.
*   **Legacy Users (Edge Case):** If the user uses Legacy inputs, the TxID will change after signing. The pre-calculated ID stored in our DB will be incorrect.
    *   **Mitigation:** The `block_monitor` will fail to find the ID. It will proceed to its existing fallback logic: scanning for the **Contractor's Payout Script Hash**.
    *   **Result:** The transaction is still found, just via the secondary path.

## Benefits
1.  **Zero Cost:** No need to add a 546 sat output to every transaction.
2.  **Performance:** Reconciliation is faster (O(1) lookup by ID vs O(N) script scanning).
3.  **Cleanliness:** Reduces blockchain bloat by removing unnecessary dust outputs.

## Action Items
- [ ] Modify `backend/bitcoin/psbt_builder.go` to populate `FundingTxID`.
- [ ] Verify `backend/middleware/smart_contract/server.go` correctly saves this ID.
- [ ] Test with both SegWit (Native/Nested) and Legacy inputs to confirm behavior.
