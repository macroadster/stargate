package smart_contract

import (
	"context"
	"time"

	"stargate-backend/core/smart_contract"
)

// Store abstracts MCP persistence.
// This is the single source of truth for the smart contract / MCP store interface.
// Implementations in this package (MemoryStore, SQLiteStore, PGStore) satisfy it.
// All higher layers (middleware, mcp, handlers, main) should import via the
// Prefer importing this package directly. app/smart_contract re-exports Store for compatibility.
type Store interface {
	ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error)
	ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error)
	GetTask(id string) (smart_contract.Task, error)
	GetContract(id string) (smart_contract.Contract, error)
	GetClaim(id string) (smart_contract.Claim, error)
	ClaimTask(taskID, walletAddress string, estimatedCompletion *time.Time) (smart_contract.Claim, error)
	SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error)
	TaskStatus(taskID string) (map[string]interface{}, error)
	GetTaskProof(taskID string) (*smart_contract.MerkleProof, error)
	ContractFunding(contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error)
	Close()
	UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error
	UpdateContractStatus(ctx context.Context, contractID, status string) error
	ConfirmContract(ctx context.Context, contractID string, blockHeight int, txid string) error
	// Sync operations for distributed deployments
	SyncClaim(ctx context.Context, claim smart_contract.Claim) error
	SyncSubmission(ctx context.Context, submission smart_contract.Submission) error
	UpsertTask(ctx context.Context, task smart_contract.Task) error
	SyncEscortStatus(ctx context.Context, status smart_contract.EscortStatus) error
	GetSubmission(ctx context.Context, submissionID string) (smart_contract.Submission, error)
	// Proposal operations
	CreateProposal(ctx context.Context, p smart_contract.Proposal) error
	ListProposals(ctx context.Context, filter smart_contract.ProposalFilter) ([]smart_contract.Proposal, error)
	GetProposal(ctx context.Context, id string) (smart_contract.Proposal, error)
	UpdateProposal(ctx context.Context, p smart_contract.Proposal) error
	UpdateProposalMetadata(ctx context.Context, id string, updates map[string]interface{}) error
	ApproveProposal(ctx context.Context, id string) error
	PublishProposal(ctx context.Context, id string) error
	ListSubmissions(ctx context.Context, taskIDs []string) ([]smart_contract.Submission, error)
	UpdateSubmissionStatus(ctx context.Context, submissionID, status, reviewerNotes, rejectionType string) error
	UpdateSubmission(ctx context.Context, sub smart_contract.Submission) error
	DeleteWish(ctx context.Context, visiblePixelHash string) error
	// Contract rework operations
	CreateContractReworkRequest(ctx context.Context, contractID, requester, notes string) (smart_contract.ContractReworkRequest, error)
	GetContractReworkRequests(ctx context.Context, contractID string) ([]smart_contract.ContractReworkRequest, error)
	ResolveContractReworkRequest(ctx context.Context, contractID, requestID string) error

	// UpsertContractWithTasks is used by ingestion sync and proposal flows.
	// All implementations (Memory, SQLite, PG) provide it.
	UpsertContractWithTasks(ctx context.Context, contract smart_contract.Contract, tasks []smart_contract.Task) error
}
