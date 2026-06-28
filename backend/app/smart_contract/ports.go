package smart_contract

import (
	"context"

	"stargate-backend/stego"
)

// Domain seams: interfaces that define how stego, ingestion, bitcoin confirmations,
// and contract storage collaborate without importing each other circularly.
//
//	┌─────────────┐     Prepare/Finalize      ┌──────────────┐
//	│ PSBT / REST │ ─────────────────────────► │ StegoPublish │  (this package)
//	└─────────────┘                            └──────┬───────┘
//	                                                  │ artifacts + manifest
//	┌─────────────┐     ReconcileStego(cid,hash)     ▼
//	│ BlockMonitor│ ◄── StegoReconciler ────── ┌──────────────┐
//	│ (bitcoin)   │     (injected at wiring)   │ StegoReconcile│
//	└──────┬──────┘                            └──────┬───────┘
//	       │ confirm + match ingestions               │ UpsertFromPayload
//	       ▼                                          ▼
//	┌─────────────┐                            ┌──────────────┐
//	│ Ingestion   │ ◄── ensureStegoIngestion ──│ Store        │
//	│ Service     │                            │ (storage)    │
//	└─────────────┘                            └──────────────┘
//
// Identity join key: core/identity (visible pixel hash / wish-<hash>).

// StegoPublishPort is implemented by Server (Prepare/Finalize around PSBT).
// Bitcoin and agents should depend on this interface, not on Server methods.
type StegoPublishPort interface {
	PreparePublishArtifacts(ctx context.Context, proposalID string) (*PublishArtifacts, error)
	FinalizePublishArtifacts(ctx context.Context, proposalID string, artifacts *PublishArtifacts)
}

// StegoReconcilePort is the app-side reconcile entry (CID or local hash).
// bitcoin.BlockMonitor injects a thinner StegoReconciler (CID+hash only).
type StegoReconcilePort interface {
	ReconcileStego(ctx context.Context, stegoCID, expectedHash string) error
	ReconcileStegoWithAnnouncement(ctx context.Context, ann *stegoAnnouncement) error
}

// ContractFromStegoPort upserts proposal/contract/tasks from a decoded stego payload.
// Keeps manifest→store mapping in one place (stego reconcile / IPFS ingest).
type ContractFromStegoPort interface {
	UpsertContractFromStegoPayload(ctx context.Context, contractID, stegoCID, stegoHash string, manifest stego.Manifest, payload stego.Payload) error
}

// Ensure Server implements the publish/reconcile ports (compile-time checks).
var (
	_ StegoPublishPort     = (*Server)(nil)
	_ StegoReconcilePort   = (*Server)(nil)
	_ ContractFromStegoPort = (*Server)(nil)
)
