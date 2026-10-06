package mcp

import (
	"context"
	"strings"
	"testing"

	"stargate-backend/core/smart_contract"
	scstore "stargate-backend/storage/smart_contract"
)

// irl.7: create_task took an apiKey argument and never used it for
// authorization, so any self-issued key could add work to anyone's contract and
// draw down its budget. create_task exists only on this surface; REST has no
// task-creation route.

// seedOwnedContract creates the contract for the wish whose creator is
// surfaceCreatorWlt, so a refusal can only come from the actor check.
func seedOwnedContract(t *testing.T, store scstore.Store) string {
	t.Helper()
	contractID := "wish-" + surfaceWishHash
	if err := store.UpsertContractWithTasks(context.Background(), smart_contract.Contract{
		ContractID: contractID,
		Title:      "Wish with a creator",
		Status:     "active",
	}, nil); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	return contractID
}

func TestMCPCreateTaskRejectsNonOwner(t *testing.T) {
	srv, store := surfaceFixture(t)
	contractID := seedOwnedContract(t, store)

	resp := callTool(t, srv, surfaceStrangerKey, "create_task", map[string]interface{}{
		"contract_id": contractID,
		"title":       "Task added by a stranger",
		"description": "work",
		"budget_sats": float64(1000),
	})

	if resp.Success {
		t.Fatal("a non-owner must not be able to add tasks to someone else's contract")
	}
	if resp.ErrorCode != ErrCodeUnauthorized {
		t.Fatalf("error_code = %q, want %q (body: %s)", resp.ErrorCode, ErrCodeUnauthorized, resp.Error)
	}

	// The error alone would not prove the task was never written: the point of
	// the bug was the UpsertTask that ran regardless.
	tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if strings.Contains(task.Title, "stranger") {
			t.Fatalf("task %s was written despite the refusal", task.TaskID)
		}
	}
}

func TestMCPCreateTaskStillAllowsOwner(t *testing.T) {
	srv, store := surfaceFixture(t)
	contractID := seedOwnedContract(t, store)

	resp := callTool(t, srv, surfaceCreatorKey, "create_task", map[string]interface{}{
		"contract_id": contractID,
		"title":       "Task added by the creator",
		"description": "work",
		"budget_sats": float64(1000),
	})

	if !resp.Success {
		t.Fatalf("the contract's wish creator must still be able to add tasks: %s", resp.Error)
	}
	tasks, err := store.ListTasks(smart_contract.TaskFilter{ContractID: contractID})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	found := false
	for _, task := range tasks {
		if strings.Contains(task.Title, "creator") {
			found = true
		}
	}
	if !found {
		t.Fatal("the creator's task was not written despite the success response")
	}
}
