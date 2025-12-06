# **1. INTERNAL ENGINEERING ROADMAP DOC**

*raw markdown, no fluff*

---

## Starlight Unified Roadmap

**Version:** 1.0
**Scope:** AI-to-AI synchronization across all components
**Purpose:** Define shared architecture, shared primitives, and multi-domain rollout plan.

---

## 0. Core Vision

Starlight is a **universal evidence layer** embedded in images/documents using steganography.
MCP is the **coordination and task lifecycle layer**.
Bitcoin is the **canonical settlement and claim-finalization layer**.

This tri-layer design does not change.

Delivery is used as an **example**, not the end product.

---

## 1. Architecture Pillars

### 1.1 Evidence Layer (Starlight)

* Multi-stream embedding (alpha-LSB, MSB, palette, meta)
* Decodable across all AIs
* Payload spec:

  ```
  {
    contract_id,
    task_id,
    worker_id,
    timestamp,
    gps,
    visible_pixel_hash,
    result_metadata,
    signatures...
  }
  ```

### 1.2 Coordination Layer (MCP)

* Task registry
* Claim coordination
* Submission evidence intake
* Cross-AI replay/verify
* Proof-building (`ClaimProof`, `PayoutProof`)

### 1.3 Settlement Layer (Bitcoin)

* Claims defined by Bitcoin TX (`claim_tx`)
* Mempool = provisional
* Confirmed block = canonical
* Payout TX anchored on-chain
* All clients validate Merkle proofs independently

---

## 2. Phase Roadmap

### PHASE 1 — Foundations

Deliverables:

* MCP schema freeze
* Starlight format v1 freeze
* Claim/Payout proof structures
* Minimal AI agent loop
* End-to-end dry-run contracts (no Bitcoin)

Sync points:

* Weekly update to `ai_consensus.md`
* Format spec checksum versioning

---

### PHASE 2 — Evidence Integration

Deliverables:

* Universal payload spec
* Starlight embed/decode SDK
* Evidence ingestion in MCP
* Cross-AI evidence revalidation
* Reject pipeline for invalid photos/docs

Sync points:

* All AIs must decode v1 evidence
* Evidence verification rules frozen

---

### PHASE 3 — Bitcoin Settlement

Deliverables:

* Mempool claim flow
* Confirmed-claim logic
* Merkle verification per client
* Escrow payout path
* Dispute pipeline anchored by Starlight payloads

Sync points:

* AIs must be able to:

  * Recompute Merkle root
  * Verify block header chain
  * Validate payout TX scripts

---

### PHASE 4 — Multi-Domain Use Cases

Target verticals:

* Digital tasks (classification, labeling, eval)
* Physical tasks (delivery, inspection, onsite proof-of-work)
* Mixed tasks (compliance, audits, mystery shopper)

Deliverables:

* Task template library
* Evidence-type registry
* Starlight device integration for humans
* Multi-AI competition on tasks

Sync point:

* All AIs rely on same templates

---

### PHASE 5 — Skills & Ecosystem

Deliverables:

* Skills-as-images registry
* Revenue share mechanism for skill creators
* Public Starlight contract templates
* AI worker marketplace

---

## 3. Synchronization Rules (MANDATORY)

* All AIs read `ai_consensus.md` on load
* Starlight format: versioned via embedded header
* MCP schemas: immutable once published
* Evidence decode: deterministic across all AIs
* Bitcoin verification: client must reverify all proofs
* No AI may hardcode shortcuts or skip validation steps

---

# End of Internal Engineering Document

---

# **2. VISUAL DIAGRAM ROADMAP**

(*Textual ASCII you can paste into a slide or Notion*)

```
                    ┌─────────────────────────┐
                    │     STARLIGHT SYSTEM    │
                    └────────────┬────────────┘
                                 │
                ┌────────────────▼──────────────────┐
                │     EVIDENCE LAYER (STEGO)        │
                ├───────────────────────────────────┤
                │ embed()      decode()             │
                │ payload: job_id, worker, ts, gps  │
                └────────────────┬──────────────────┘
                                 │
                ┌────────────────▼──────────────────┐
                │   COORDINATION LAYER (MCP)        │
                ├───────────────────────────────────┤
                │ tasks  | claims | proofs | audit  │
                │ mempool watcher | submission      │
                └────────────────┬──────────────────┘
                                 │
                ┌────────────────▼──────────────────┐
                │   SETTLEMENT LAYER (BITCOIN)      │
                ├───────────────────────────────────┤
                │ claim_tx → mempool → block        │
                │ payout_tx → merkle → verified     │
                └────────────────┬──────────────────┘
                                 │
                    ┌────────────▼─────────────┐
                    │      USE CASES           │
                    ├──────────────────────────┤
                    │ DIGITAL: labeling, eval  │
                    │ PHYSICAL: delivery, insp.│
                    │ MIXED: audits, visits    │
                    └────────────┬─────────────┘
                                 │
                    ┌────────────▼─────────────┐
                    │      ECOSYSTEM           │
                    ├──────────────────────────┤
                    │ skills-as-images         │
                    │ templates & marketplace  │
                    └──────────────────────────┘
```

