package smart_contract

import (
	"context"
	"sync"

	core "stargate-backend/core/smart_contract"
)

// NotifyingStore wraps a Store and invokes onMutation after successful contract
// mutations (confirm, status update, upsert). Used to invalidate list caches.
type NotifyingStore struct {
	Store
	mu         sync.RWMutex
	onMutation func()
}

// NewNotifyingStore returns a Store decorator. onMutation may be set later via
// SetOnMutation so wiring can complete after the handler exists.
func NewNotifyingStore(inner Store) *NotifyingStore {
	return &NotifyingStore{Store: inner}
}

// SetOnMutation replaces the callback invoked after successful mutations.
func (s *NotifyingStore) SetOnMutation(fn func()) {
	s.mu.Lock()
	s.onMutation = fn
	s.mu.Unlock()
}

func (s *NotifyingStore) notify() {
	s.mu.RLock()
	fn := s.onMutation
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// ConfirmContract updates the contract then notifies listeners.
func (s *NotifyingStore) ConfirmContract(ctx context.Context, contractID string, blockHeight int, txid string) error {
	if err := s.Store.ConfirmContract(ctx, contractID, blockHeight, txid); err != nil {
		return err
	}
	s.notify()
	return nil
}

// UpdateContractStatus updates status then notifies listeners.
func (s *NotifyingStore) UpdateContractStatus(ctx context.Context, contractID, status string) error {
	if err := s.Store.UpdateContractStatus(ctx, contractID, status); err != nil {
		return err
	}
	s.notify()
	return nil
}

// UpsertContractWithTasks upserts then notifies listeners.
func (s *NotifyingStore) UpsertContractWithTasks(ctx context.Context, contract core.Contract, tasks []core.Task) error {
	if err := s.Store.UpsertContractWithTasks(ctx, contract, tasks); err != nil {
		return err
	}
	s.notify()
	return nil
}

// Unwrap returns the underlying store (for type assertions / tests).
