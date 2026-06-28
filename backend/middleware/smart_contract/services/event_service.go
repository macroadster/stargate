package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// EventRecorder records domain events (wired to Server.recordEvent).
type EventRecorder func(evt smart_contract.Event)

// EventService handles proposal→task publishing and event emission.
type EventService struct {
	store  scstore.Store
	record EventRecorder
}

// NewEventService creates an EventService. record may be nil (events are dropped).
func NewEventService(store scstore.Store, record EventRecorder) *EventService {
	return &EventService{store: store, record: record}
}

// SetRecorder updates the event sink (useful when Server is constructed first).
func (s *EventService) SetRecorder(record EventRecorder) {
	s.record = record
}

func (s *EventService) emit(evt smart_contract.Event) {
	if s.record != nil {
		s.record(evt)
	}
}

// PublishProposalTasks publishes tasks from a proposal into the contract store and records events.
func (s *EventService) PublishProposalTasks(ctx context.Context, proposalID string) error {
	if s.store == nil {
		return nil
	}
	p, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		if em, ok := p.Metadata["embedded_message"].(string); ok && em != "" {
			p.Tasks = scstore.BuildTasksFromMarkdown(p.ID, em, p.VisiblePixelHash, p.BudgetSats, scstore.FundingAddressFromMeta(p.Metadata))
		}
		if len(p.Tasks) == 0 {
			return nil
		}
	}

	contractID := ContractIDFromMeta(p.Metadata, p.ID)
	contract := smart_contract.Contract{
		ContractID:          contractID,
		Title:               p.Title,
		TotalBudgetSats:     p.BudgetSats,
		GoalsCount:          1,
		AvailableTasksCount: len(p.Tasks),
		Status:              "active",
	}

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
		s.emit(smart_contract.Event{
			Type:      "contract_upsert",
			EntityID:  contract.ContractID,
			Actor:     "system",
			Message:   fmt.Sprintf("contract upserted with %d tasks", len(tasks)),
			CreatedAt: time.Now(),
		})
		s.emit(smart_contract.Event{
			Type:      "publish",
			EntityID:  proposalID,
			Actor:     "system",
			Message:   "proposal tasks published",
			CreatedAt: time.Now(),
		})
	}
	return nil
}

// ContractIDFromMeta returns contract_id from metadata or a derived id.
func ContractIDFromMeta(meta map[string]interface{}, proposalID string) string {
	if v, ok := meta["contract_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return "contract-" + proposalID
}
