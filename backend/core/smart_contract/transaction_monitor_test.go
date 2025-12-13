package smart_contract

import (
	"context"
	"testing"
	"time"
)

// TestNewTransactionMonitor tests the constructor
func TestNewTransactionMonitor(t *testing.T) {
	bitcoinRPC := "http://localhost:8332"

	tm := NewTransactionMonitor(bitcoinRPC)

	if tm == nil {
		t.Fatal("Expected transaction monitor but got nil")
	}

	if tm.bitcoinRPC != bitcoinRPC {
		t.Errorf("Expected Bitcoin RPC '%s' but got '%s'", bitcoinRPC, tm.bitcoinRPC)
	}

	if tm.httpClient == nil {
		t.Error("Expected HTTP client but got nil")
	}

	if tm.monitoredTxs == nil {
		t.Error("Expected monitored transactions map but got nil")
	}

	if tm.eventHandlers == nil {
		t.Error("Expected event handlers map but got nil")
	}

	if tm.checkInterval != 2*time.Minute {
		t.Errorf("Expected check interval 2 minutes but got %v", tm.checkInterval)
	}
}

// TestAddTransaction tests adding transactions to monitoring
func TestAddTransaction(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	t.Run("Valid transaction", func(t *testing.T) {
		tx := &MonitoredTransaction{
			TxID:          "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
			ContractID:    "contract123",
			Type:          "funding",
			Status:        "pending",
			RequiredConfs: 6,
			AmountSats:    100000,
			FromAddress:   "bc1q...",
			ToAddress:     "bc1q...",
			ScriptHex:     "76a914...",
			Metadata:      make(map[string]any),
		}

		err := tm.AddTransaction(tx)

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Check transaction was added
		storedTx, exists := tm.monitoredTxs[tx.TxID]
		if !exists {
			t.Error("Expected transaction to be stored")
		}

		if storedTx.ContractID != tx.ContractID {
			t.Errorf("Expected contract ID '%s' but got '%s'", tx.ContractID, storedTx.ContractID)
		}

		if storedTx.CreatedAt.IsZero() {
			t.Error("Expected created at to be set")
		}

		if storedTx.LastChecked.IsZero() {
			t.Error("Expected last checked to be set")
		}

		if storedTx.NextCheck.IsZero() {
			t.Error("Expected next check to be set")
		}
	})

	t.Run("Empty transaction ID", func(t *testing.T) {
		tx := &MonitoredTransaction{
			TxID:          "", // Empty TX ID
			ContractID:    "contract123",
			Type:          "funding",
			Status:        "pending",
			RequiredConfs: 6,
		}

		err := tm.AddTransaction(tx)

		if err == nil {
			t.Error("Expected error for empty transaction ID")
		}

		if err.Error() != "transaction ID is required" {
			t.Errorf("Expected specific error message but got: %v", err)
		}
	})

	t.Run("Duplicate transaction", func(t *testing.T) {
		tx := &MonitoredTransaction{
			TxID:          "duplicate123",
			ContractID:    "contract123",
			Type:          "funding",
			Status:        "pending",
			RequiredConfs: 6,
		}

		// Add first time
		err := tm.AddTransaction(tx)
		if err != nil {
			t.Errorf("Unexpected error on first add: %v", err)
		}

		// Add second time (should overwrite)
		err = tm.AddTransaction(tx)
		if err != nil {
			t.Errorf("Unexpected error on second add: %v", err)
		}

		// Should still exist
		_, exists := tm.monitoredTxs[tx.TxID]
		if !exists {
			t.Error("Expected transaction to still exist after duplicate add")
		}
	})
}

// TestAddEventHandler tests adding event handlers
func TestAddEventHandler(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	handler := func(ctx context.Context, event *TransactionEvent) error {
		return nil
	}

	t.Run("Add handler for specific event type", func(t *testing.T) {
		tm.AddEventHandler("tx_confirmed", handler)

		handlers, exists := tm.eventHandlers["tx_confirmed"]
		if !exists {
			t.Error("Expected event handlers to be created")
		}

		if len(handlers) != 1 {
			t.Errorf("Expected 1 handler but got %d", len(handlers))
		}
	})

	t.Run("Add multiple handlers for same event type", func(t *testing.T) {
		tm.AddEventHandler("tx_confirmed", handler)
		tm.AddEventHandler("tx_confirmed", handler)

		handlers, exists := tm.eventHandlers["tx_confirmed"]
		if !exists {
			t.Error("Expected event handlers to exist")
		}

		if len(handlers) != 3 {
			t.Errorf("Expected 3 handlers but got %d", len(handlers))
		}
	})

	t.Run("Add handler for wildcard event type", func(t *testing.T) {
		tm.AddEventHandler("*", handler)

		handlers, exists := tm.eventHandlers["*"]
		if !exists {
			t.Error("Expected wildcard event handlers to be created")
		}

		if len(handlers) != 1 {
			t.Errorf("Expected 1 wildcard handler but got %d", len(handlers))
		}
	})
}

