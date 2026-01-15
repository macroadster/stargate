# Improving the Starlight MCP Server: A Post-mortem and Recommendations

## 1. Introduction

This document summarizes the findings from a collaborative session with an AI agent to interact with the Starlight MCP (Model-Contract-Protocol) server. While the core workflow of the server (Wish > Proposal > Tasks > Submission) was successfully navigated, the process was hindered by several undocumented requirements and API behaviors.

The session culminated in a critical failure at the final payment step: the user was unable to build the PSBT (Partially Signed Bitcoin Transaction) to pay the contractor, a functionality that appears to have regressed after recent security fixes.

The purpose of this document is to provide a detailed account of the issues encountered and to offer actionable recommendations for the engineering team to improve the system's robustness, usability, and documentation.

## 2. Summary of Issues Encountered

### 2.1. Proposal and Task Workflow

*   **Task-less Proposals**: The server allows the approval of proposals that do not contain a structured `tasks` list. This leads to a dead-end state where the contract is not activated, and no tasks are generated.
*   **Unbudgeted Tasks**: The server allows the creation of proposals with tasks that have no `budget_sats` assigned. This leads to the current payment issue, where there is no budget to be paid out.
*   **Inability to Update Proposals**: Once a proposal is approved, no other proposal can be approved for the same contract. This is a rigid constraint that makes it difficult to recover from mistakes in the proposal creation process.

### 2.2. API Documentation

*   The API documentation at `/mcp/docs` is incomplete and, in some cases, misleading.
    *   The example for creating a proposal is missing the `tasks` array, which is mandatory for activating a contract.
    *   The documentation does not mention the `metadata.visible_pixel_hash` field, which was required to resolve the "image scan metadata" error during proposal creation.
*   The process for associating a contractor's wallet address with their work is not documented.

## 3. Analysis of the PSBT Creation Failure (The Regression)

The most critical issue is the user's inability to build a PSBT for payment, which they suspect is a regression.

### 3.1. The Problem

After all work submissions were approved, the user was unable to build the PSBT to pay the contractor. This is the final and most crucial step of the workflow.

### 3.2. What We Know

*   The user is responsible for building the PSBT.
*   The total payment amount should be 3000 sats (1000 for each of the 3 tasks).
*   The contractor's wallet address is associated with their API key on the server side, and there is a challenge-response mechanism for wallet verification.

### 3.3. The Likely Cause of the Regression

Given the context of recent "security fixes" and the code snippets provided (especially the `secure_tool` decorator and the `ResponseSanitizer`), it is highly likely that the regression is caused by an over-aggressive sanitization of the server's responses.

The `secure_tool` decorator and its `ResponseSanitizer` are designed to redact Personally Identifiable Information (PII) from API responses. The tests for this feature show that it redacts emails, IP addresses, AWS keys, etc. It is very likely that **the contractor's Bitcoin wallet address is being incorrectly identified as PII and redacted** from the response that the user's PSBT building tool consumes.

The user's tool for building the PSBT likely calls an API endpoint to get the payment details (recipient address and amount). If the recipient address is redacted from the response, the tool will not have the necessary information to construct the transaction, leading to the "no way to pay out" error.

## 4. Recommendations for Improvement

### 4.1. For the PSBT Creation Failure

*   **Immediate Fix**:
    *   Review the `ResponseSanitizer` logic and its configuration.
    *   Ensure that Bitcoin wallet addresses are **not** redacted from the responses of the API endpoints that provide payment information.
    *   Add a test case to the `TestSecurityIntegration` to verify that a contractor's wallet address is correctly returned by the payment information endpoint.
*   **Long-term Fix**:
    *   Create a dedicated and well-documented API endpoint (e.g., `GET /api/smart_contract/contracts/{id}/payment_details`) that returns all the necessary information for building a PSBT, including the recipient's address, the total amount, and any other relevant details.
    *   This endpoint should be secured appropriately, but it should not redact the information that is essential for its intended purpose.

### 4.2. For the Proposal and Task Workflow

*   **Improve Validation**:
    *   The server should validate proposals more strictly upon submission. A proposal for a contract that requires tasks should be rejected if it doesn't contain a non-empty `tasks` array with budgeted tasks.
*   **Allow Proposal Updates**:
    *   Consider implementing a mechanism to update a proposal before it is approved. This would make the system more forgiving of user mistakes.

### 4.3. For the API Documentation

*   **Update and Complete**:
    *   The API documentation at `/mcp/docs` must be thoroughly updated to be accurate and complete.
    *   All examples, especially for creating proposals, should be comprehensive and include all required fields (`tasks`, `budget_sats` per task, `metadata`, etc.).
*   **Document the Payment Workflow**:
    *   Create a dedicated section in the documentation that explains the entire payment workflow, from the association of the wallet address with the API key to the building of the PSBT.
    *   This section should also reference the new `payment_details` endpoint recommended above.
