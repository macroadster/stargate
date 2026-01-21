package services

import (
	"context"
	"fmt"

	"stargate-backend/bitcoin"
	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	"stargate-backend/services"
)

// TaskService handles task-related business logic
type TaskService struct {
	store        smartstore.Store
	ingestionSvc *services.IngestionService
}

// NewTaskService creates a new task service
func NewTaskService(store smartstore.Store, ingestionSvc *services.IngestionService) *TaskService {
	return &TaskService{
		store:        store,
		ingestionSvc: ingestionSvc,
	}
}

// PublishTasksForTaskID attempts to publish tasks from proposals that reference the task ID
func (s *TaskService) PublishTasksForTaskID(ctx context.Context, taskID string) error {
	// TODO: Extract the publishing logic from original server.go
	// This is a placeholder for the complex publishing logic

	// The original logic involves:
	// 1. Finding proposals that reference this task
	// 2. Validating proposal status and approval
	// 3. Creating tasks from proposal task definitions
	// 4. Updating proposal and task states

	return fmt.Errorf("task publishing logic not yet extracted")
}

// ResolvePixelHashFromIngestion resolves pixel hash from ingestion record
func (s *TaskService) ResolvePixelHashFromIngestion(rec *services.IngestionRecord, normalize func([]byte) []byte) []byte {
	if rec == nil {
		return nil
	}

	for _, key := range []string{"pixel_hash", "payout_script_hash", "visible_pixel_hash"} {
		if v, ok := rec.Metadata[key].(string); ok {
			// TODO: Extract pixel hash resolution logic
			// This involves hex decoding and normalization
			_ = v // placeholder
		}
	}

	// TODO: Extract image processing and hash generation logic
	return nil
}

// UpdateTaskCommitmentProof updates task commitment proof after PSBT creation
func (s *TaskService) UpdateTaskCommitmentProof(ctx context.Context, taskID string, res *bitcoin.PSBTResult, pixelBytes []byte) error {
	task, err := s.store.GetTask(taskID)
	if err != nil {
		return err
	}

	proof := task.MerkleProof
	if proof == nil {
		proof = &smart_contract.MerkleProof{}
	}

	// TODO: Extract proof update logic from original server.go
	// This involves updating various proof fields based on PSBT result

	_ = res        // placeholder
	_ = pixelBytes // placeholder

	return s.store.UpdateTaskProof(ctx, taskID, proof)
}