// TestGetTransactionStatus tests retrieving transaction status
func TestGetTransactionStatus(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	tx := &MonitoredTransaction{
		TxID:          "test123",
		ContractID:    "contract123",
		Type:          "funding",
		Status:        "pending",
		RequiredConfs: 6,
	}

	tm.AddTransaction(tx)

	t.Run("Existing transaction", func(t *testing.T) {
		status, err := tm.GetTransactionStatus("test123")

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if status == nil {
			t.Error("Expected status but got nil")
		}

		if status.TxID != tx.TxID {
			t.Errorf("Expected TX ID '%s' but got '%s'", tx.TxID, status.TxID)
		}

		if status.Status != tx.Status {
			t.Errorf("Expected status '%s' but got '%s'", tx.Status, status.Status)
		}
	})

	t.Run("Non-existing transaction", func(t *testing.T) {
		status, err := tm.GetTransactionStatus("nonexistent")

		if err == nil {
			t.Error("Expected error for non-existing transaction")
		}

		if status != nil {
			t.Error("Expected nil status for non-existing transaction")
		}

		expectedError := "transaction nonexistent is not being monitored"
		if err.Error() != expectedError {
			t.Errorf("Expected error '%s' but got '%s'", expectedError, err.Error())
		}
	})
}

// TestRemoveTransaction tests removing transactions from monitoring
func TestRemoveTransaction(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	tx := &MonitoredTransaction{
		TxID:          "test123",
		ContractID:    "contract123",
		Type:          "funding",
		Status:        "pending",
		RequiredConfs: 6,
	}

	tm.AddTransaction(tx)

	t.Run("Existing transaction", func(t *testing.T) {
		err := tm.RemoveTransaction("test123")

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Check transaction was removed
		_, exists := tm.monitoredTxs["test123"]
		if exists {
			t.Error("Expected transaction to be removed")
		}
	})

	t.Run("Non-existing transaction", func(t *testing.T) {
		err := tm.RemoveTransaction("nonexistent")

		if err == nil {
			t.Error("Expected error for non-existing transaction")
		}

		expectedError := "transaction nonexistent is not being monitored"
		if err.Error() != expectedError {
			t.Errorf("Expected error '%s' but got '%s'", expectedError, err.Error())
		}
	})
}

// TestGetMonitoredTransactions tests retrieving all monitored transactions
func TestGetMonitoredTransactions(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	// Add some test transactions
	txs := []*MonitoredTransaction{
		{
			TxID:       "test1",
			ContractID: "contract1",
			Type:       "funding",
			Status:     "pending",
		},
		{
			TxID:       "test2",
			ContractID: "contract2",
			Type:       "claim",
			Status:     "confirmed",
		},
	}

	for _, tx := range txs {
		tm.AddTransaction(tx)
	}

	monitoredTxs := tm.GetMonitoredTransactions()

	if len(monitoredTxs) != 2 {
		t.Errorf("Expected 2 monitored transactions but got %d", len(monitoredTxs))
	}

	// Check that we got copies, not references
	originalTx := tm.monitoredTxs["test1"]
	returnedTx := monitoredTxs["test1"]

	if returnedTx.Status != originalTx.Status {
		t.Errorf("Expected returned transaction to have same status")
	}

	// Modify returned transaction and verify original is unchanged
	returnedTx.Status = "modified"
	if tm.monitoredTxs["test1"].Status == "modified" {
		t.Error("Expected original transaction to be unchanged when returned copy is modified")
	}
}

