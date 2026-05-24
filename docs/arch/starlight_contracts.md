# Bitcoin Script Pseudo-code for Starlight Contracts

## Overview

This document describes Bitcoin transaction flows for the Starlight system using Bitcoin Script concepts. Since Bitcoin Script is limited, we'll show both **native Bitcoin approaches** (using basic Script) and **smart contract extensions** (using systems like RGB, Taproot, or sidechains that enable more complex logic).

---

## Table of Contents
1. [Bitcoin Script Basics](#1-bitcoin-script-basics)
2. [Transaction Type 1: Contract Creation (Escrow Funding)](#2-transaction-type-1-contract-creation-escrow-funding)
3. [Transaction Type 2: Task Claim (Commitment)](#3-transaction-type-2-task-claim-commitment)
4. [Transaction Type 3: Work Submission (Proof Upload)](#4-transaction-type-3-work-submission-proof-upload)
5. [Transaction Type 4: Milestone Approval (Payment Release)](#5-transaction-type-4-milestone-approval-payment-release)
6. [Transaction Type 5: Dispute & Refund](#6-transaction-type-5-dispute--refund)
7. [Advanced: Multi-AI Collaboration](#7-advanced-multi-ai-collaboration)
8. [Implementation Strategies](#8-implementation-strategies)

---

## 1. Bitcoin Script Basics

### Standard Bitcoin Script Operations
```
OP_DUP          - Duplicate top stack item
OP_HASH160      - Hash top item (SHA256 + RIPEMD160)
OP_EQUALVERIFY  - Verify equality, fail if false
OP_CHECKSIG     - Verify signature against public key
OP_CHECKMULTISIG - Verify M-of-N multisig
OP_IF / OP_ELSE - Conditional branches
OP_CHECKLOCKTIMEVERIFY (CLTV) - Time-locked spending
OP_CHECKSEQUENCEVERIFY (CSV) - Relative time locks
```

### Key Concepts
- **Locking Script (scriptPubKey)**: Conditions that must be met to spend
- **Unlocking Script (scriptSig)**: Provides data to satisfy locking script
- **Witness Data**: Segregated Witness (SegWit) signature data
- **Taproot**: Enables complex scripts that look like normal transactions

---

## 2. Transaction Type 1: Contract Creation (Escrow Funding)

### Purpose
Human deposits BTC into escrow address tied to contract goals.

### Native Bitcoin Approach: 2-of-3 Multisig

```bitcoin-script
# TRANSACTION: Contract Creation
# Human creates UTXO locked to 2-of-3 multisig

INPUT:
  - Human's wallet UTXO (50,000,000 sats = 0.5 BTC)
  - Signature: <sig_human>

OUTPUT 0 (Escrow Address):
  Value: 50,000,000 sats
  scriptPubKey:
    OP_2                              # Require 2 signatures
    <pubkey_human>                    # Human (can approve/dispute)
    <pubkey_arbitrator>               # Neutral arbitrator
    <pubkey_oracle>                   # Reputation oracle / DAO
    OP_3                              # Out of 3 total keys
    OP_CHECKMULTISIG                  # Verify 2-of-3 signatures

OUTPUT 1 (OP_RETURN - Contract Metadata):
  Value: 0 sats (unspendable)
  scriptPubKey:
    OP_RETURN
    <contract_id_hash>                # SHA256("CONTRACT-550e8400")
    <ipfs_cid_goals>                  # Link to full contract JSON
    <timestamp>                       # Creation time

# Notes:
# - Funds are locked until 2-of-3 parties sign release
# - OP_RETURN embeds contract reference on-chain
# - Full contract details stored off-chain (IPFS/Arweave)
```

### Extended Script (Taproot/RGB): Programmable Escrow

```bitcoin-script
# TRANSACTION: Contract Creation (Taproot)
# Uses Taproot to hide complex spending conditions

INPUT:
  - Human's wallet UTXO (50,000,000 sats)

OUTPUT 0 (Taproot Escrow):
  Value: 50,000,000 sats
  scriptPubKey:
    OP_1                              # SegWit v1 (Taproot)
    <taproot_output_key>              # Aggregated public key
  
  # Hidden Taproot Script Tree:
  taproot_script_tree:
    leaf_1: "Approval Path"
      OP_IF
        <pubkey_human>                # Human approves work
        OP_CHECKSIGVERIFY
        <pubkey_ai_claimant>          # AI who claimed task
        OP_CHECKSIG
      OP_ENDIF
    
    leaf_2: "Dispute Path"
      OP_IF
        2
        <pubkey_human>
        <pubkey_arbitrator>
        2
        OP_CHECKMULTISIG              # 2-of-2 for dispute resolution
      OP_ENDIF
    
    leaf_3: "Timeout Refund"
      <locktime_90_days>
      OP_CHECKLOCKTIMEVERIFY          # Refund if no claims after 90 days
      OP_DROP
      <pubkey_human>
      OP_CHECKSIG

OUTPUT 1 (OP_RETURN):
  Value: 0 sats
  scriptPubKey:
    OP_RETURN
    <contract_metadata>               # Contract ID, goal hashes
```

---

## 3. Transaction Type 2: Task Claim (Commitment)

### Purpose
AI agent claims a specific task, locking it for 72 hours.

### Approach: Bitcoin-anchored State Commitment

```bitcoin-script
# TRANSACTION: Task Claim Commitment
# AI broadcasts claim to MCP, which anchors state to Bitcoin

INPUT:
  - AI's wallet UTXO (small amount for TX fees)

OUTPUT 0 (Commitment):
  Value: 10,000 sats (minimal, may be reclaimed later)
  scriptPubKey:
    OP_DUP
    OP_HASH160
    <hash160(pubkey_ai)>              # AI's public key hash
    OP_EQUALVERIFY
    72 * 144                          # ~72 hours in blocks (144 blocks/day)
    OP_CHECKSEQUENCEVERIFY            # Funds locked for 72 hours
    OP_DROP
    OP_CHECKSIG

OUTPUT 1 (OP_RETURN - Claim Metadata):
  Value: 0 sats
  scriptPubKey:
    OP_RETURN
    <task_id_hash>                    # SHA256("TASK-7f3b9c2a")
    <contract_id_hash>                # Links to parent contract
    <ai_pubkey>                       # AI's identity
    <claim_timestamp>
    <merkle_root_current_state>       # Anchors MCP state tree

# Notes:
# - AI stakes small amount (anti-spam)
# - CSV lock prevents immediate reclaim (forces commitment)
# - OP_RETURN publishes claim proof
# - MCP monitors blockchain for claim TXs
```

### Alternative: Lightning Network Payment Hash

```bitcoin-script
# TRANSACTION: Lightning HTLC for Task Claim
# Uses Hash Time-Locked Contract for instant claims

LIGHTNING_HTLC:
  amount: 10,000 sats
  payment_hash: SHA256(<task_claim_preimage>)
  timeout: 72 hours
  
  # AI locks funds in Lightning channel
  # Reveals preimage upon work submission
  # If timeout expires, funds return to AI
  
scriptPubKey (HTLC):
  OP_IF
    # Success path: AI reveals preimage (submitted work)
    OP_HASH256
    <payment_hash>
    OP_EQUALVERIFY
    <pubkey_human>                    # Human receives stake
    OP_CHECKSIG
  OP_ELSE
    # Timeout path: AI reclaims after 72 hours
    <locktime_72_hours>
    OP_CHECKLOCKTIMEVERIFY
    OP_DROP
    <pubkey_ai>
    OP_CHECKSIG
  OP_ENDIF
```

---

## 4. Transaction Type 3: Work Submission (Proof Upload)

### Purpose
AI submits completed work and proof to blockchain.

### Approach: OP_RETURN with Content Hash

```bitcoin-script
# TRANSACTION: Work Submission
# AI publishes proof of deliverables

INPUT:
  - AI's UTXO (from claim TX, now unlocked after work completion)

OUTPUT 0 (AI's Change):
  Value: 9,000 sats (minus TX fee)
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_ai)> OP_EQUALVERIFY OP_CHECKSIG

OUTPUT 1 (OP_RETURN - Submission Proof):
  Value: 0 sats
  scriptPubKey:
    OP_RETURN
    <task_id_hash>
    <submission_id>                   # "SUB-4e5f6a7b"
    <deliverable_hash>                # SHA256 of GitHub commit
    <test_results_hash>               # Hash of CI/CD output
    <timestamp>
    <ai_signature>                    # Signature over all above

# Notes:
# - Immutable proof that AI submitted work at specific time
# - Human can verify hashes match actual deliverables
# - MCP indexes this TX to update task status
```

### Extended: Discreet Log Contract (DLC) Oracle

```bitcoin-script
# TRANSACTION: DLC-Based Automated Verification
# Oracle attests to test results, auto-releases payment

SETUP (DLC Contract):
  Parties: Human, AI, Oracle
  Outcomes:
    - "tests_passed": Oracle signs with key R1
    - "tests_failed": Oracle signs with key R2
  
  Oracle_Statement:
    "Test suite for TASK-7f3b9c2a: PASSED (coverage 96.5%)"
    Signature: <sig_oracle(R1)>

TRANSACTION: Conditional Payment Based on Oracle
INPUT:
  - Escrow UTXO (from Contract Creation)
  - Oracle signature <sig_oracle(R1)>          # Proves tests passed

OUTPUT (Success Path):
  Value: 5,000,000 sats
  scriptPubKey:
    <pubkey_ai + R1>                  # AI's key + oracle's attestation key
    OP_CHECKSIG                       # Only valid if oracle signed "PASSED"

# If Oracle signed "FAILED":
OUTPUT (Refund Path):
  Value: 5,000,000 sats
  scriptPubKey:
    <pubkey_human + R2>               # Returns funds to human
    OP_CHECKSIG
```

---

## 5. Transaction Type 4: Milestone Approval (Payment Release)

### Purpose
Human reviews work and releases payment from escrow.

### Approach: Spending Multisig Escrow

```bitcoin-script
# TRANSACTION: Payment Release
# Human and AI (or arbitrator) co-sign to release funds

INPUT:
  - Escrow UTXO (from Contract Creation)
  scriptSig:
    0                                 # Placeholder for OP_CHECKMULTISIG
    <sig_human>                       # Human approves
    <sig_ai>                          # AI acknowledges receipt
    # (2-of-3 multisig satisfied)

OUTPUT 0 (Payment to AI):
  Value: 5,000,000 sats
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_ai)> OP_EQUALVERIFY OP_CHECKSIG

OUTPUT 1 (Return Remaining to Escrow):
  Value: 44,900,000 sats              # Remaining budget for other tasks
  scriptPubKey:
    OP_2 <pubkey_human> <pubkey_arbitrator> <pubkey_oracle> OP_3 OP_CHECKMULTISIG

OUTPUT 2 (OP_RETURN - Approval Record):
  Value: 0 sats
  scriptPubKey:
    OP_RETURN
    <task_id_hash>
    <approval_status>                 # "APPROVED"
    <quality_score>                   # Optional: 1-100 rating
    <timestamp>

# Notes:
# - Requires human signature (approval)
# - Payment flows directly to AI's wallet
# - On-chain record of approval for reputation system
```

### Alternative: Taproot Key-Spend Path (Most Efficient)

```bitcoin-script
# TRANSACTION: Taproot Cooperative Close
# Both parties agree, spend via key aggregation (cheapest)

INPUT:
  - Taproot Escrow UTXO
  scriptSig:
    <musig2_aggregate_signature>      # Combined sig (human + AI)
    # Taproot script tree not revealed (privacy + lower fees)

OUTPUT:
  Value: 5,000,000 sats
  scriptPubKey:
    OP_1 <taproot_key_ai>             # AI receives payment

# Notes:
# - Indistinguishable from normal payment on blockchain
# - No complex script revealed (privacy win)
# - Only used if both parties cooperate
```

---

## 6. Transaction Type 5: Dispute & Refund

### Purpose
Handle cases where human rejects work or AI abandons task.

### Scenario A: Human Disputes Quality

```bitcoin-script
# TRANSACTION: Dispute Arbitration
# Requires 2-of-3 multisig (Human + Arbitrator)

INPUT:
  - Escrow UTXO
  scriptSig:
    0
    <sig_human>                       # Human claims work insufficient
    <sig_arbitrator>                  # Arbitrator reviews and sides

OUTPUT 0 (Partial Refund to Human):
  Value: 3,000,000 sats               # 60% refund
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_human)> OP_EQUALVERIFY OP_CHECKSIG

OUTPUT 1 (Partial Payment to AI):
  Value: 2,000,000 sats               # 40% for partial work
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_ai)> OP_EQUALVERIFY OP_CHECKSIG

OUTPUT 2 (OP_RETURN - Dispute Resolution):
  Value: 0 sats
  scriptPubKey:
    OP_RETURN
    <task_id_hash>
    <dispute_outcome>                 # "PARTIAL_PAYMENT"
    <arbitrator_statement_hash>       # Link to written decision
```

### Scenario B: AI Abandons Task (Auto-Reclaim)

```bitcoin-script
# TRANSACTION: Automatic Expiry Refund
# AI's claim expired without submission

INPUT:
  - AI's Claim Commitment UTXO (locked 72 hours ago)
  scriptSig:
    <sig_human>                       # Human reclaims after timeout

OUTPUT:
  Value: 10,000 sats
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_human)> OP_EQUALVERIFY OP_CHECKSIG

# Notes:
# - CSV lock expired, funds now spendable
# - Human can reclaim staked amount (penalty to AI)
# - Task returns to "available" in MCP
```

### Scenario C: Full Contract Timeout (No Claims)

```bitcoin-script
# TRANSACTION: 90-Day Refund (Taproot Timeout Path)
# No AI claimed any tasks; human gets full refund

INPUT:
  - Taproot Escrow UTXO
  scriptSig:
    <sig_human>
    <taproot_script_leaf_3>           # Reveals timeout branch
    <control_block>                   # Merkle proof of script in tree

  # Unlocks via hidden script:
  taproot_script_leaf_3:
    <locktime_90_days>
    OP_CHECKLOCKTIMEVERIFY
    OP_DROP
    <pubkey_human>
    OP_CHECKSIG

OUTPUT:
  Value: 50,000,000 sats              # Full refund
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_human)> OP_EQUALVERIFY OP_CHECKSIG
```

---

## 7. Advanced: Multi-AI Collaboration

### Purpose
Complex task requiring multiple AIs (e.g., researcher + coder + designer).

### Approach: Threshold Signatures with Payment Splits

```bitcoin-script
# TRANSACTION: Multi-AI Collaborative Payment
# Escrow releases to multiple recipients

INPUT:
  - Escrow UTXO (10,000,000 sats task)
  scriptSig:
    0
    <sig_human>                       # Human approves all work
    <sig_ai_1>                        # Researcher confirms
    <sig_ai_2>                        # Coder confirms
    # 3-of-5 multisig (human + 2 AIs + 2 backups)

OUTPUT 0 (AI #1 - Researcher):
  Value: 3,000,000 sats (30%)
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_ai1)> OP_EQUALVERIFY OP_CHECKSIG

OUTPUT 1 (AI #2 - Coder):
  Value: 5,000,000 sats (50%)
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_ai2)> OP_EQUALVERIFY OP_CHECKSIG

OUTPUT 2 (AI #3 - Designer):
  Value: 2,000,000 sats (20%)
  scriptPubKey:
    OP_DUP OP_HASH160 <hash160(pubkey_ai3)> OP_EQUALVERIFY OP_CHECKSIG

# Notes:
# - Revenue split encoded in transaction outputs
# - All AIs must acknowledge before funds release (prevents disputes)
# - Human signs once, distribution automatic
```

---

## 8. Implementation Strategies

### Option A: Pure Bitcoin (Limited Functionality)
**What You Get:**
- Multisig escrow (2-of-3 works today)
- Time-locked refunds (CSV/CLTV)
- OP_RETURN metadata anchoring

**Limitations:**
- No automated verification (human must manually sign)
- Limited programmability
- No native state machine

**Best For:** Simple workflows, high-trust environments

---

### Option B: Taproot + DLCs (Moderate Complexity)
**What You Get:**
- Hidden complex scripts (privacy)
- Oracle-based automated verification
- Efficient cooperative closes
- Conditional payments based on attestations

**Limitations:**
- Requires oracle infrastructure
- More complex to implement
- Still limited state logic

**Best For:** Automated verification with trusted oracles

---

### Option C: RGB/Lightning (Advanced State)
**What You Get:**
- Full smart contract capabilities (off-chain)
- Instant claims via Lightning HTLCs
- Complex state machines
- Low on-chain footprint

**Limitations:**
- Requires Lightning channels
- More infrastructure
- Less battle-tested

**Best For:** High-frequency task marketplace, instant payments

---

### Option D: Federated Sidechain (Liquid/Rootstock)
**What You Get:**
- Solidity-style smart contracts
- Full Turing-complete logic
- Fast confirmations (1-2 min blocks)
- Bitcoin-backed security

**Limitations:**
- Federation trust assumption
- Separate blockchain
- Need to bridge BTC

**Best For:** Complex multi-party workflows, production-ready today

---

## 9. Recommended Architecture

### Hybrid Approach
```
┌─────────────────────────────────────────┐
│     BITCOIN LAYER 1 (Settlement)        │
│  - Contract funding (multisig/Taproot)  │
│  - Final payment releases               │
│  - Dispute resolutions                  │
│  - OP_RETURN state commitments          │
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│     LIGHTNING NETWORK (Claims)          │
│  - Instant task claims (HTLCs)          │
│  - Micropayments for small tasks        │
│  - Fast state updates                   │
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│     MCP SERVER (Coordination)           │
│  - Indexes Bitcoin TXs                  │
│  - Manages claim state                  │
│  - Provides query API                   │
│  - Submits state commitments            │
└─────────────────────────────────────────┘
```

### Transaction Flow Summary
1. **Contract Creation**: Bitcoin L1 (Taproot multisig)
2. **Task Claims**: Lightning HTLCs (instant, revocable)
3. **Work Submission**: OP_RETURN proof + IPFS hash
4. **Verification**: Oracle attestation (DLC) or human signature
5. **Payment Release**: Bitcoin L1 (final settlement)
6. **Disputes**: On-chain arbitration (2-of-3 multisig)

---

## 10. Sample Transaction Sequence

### Complete Workflow Example

```
Block 850000: CONTRACT CREATION
TX: a1b2c3d4...
├─ Input: Human's wallet (50,000,000 sats)
└─ Outputs:
   ├─ [0] Taproot Escrow (50M sats)
   └─ [1] OP_RETURN (contract metadata)

Block 850010: TASK CLAIM (via Lightning)
Lightning HTLC: payment_hash_xyz
├─ Amount: 10,000 sats
├─ Timeout: Block 850154 (72 hours)
└─ Preimage: <revealed_upon_submission>

Block 850100: WORK SUBMISSION
TX: b2c3d4e5...
├─ Input: AI's UTXO
└─ Outputs:
   ├─ [0] AI's change (9K sats)
   └─ [1] OP_RETURN (deliverable hashes)

Block 850105: ORACLE ATTESTATION
TX: c3d4e5f6...
├─ Input: Oracle's UTXO
└─ Output:
   └─ [0] OP_RETURN (DLC signature "TESTS_PASSED")

Block 850110: PAYMENT RELEASE
TX: d4e5f6a7...
├─ Input: Taproot Escrow (50M sats)
│   scriptSig: <musig2_sig(human, ai)>
└─ Outputs:
   ├─ [0] AI Payment (5M sats)
   ├─ [1] Return to Escrow (44.9M sats)
   └─ [2] OP_RETURN (approval record)

MCP UPDATE:
- Task TASK-7f3b9c2a → Status: "APPROVED"
- AI reputation: +1 completion
- Contract budget: 44.9M sats remaining
```

---

## 11. Key Takeaways

1. **Bitcoin Script is Limited**: Native Bitcoin can handle escrow, multisig, and timelocks, but not complex state machines.

2. **Taproot Enables Privacy**: Complex spending conditions can be hidden until needed, making transactions look normal.

3. **Oracles Add Automation**: DLCs allow automated verification without requiring human signatures for every task.

4. **Lightning Speeds Claims**: HTLCs enable instant task reservations with automatic reversal on timeout.

5. **OP_RETURN Anchors State**: Commitments to off-chain state (MCP database) provide verifiable history.

6. **Hybrid is Practical**: Use Bitcoin L1 for settlement, Lightning for speed, MCP for coordination.

---

**This pseudo-code provides the foundation for implementing Starlight's Bitcoin-backed AI task marketplace with cryptographic verification at every step.**

---

## 12. Implemented: Two-Phase Donation Sweep (Hashlock P2WSH)

> **Status: Implemented** — commit `54cb24a` on `feature/single-binary`

This section documents the **actually implemented** donation commitment mechanism. Unlike the theoretical designs above (multisig, Taproot, DLCs), this uses bare hashlock P2WSH scripts that require no signatures — only knowledge of the preimage.

### 12.1 Design Goal

When a contract is funded and a product is delivered, a portion of the funding (the "donation commitment") should be provably locked on-chain using the **wish image** hash, then re-locked using the **product image** hash, and finally swept to the project's donation wallet. This creates a two-phase cryptographic proof chain:

```
wish-image-hash  →  product-image-hash  →  donation wallet
```

### 12.2 PSBT Output Structure

When a funder builds a PSBT with `CommitmentSats > 0`:

```
Output 0..N:   P2WPKH  — contractor payouts (direct to wallet addresses)
Output N+1:    P2WSH   — hashlock donation commitment (wish image)
Output N+2:    P2WPKH  — change (if any)
```

The donation commitment output (N+1) uses a bare hashlock redeem script:

```bitcoin-script
# Redeem Script (wish-hash hashlock)
OP_SHA256
<SHA256(wish_pixel_hash_bytes)>    # double-SHA256: SHA256 of the raw wish image pixel hash
OP_EQUAL

# Spending witness: [wish_pixel_hash_bytes, redeem_script]
# Anyone who knows the wish image preimage can spend this UTXO.
```

The `wish_pixel_hash` is `manifest.VisiblePixelHash` — the SHA256 of the visible pixel data of the wish image. The lock hash is `SHA256(wish_pixel_hash_bytes)`, making it a double-hash commitment.

**Source**: `backend/bitcoin/psbt_builder.go` → `buildHashlockRedeemScript()`, `buildCommitmentScript()`

### 12.3 Two-Phase Sweep Flow

```
┌──────────────────────────────────────────────────────────┐
│  PSBT Funding TX (confirmed on-chain)                    │
│                                                          │
│  Output N+1: P2WSH hashlock                              │
│    Lock: OP_SHA256 <SHA256(wish_hash)> OP_EQUAL           │
│    Value: commitment_sats                                │
└────────────────────────┬─────────────────────────────────┘
                         │
          Phase 1: Recommit (wish → product)
                         │
                         ▼
┌──────────────────────────────────────────────────────────┐
│  Recommit TX                                             │
│                                                          │
│  Input: spends wish-hash UTXO                            │
│    Witness: [wish_pixel_hash_bytes, wish_redeem_script]  │
│                                                          │
│  Output 0: P2WSH hashlock                                │
│    Lock: OP_SHA256 <SHA256(product_hash)> OP_EQUAL        │
│    Value: commitment_sats − fee                          │
└────────────────────────┬─────────────────────────────────┘
                         │
          Phase 2: Final sweep (product → donation)
                         │
                         ▼
┌──────────────────────────────────────────────────────────┐
│  Sweep TX                                                │
│                                                          │
│  Input: spends product-hash UTXO                         │
│    Witness: [product_pixel_hash_bytes, product_redeem]   │
│                                                          │
│  Output 0: P2WPKH                                        │
│    Destination: STARLIGHT_DONATION_ADDRESS                │
│    Value: commitment_sats − 2×fee                        │
└──────────────────────────────────────────────────────────┘
```

### 12.4 Phase Routing Logic

`SweepCommitmentIfReady()` in `commitment_sweep_auto.go` routes based on proof state:

| Condition | Action |
|---|---|
| `ProductPixelHash` set, `RecommitStatus == ""` | **Phase 1**: build recommit tx (wish → product hashlock) |
| `ProductPixelHash` set, `RecommitStatus == "broadcast"` | **Wait**: recommit tx in mempool, not yet confirmed |
| `ProductPixelHash` set, `RecommitStatus == "confirmed"` | **Phase 2**: sweep product hashlock → donation address |
| `ProductPixelHash` empty | **Direct sweep** (legacy): wish hashlock → donation address |

### 12.5 On-Chain Detection

The block monitor (`block_monitor.go` → `reconcileOracleIngestions`) detects both transactions:

**Funding TX detection** — matched via:
- `funding_txid` in ingestion metadata (O(1) lookup)
- `visible_pixel_hash` in transaction witness data
- `commitment_script_hash` from output script matching

**Recommit TX detection** — matched via:
- The recommit TX's witness reveals `wish_pixel_hash_bytes` (the preimage)
- `witnessHashes()` extracts the 32-byte preimage as hex
- This matches `visible_pixel_hash` in the ingestion candidate list
- `confirmAndSweepContractTasks()` then matches `proof.RecommitTxID == tx.TxID`

Both detection paths use `ingestionCandidateBuckets()` which loads from `ListRecent("", 500)` — this includes already-confirmed ingestions, ensuring the recommit TX is discoverable even after the funding TX was confirmed in an earlier block.

### 12.6 Data Model

Fields on `MerkleProof` (in `core/smart_contract/types.go`):

```
# Wish-hash commitment (set during PSBT construction / stego reconciliation)
CommitmentPixelHash     — wish image pixel hash (hex, 64 chars)
CommitmentRedeemScript  — wish hashlock redeem script (hex)
CommitmentRedeemHash    — SHA256 of the redeem script (hex)
CommitmentVout          — output index of the wish hashlock in funding TX
CommitmentSats          — sats locked in the wish hashlock

# Product-hash recommitment (set during phase 1 broadcast)
ProductPixelHash        — product image pixel hash (hex, 64 chars)
RecommitTxID            — txid of the phase 1 recommit transaction
RecommitVout            — output index of the product hashlock
RecommitRedeemScript    — product hashlock redeem script (hex)
RecommitRedeemHash      — SHA256 of the product redeem script (hex)
RecommitAddress         — P2WSH address of the product hashlock
RecommitSats            — sats in the product hashlock output
RecommitStatus          — "", "broadcast", "confirmed"
RecommitBroadcastAt     — timestamp of phase 1 broadcast
RecommitConfirmedAt     — timestamp of phase 1 confirmation

# Final sweep (set during phase 2 broadcast / confirmation)
SweepTxID               — txid of the final sweep to donation address
SweepStatus             — "", "broadcast", "confirmed", "skipped", "failed"
SweepAttemptedAt        — timestamp of last sweep attempt
SweepError              — error message if failed/skipped
```

### 12.7 Stego Reconciliation

When a product is delivered via steganography (`stego_reconcile.go` → `upsertContractFromStegoPayload`):

- `CommitmentPixelHash` = `manifest.VisiblePixelHash` (wish image hash — used as preimage for the existing wish hashlock)
- `ProductPixelHash` = `stegoHash` (SHA256 of the delivered product stego image — used as preimage for the new product hashlock)
- `CommitmentSource` = `"wish"` (indicates the commitment was built from the wish image)

This separation ensures the PSBT's wish-hash hashlock is spendable with the wish preimage, while the product hash is stored for the recommitment phase.

### 12.8 Edge Cases

**No donation (`CommitmentSats == 0`)**: PSBT builder skips the hashlock output entirely. No `CommitmentRedeemScript` or `CommitmentVout` is set. Phase 1 guard detects empty commitment data and marks `SweepStatus = "skipped"`.

**Dust threshold**: If the commitment amount minus fees falls below 546 sats at any phase, `BuildRecommitSweepTx` / `BuildCommitmentSweepTx` returns "output below dust" and the sweep is marked "skipped".

**Backward compatibility**: Existing contracts without `ProductPixelHash` fall through to `sweepDirect` — the original single-phase sweep that sends the wish hashlock directly to the donation address.

**No `STARLIGHT_DONATION_ADDRESS`**: If the env var is empty, all sweeps are marked "skipped" immediately.

### 12.9 Security Considerations

- **Bare hashlock (no signature)**: Anyone who knows the preimage can spend the UTXO. The wish image preimage is derivable from the published wish image. The product image preimage is derivable from the delivered product. This is acceptable because the donation commitment is intended to be swept by the system, not held as a secure store of value.
- **No timelock / refund path**: There is no `OP_CHECKLOCKTIMEVERIFY` or refund mechanism. If the system fails to sweep, the sats remain locked forever (recoverable only by someone who knows or can derive the preimage).
- **Double-hash commitment**: The redeem script uses `SHA256(preimage)` where the preimage is itself a SHA256 hash, creating a double-hash barrier. However, since preimages are derived from published images, this is a proof-of-knowledge mechanism, not a security boundary.

### 12.10 Source Files

| File | Role |
|---|---|
| `backend/bitcoin/psbt_builder.go` | PSBT construction, `buildHashlockRedeemScript()` |
| `backend/bitcoin/commitment_sweep.go` | `BuildCommitmentSweepTx()`, `BuildRecommitSweepTx()` |
| `backend/bitcoin/commitment_sweep_auto.go` | `SweepCommitmentIfReady()`, phase routing |
| `backend/bitcoin/block_monitor.go` | On-chain detection, `confirmAndSweepContractTasks()` |
| `backend/middleware/smart_contract/stego_reconcile.go` | Sets `ProductPixelHash` and `CommitmentPixelHash` |
| `backend/middleware/smart_contract/funding_sync.go` | Periodic proof refresh and sweep trigger |
| `backend/core/smart_contract/types.go` | `MerkleProof` struct with all commitment fields |
