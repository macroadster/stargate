package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// EventService handles event broadcasting and management
type EventService struct {
	store     smartstore.Store
	eventChan chan smart_contract.Event
}

// NewEventService creates a new event service
func NewEventService(store smartstore.Store) *EventService {
	return &EventService{
		store:     store,
		eventChan: make(chan smart_contract.Event, 100),
	}
}

// GetEventChannel returns the event channel for broadcasting
func (s *EventService) GetEventChannel() chan smart_contract.Event {
	return s.eventChan
}

// BroadcastEvent broadcasts an event to all listeners
func (s *EventService) BroadcastEvent(evt smart_contract.Event) {
	select {
	case s.eventChan <- evt:
	default:
		// Channel full, drop event
	}
}

// PublishProposalTasks publishes tasks from a proposal and records events
func (s *EventService) PublishProposalTasks(ctx context.Context, proposalID string) error {
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}

	if len(p.Tasks) == 0 {
		// Try to derive tasks from metadata embedded_message
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}

	// Build a contract from proposal, then upsert tasks
	contractID := s.contractIDFromMeta(p.Metadata, p.ID)
	contract := smart_contract.Contract{
		ContractID:          contractID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}

	// Preserve hashes/funding if present
	fundingAddr := scstore.FundingAddressFromMeta(p.Metadata)
	tasks := make([]smart_contract.Task, 0, len(p.Tasks))
	for i, t := range p.Tasks {
		task := t
		if strings.TrimSpace(task.TaskID) == "" {
			task.TaskID = proposalID + "-task-" + strconv.Itoa(i+1)
		}
		if task.ContractID == "" || task.ContractID == p.ID {
			task.ContractID = contractID
		}
		if task.MerkleProof == nil && p.VisiblePixelHash != "" {
			task.MerkleProof = &smart_contract.MerkleProof{
				VisiblePixelHash:   p.VisiblePixelHash,
				FundedAmountSats:   p.BudgetSats / int64(len(p.Tasks)),
				FundingAddress:     fundingAddr,
				ConfirmationStatus: "provisional",
			}
		}
		if task.MerkleProof != nil && task.MerkleProof.FundingAddress == "" {
			task.MerkleProof.FundingAddress = fundingAddr
		}
		tasks = append(tasks, task)
	}

	if pg, ok := s.store.(interface {
		UpsertContractWithTasks(context.Context, smart_contract.Contract, []smart_contract.Task) error
	}); ok {
		if err := pg.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
			return err
		}

		// Record and broadcast events
		s.BroadcastEvent(smart_contract.Event{
			Type:      "contract_upsert",
			EntityID:  contract.ContractID,
			Actor:     "system",
			Message:   fmt.Sprintf("contract upserted with %d tasks", len(tasks)),
			CreatedAt: time.Now(),
		})

		s.BroadcastEvent(smart_contract.Event{
			Type:      "publish",
			EntityID:  proposalID,
			Actor:     "system",
			Message:   "proposal tasks published",
			CreatedAt: time.Now(),
		})

		return nil
	}

	return nil
}

// contractIDFromMeta generates contract ID from metadata
func (s *EventService) contractIDFromMeta(meta map[string]interface{}, proposalID string) string {
	if v, ok := meta["contract_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return "contract-" + proposalID
}