// TestGetMonitoringStats tests retrieving monitoring statistics
func TestGetMonitoringStats(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	// Add some test transactions with different statuses and types
	txs := []*MonitoredTransaction{
		{
			TxID:       "test1",
			ContractID: "contract1",
			Type:       "funding",
			Status:     "pending",
		},
		{
			TxID:       "test2",
			ContractID: "contract2",
			Type:       "funding",
			Status:     "confirmed",
		},
		{
			TxID:       "test3",
			ContractID: "contract3",
			Type:       "claim",
			Status:     "pending",
		},
	}

	for _, tx := range txs {
		tm.AddTransaction(tx)
	}

	stats := tm.GetMonitoringStats()

	// Check basic stats
	if stats["total_monitored"] != 3 {
		t.Errorf("Expected total_monitored 3 but got %v", stats["total_monitored"])
	}

	if stats["check_interval"] != "2m0s" {
		t.Errorf("Expected check_interval '2m0s' but got %v", stats["check_interval"])
	}

	if stats["service_status"] != "running" {
		t.Errorf("Expected service_status 'running' but got %v", stats["service_status"])
	}

	if stats["version"] != "1.0.0" {
		t.Errorf("Expected version '1.0.0' but got %v", stats["version"])
	}

	// Check status counts
	statusCounts, ok := stats["status_counts"].(map[string]int)
	if !ok {
		t.Error("Expected status_counts to be a map")
	} else {
		if statusCounts["pending"] != 2 {
			t.Errorf("Expected 2 pending transactions but got %d", statusCounts["pending"])
		}

		if statusCounts["confirmed"] != 1 {
			t.Errorf("Expected 1 confirmed transaction but got %d", statusCounts["confirmed"])
		}
	}

	// Check type counts
	typeCounts, ok := stats["type_counts"].(map[string]int)
	if !ok {
		t.Error("Expected type_counts to be a map")
	} else {
		if typeCounts["funding"] != 2 {
			t.Errorf("Expected 2 funding transactions but got %d", typeCounts["funding"])
		}

		if typeCounts["claim"] != 1 {
			t.Errorf("Expected 1 claim transaction but got %d", typeCounts["claim"])
		}
	}
}

// TestSetCheckInterval tests updating the check interval
func TestSetCheckInterval(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	originalInterval := tm.checkInterval
	newInterval := 5 * time.Minute

	tm.SetCheckInterval(newInterval)

	if tm.checkInterval != newInterval {
		t.Errorf("Expected check interval %v but got %v", newInterval, tm.checkInterval)
	}

	if tm.checkInterval == originalInterval {
		t.Error("Expected check interval to be changed")
	}
}

// TestMonitorContractTransactions tests monitoring contract transactions
func TestMonitorContractTransactions(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	contractID := "contract123"
	txIDs := []string{"tx1", "tx2", "tx3"}

	err := tm.MonitorContractTransactions(context.Background(), contractID, txIDs)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check that all transactions were added
	if len(tm.monitoredTxs) != 3 {
		t.Errorf("Expected 3 monitored transactions but got %d", len(tm.monitoredTxs))
	}

	for _, txID := range txIDs {
		tx, exists := tm.monitoredTxs[txID]
		if !exists {
			t.Errorf("Expected transaction %s to be monitored", txID)
		}

		if tx.ContractID != contractID {
			t.Errorf("Expected contract ID '%s' but got '%s'", contractID, tx.ContractID)
		}

		if tx.Type != "unknown" {
			t.Errorf("Expected type 'unknown' but got '%s'", tx.Type)
		}

		if tx.RequiredConfs != 6 {
			t.Errorf("Expected required confirmations 6 but got %d", tx.RequiredConfs)
		}
	}
}

// TestTransactionMonitorEdgeCases tests edge cases and error conditions
func TestTransactionMonitorEdgeCases(t *testing.T) {
	tm := NewTransactionMonitor("http://localhost:8332")

	t.Run("Start service with context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Service should start and then stop when context is cancelled
		err := tm.Start(ctx)

		if err != context.DeadlineExceeded && err != context.Canceled {
			t.Errorf("Expected context cancellation error but got: %v", err)
		}
	})

	t.Run("Event handler with nil context", func(t *testing.T) {
		event := &TransactionEvent{
			Type:       "test_event",
			TxID:       "test123",
			ContractID: "contract123",
			Timestamp:  time.Now(),
		}

		// This should not panic
		tm.emitEvent(nil, event)
	})

	t.Run("Event handler with nil event", func(t *testing.T) {
		// Note: Current implementation panics with nil event
		// This test documents the current behavior
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for nil event")
			}
		}()

		tm.emitEvent(context.Background(), nil)

		// Should not reach here due to panic
		t.Errorf("Expected panic for nil event but got: %v", "no panic")
	})

	t.Run("Add transaction with nil metadata", func(t *testing.T) {
		tx := &MonitoredTransaction{
			TxID:          "test123",
			ContractID:    "contract123",
			Type:          "funding",
			Status:        "pending",
			RequiredConfs: 6,
			Metadata:      nil, // Nil metadata
		}

		err := tm.AddTransaction(tx)

		if err != nil {
			t.Errorf("Unexpected error with nil metadata: %v", err)
		}

		// Note: Current implementation doesn't initialize nil metadata
		// This test documents the current behavior
		storedTx := tm.monitoredTxs[tx.TxID]
		if storedTx.Metadata != nil {
			t.Error("Expected metadata to remain nil (current implementation behavior)")
		}
	})

	t.Run("Stop service", func(t *testing.T) {
		// This should not panic
		tm.Stop()
	})
}