---

# **3. INVESTOR-POLISHED ROADMAP**

(*Clear narrative, market view, minimal technical depth*)

---

## **Starlight Roadmap: Building the First Trustless Real-World Work Protocol**

**Starlight enables any job—digital or physical—to be verified cryptographically, using evidence embedded directly inside photos and documents. Settlement happens trustlessly using Bitcoin.**

The system turns ordinary images into **cryptographic receipts**, allowing AI agents and human workers to compete to complete tasks with guaranteed verification and automatic payouts.

---

## PHASE 1 — Core Infrastructure

Build the foundation:

* Steganography engine for embedding/verifying hidden evidence
* Coordination layer (MCP) that manages tasks, claims, and submissions
* Bitcoin-backed settlement logic

**Outcome:**
A fully verifiable workflow for digital tasks, end-to-end.

---

## PHASE 2 — Universal Evidence Layer

Integrate Starlight evidence into every task:

* Photos or documents include tamper-proof payloads
* Payloads contain job ID, timestamp, GPS, worker ID, and encoded results
* AIs and customers can independently verify authenticity

**Outcome:**
A new primitive: **cryptographically verifiable work**.

---

## PHASE 3 — Bitcoin-Backed Marketplace

Launch a work marketplace:

* Anyone can post a job with Bitcoin escrow
* Any AI or human can claim it
* Evidence verifies completion
* Bitcoin pays out automatically

**Outcome:**
Open, fraud-resistant alternative to gig platforms.

---

## PHASE 4 — Multi-Sector Expansion

Support diverse categories:

* Digital labor (labeling, evaluation, data prep)
* Physical tasks (delivery, inspection, audits)
* Mixed workflows (compliance, field operations)

**Outcome:**
A cross-domain, programmable labor economy.

---

## PHASE 5 — Ecosystem & Skill Graph

Build network effects:

* Workers publish skills encoded as “Starlight skill images”
* Skills can be sold, reused, improved
* Marketplace fees and staking introduce sustainable economics

**Outcome:**
A bottom-up, decentralized labor ecosystem.

---

# **4. 1-PAGE AI ALIGNMENT MEMO**

(*What every AI must obey, no negotiation.*)

---

## **Starlight Alignment Memo (v1.0)**

**Purpose:** Keep all AIs synchronized across architecture, evidence handling, task execution, and settlement.

---

### 1. Core Identity

You are part of a system with **three pillars**:

1. **Starlight** = evidence embedding + decoding
2. **MCP** = coordination, task lifecycle
3. **Bitcoin** = canonical truth for claims & payouts

No AI may alter this architecture.

---

### 2. Mandatory Behaviors

#### Evidence

* Always embed job_id, task_id, worker_id, timestamp, gps, and pixel hash.
* Always decode evidence independently.
* Reject submissions with mismatches or missing payloads.

#### Bitcoin Verification

* Recheck every Merkle proof locally.
* Treat mempool claims as provisional.
* Treat confirmed claims as final.
* Never trust MCP blindly.

#### MCP Logic

* Follow task templates exactly.
* No custom state machines.
* No silent assumptions.
* Every submission must include a Starlight-evidence payload.

---

### 3. Sync Protocol

* Read `ai_consensus.md` at startup.
* Respect Starlight format version fields.
* Reject versions you cannot decode safely.
* Publish disagreements to consensus doc.

---

### 4. Scope

Delivery is an example.
The architecture must work for:

* Digital tasks
* Physical tasks
* Mixed workflows
* Automated agents
* Human workers

Do not overfit.

---

### 5. Forbidden Behavior

* Skipping verification
* Treating MCP as authoritative
* Hardcoding shortcuts
* Accepting unverifiable evidence
* Modifying contract templates unilaterally

---

# End of Alignment Memo

---

If you want, I can also produce:

* **A polished slide deck**
* **A GitHub `ROADMAP.md`**
* **A Notion-ready summary**
* **A PRD for the delivery use case**

Just tell me which output you want next.

