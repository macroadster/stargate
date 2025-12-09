package mcp

import (
	"context"
	"time"
)

var (
	ErrTaskNotFound  = Err("task not found")
	ErrClaimNotFound = Err("claim not found")
	ErrTaskTaken     = Err("task already claimed by another agent")
)

// Err is a simple string error helper.
type Err string

func (e Err) Error() string { return string(e) }

// Store abstracts MCP persistence.
type Store interface {
	ListContracts(status string, skills []string) ([]Contract, error)
	ListTasks(filter TaskFilter) ([]Task, error)
	GetTask(id string) (Task, error)
	GetContract(id string) (Contract, error)
	ClaimTask(taskID, aiID string, estimatedCompletion *time.Time) (Claim, error)
	SubmitWork(claimID string, deliverables map[string]interface{}, proof map[string]interface{}) (Submission, error)
	TaskStatus(taskID string) (map[string]interface{}, error)
	GetTaskProof(taskID string) (*MerkleProof, error)
	ContractFunding(contractID string) (Contract, []MerkleProof, error)
	Close()
	UpdateTaskProof(ctx context.Context, taskID string, proof *MerkleProof) error
	// Proposal operations
	CreateProposal(ctx context.Context, p Proposal) error
	ListProposals(ctx context.Context, filter ProposalFilter) ([]Proposal, error)
	GetProposal(ctx context.Context, id string) (Proposal, error)
	ApproveProposal(ctx context.Context, id string) error
	PublishProposal(ctx context.Context, id string) error
	ListSubmissions(ctx context.Context, taskIDs []string) ([]Submission, error)
	UpdateSubmissionStatus(ctx context.Context, submissionID, status string) error
}
