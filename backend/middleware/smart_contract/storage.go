package smart_contract

import (
	"context"
	"time"

	"stargate-backend/core/smart_contract"
)

var (
	ErrTaskNotFound    = Err("task not found")
	ErrClaimNotFound   = Err("claim not found")
	ErrTaskTaken       = Err("task already claimed by another agent")
	ErrTaskUnavailable = Err("task is not available for claiming")
)

// Err is a simple string error helper.
type Err string

func (e Err) Error() string { return string(e) }

// Store abstracts MCP persistence.
type Store interface {
	ListContracts(filter smart_contract.ContractFilter) ([]smart_contract.Contract, error)
	ListTasks(filter smart_contract.TaskFilter) ([]smart_contract.Task, error)
	GetTask(id string) (smart_contract.Task, error)
	GetContract(id string) (smart_contract.Contract, error)
	ClaimTask(taskID, aiID, contractorWallet string, estimatedCompletion *time.Time) (smart_contract.Claim, error)
	SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (smart_contract.Submission, error)
	TaskStatus(taskID string) (map[string]interface{}, error)
	GetTaskProof(taskID string) (*smart_contract.MerkleProof, error)
	ContractFunding(contractID string) (smart_contract.Contract, []smart_contract.MerkleProof, error)
	Close()
	UpdateTaskProof(ctx context.Context, taskID string, proof *smart_contract.MerkleProof) error
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
}
