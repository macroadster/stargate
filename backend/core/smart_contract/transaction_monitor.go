package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// TransactionMonitor monitors Bitcoin transactions for smart contract events
type TransactionMonitor struct {
	httpClient    *http.Client
	monitoredTxs  map[string]*MonitoredTransaction
	eventHandlers map[string][]EventHandler
	checkInterval time.Duration
	bitcoinRPC    string
}

// NewTransactionMonitor creates a new transaction monitor
func NewTransactionMonitor(bitcoinRPC string) *TransactionMonitor {
	return &TransactionMonitor{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		monitoredTxs:  make(map[string]*MonitoredTransaction),
		eventHandlers: make(map[string][]EventHandler),
		checkInterval: 2 * time.Minute, // Check every 2 minutes
		bitcoinRPC:    bitcoinRPC,
	}
}

// MonitoredTransaction represents a transaction being monitored
type MonitoredTransaction struct {
	TxID          string         `json:"tx_id"`
	ContractID    string         `json:"contract_id"`
	Type          string         `json:"type"`   // funding | claim | payout | refund
	Status        string         `json:"status"` // pending | confirmed | failed
	RequiredConfs int            `json:"required_confirmations"`
	CurrentConfs  int            `json:"current_confirmations"`
	AmountSats    int64          `json:"amount_sats"`
	FromAddress   string         `json:"from_address"`
	ToAddress     string         `json:"to_address"`
	ScriptHex     string         `json:"script_hex"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"created_at"`
	ConfirmedAt   *time.Time     `json:"confirmed_at,omitempty"`
	LastChecked   time.Time      `json:"last_checked"`
	NextCheck     time.Time      `json:"next_check"`
}

// EventHandler represents a function that handles transaction events
type EventHandler func(ctx context.Context, event *TransactionEvent) error

// TransactionEvent represents a transaction-related event
type TransactionEvent struct {
	Type          string         `json:"type"` // tx_seen | tx_confirmed | tx_failed | tx_reorg
	TxID          string         `json:"tx_id"`
	ContractID    string         `json:"contract_id"`
	BlockHeight   int64          `json:"block_height"`
	Confirmations int            `json:"confirmations"`
	Data          map[string]any `json:"data"`
	Timestamp     time.Time      `json:"timestamp"`
}

// Start begins the transaction monitoring service
func (tm *TransactionMonitor) Start(ctx context.Context) error {
	log.Printf("Starting transaction monitor with %s check interval", tm.checkInterval)

	// Start periodic checking
	ticker := time.NewTicker(tm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Transaction monitor stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := tm.checkTransactions(ctx); err != nil {
				log.Printf("Transaction check failed: %v", err)
			}
		}
	}
}

// AddTransaction adds a transaction to be monitored
func (tm *TransactionMonitor) AddTransaction(tx *MonitoredTransaction) error {
	if tx.TxID == "" {
		return fmt.Errorf("transaction ID is required")
	}

	tx.CreatedAt = time.Now()
	tx.LastChecked = time.Now()
	tx.NextCheck = time.Now().Add(tm.checkInterval)

	tm.monitoredTxs[tx.TxID] = tx
	log.Printf("Added transaction %s to monitoring (type: %s, contract: %s)", tx.TxID, tx.Type, tx.ContractID)

	return nil
}

// AddEventHandler adds an event handler for a specific event type
func (tm *TransactionMonitor) AddEventHandler(eventType string, handler EventHandler) {
	if tm.eventHandlers[eventType] == nil {
		tm.eventHandlers[eventType] = make([]EventHandler, 0)
	}
	tm.eventHandlers[eventType] = append(tm.eventHandlers[eventType], handler)
	log.Printf("Added event handler for type: %s", eventType)
}

// checkTransactions checks the status of all monitored transactions
func (tm *TransactionMonitor) checkTransactions(ctx context.Context) error {
	log.Printf("Checking %d monitored transactions", len(tm.monitoredTxs))

	for txID, tx := range tm.monitoredTxs {
		// Skip if not time to check yet
		if time.Now().Before(tx.NextCheck) {
			continue
		}

		// Check transaction status
		updatedTx, err := tm.checkTransactionStatus(ctx, tx)
		if err != nil {
			log.Printf("Failed to check transaction %s: %v", txID, err)
			continue
		}

		// Update transaction
		tm.monitoredTxs[txID] = updatedTx

		// Emit events if status changed
		if tx.Status != updatedTx.Status {
			tm.emitStatusChangeEvent(ctx, tx, updatedTx)
		}

		// Emit confirmation events
		if updatedTx.Status == "confirmed" && updatedTx.CurrentConfs >= updatedTx.RequiredConfs {
			tm.emitFullyConfirmedEvent(ctx, updatedTx)
		}

		// Update next check time
		updatedTx.LastChecked = time.Now()
		if updatedTx.Status == "pending" {
			updatedTx.NextCheck = time.Now().Add(tm.checkInterval)
		} else {
			// Confirmed transactions need less frequent checking
			updatedTx.NextCheck = time.Now().Add(tm.checkInterval * 5)
		}

		tm.monitoredTxs[txID] = updatedTx
	}

	return nil
}

// checkTransactionStatus checks the current status of a transaction
func (tm *TransactionMonitor) checkTransactionStatus(_ context.Context, tx *MonitoredTransaction) (*MonitoredTransaction, error) {
	// Get transaction data from blockchain APIs
	txData, err := tm.getTransactionData(tx.TxID)
	if err != nil {
		// Transaction might not exist yet
		if strings.Contains(err.Error(), "not found") {
			return tx, nil // Keep current status
		}
		return nil, err
	}

	// Update transaction with current data
	updatedTx := *tx
	updatedTx.CurrentConfs = txData.Confirmations

	// Determine status based on confirmations
	if txData.Confirmations == 0 {
		updatedTx.Status = "pending"
	} else if txData.Confirmations >= tx.RequiredConfs {
		updatedTx.Status = "confirmed"
		if updatedTx.ConfirmedAt == nil {
			now := time.Now()
			updatedTx.ConfirmedAt = &now
		}
	} else {
		updatedTx.Status = "confirming"
	}

	// Update block height if available
	if txData.BlockHeight > 0 {
		updatedTx.Metadata["block_height"] = txData.BlockHeight
	}

	return &updatedTx, nil
}

// getTransactionData fetches transaction data from blockchain APIs
func (tm *TransactionMonitor) getTransactionData(txID string) (*TransactionData, error) {
	// Determine network
	network := os.Getenv("BITCOIN_NETWORK")
	if network == "" {
		network = "mainnet"
	}

	// Try multiple blockchain APIs
	var apis []string
	if network == "testnet" {
		apis = []string{
			"https://blockstream.info/testnet/api/tx/" + txID,
			"https://api.blockcypher.com/v1/btc/test3/txs/" + txID,
		}
	} else {
		apis = []string{
			"https://blockstream.info/api/tx/" + txID,
			"https://api.blockcypher.com/v1/btc/main/txs/" + txID,
		}
	}

	for _, apiURL := range apis {
		resp, err := tm.httpClient.Get(apiURL)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var txData TransactionData
			if err := json.NewDecoder(resp.Body).Decode(&txData); err != nil {
				continue
			}

			return &txData, nil
		} else if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("transaction not found")
		}
	}

	return nil, fmt.Errorf("failed to fetch transaction data from all APIs")
}

// emitStatusChangeEvent emits an event when transaction status changes
func (tm *TransactionMonitor) emitStatusChangeEvent(ctx context.Context, oldTx, newTx *MonitoredTransaction) {
	event := &TransactionEvent{
		Type:       "tx_status_changed",
		TxID:       newTx.TxID,
		ContractID: newTx.ContractID,
		Data: map[string]any{
			"old_status":             oldTx.Status,
			"new_status":             newTx.Status,
			"confirmations":          newTx.CurrentConfs,
			"required_confirmations": newTx.RequiredConfs,
		},
		Timestamp: time.Now(),
	}

	if newTx.CurrentConfs > 0 {
		event.BlockHeight = newTx.Metadata["block_height"].(int64)
		event.Confirmations = newTx.CurrentConfs
	}

	tm.emitEvent(ctx, event)
}

// emitFullyConfirmedEvent emits an event when transaction is fully confirmed
func (tm *TransactionMonitor) emitFullyConfirmedEvent(ctx context.Context, tx *MonitoredTransaction) {
	event := &TransactionEvent{
		Type:          "tx_fully_confirmed",
		TxID:          tx.TxID,
		ContractID:    tx.ContractID,
		BlockHeight:   tx.Metadata["block_height"].(int64),
		Confirmations: tx.CurrentConfs,
		Data: map[string]any{
			"amount_sats":  tx.AmountSats,
			"from_address": tx.FromAddress,
			"to_address":   tx.ToAddress,
			"confirmed_at": tx.ConfirmedAt,
		},
		Timestamp: time.Now(),
	}

	tm.emitEvent(ctx, event)
}

// emitEvent emits an event to all registered handlers
func (tm *TransactionMonitor) emitEvent(ctx context.Context, event *TransactionEvent) {
	log.Printf("Emitting event: %s for tx %s", event.Type, event.TxID)

	// Call all handlers for this event type
	if handlers, exists := tm.eventHandlers[event.Type]; exists {
		for _, handler := range handlers {
			if err := handler(ctx, event); err != nil {
				log.Printf("Event handler error for %s: %v", event.Type, err)
			}
		}
	}

	// Also call general handlers
	if handlers, exists := tm.eventHandlers["*"]; exists {
		for _, handler := range handlers {
			if err := handler(ctx, event); err != nil {
				log.Printf("General event handler error: %v", err)
			}
		}
	}
}

// MonitorContractTransactions monitors all transactions for a specific contract
func (tm *TransactionMonitor) MonitorContractTransactions(ctx context.Context, contractID string, txIDs []string) error {
	log.Printf("Monitoring %d transactions for contract %s", len(txIDs), contractID)

	for _, txID := range txIDs {
		tx := &MonitoredTransaction{
			TxID:          txID,
			ContractID:    contractID,
			Type:          "unknown", // Would be determined from context
			Status:        "pending",
			RequiredConfs: 6, // Default to 6 confirmations
			Metadata:      make(map[string]any),
		}

		if err := tm.AddTransaction(tx); err != nil {
			log.Printf("Failed to add transaction %s: %v", txID, err)
		}
	}

	return nil
}

// GetTransactionStatus returns the current status of a monitored transaction
func (tm *TransactionMonitor) GetTransactionStatus(txID string) (*MonitoredTransaction, error) {
	tx, exists := tm.monitoredTxs[txID]
	if !exists {
		return nil, fmt.Errorf("transaction %s is not being monitored", txID)
	}

	return tx, nil
}

// RemoveTransaction removes a transaction from monitoring
func (tm *TransactionMonitor) RemoveTransaction(txID string) error {
	if _, exists := tm.monitoredTxs[txID]; !exists {
		return fmt.Errorf("transaction %s is not being monitored", txID)
	}

	delete(tm.monitoredTxs, txID)
	log.Printf("Removed transaction %s from monitoring", txID)

	return nil
}

// GetMonitoredTransactions returns all currently monitored transactions
func (tm *TransactionMonitor) GetMonitoredTransactions() map[string]*MonitoredTransaction {
	// Return a copy to prevent external modification
	result := make(map[string]*MonitoredTransaction)
	for txID, tx := range tm.monitoredTxs {
		txCopy := *tx
		result[txID] = &txCopy
	}
	return result
}

// GetMonitoringStats returns statistics about the monitoring service
func (tm *TransactionMonitor) GetMonitoringStats() map[string]any {
	stats := map[string]any{
		"total_monitored": len(tm.monitoredTxs),
		"check_interval":  tm.checkInterval.String(),
		"service_status":  "running",
		"started_at":      time.Now().Format(time.RFC3339),
		"version":         "1.0.0",
	}

	// Count by status
	statusCounts := make(map[string]int)
	for _, tx := range tm.monitoredTxs {
		statusCounts[tx.Status]++
	}
	stats["status_counts"] = statusCounts

	// Count by type
	typeCounts := make(map[string]int)
	for _, tx := range tm.monitoredTxs {
		typeCounts[tx.Type]++
	}
	stats["type_counts"] = typeCounts

	return stats
}

// SetCheckInterval updates the check interval
func (tm *TransactionMonitor) SetCheckInterval(interval time.Duration) {
	tm.checkInterval = interval
	log.Printf("Transaction monitor check interval updated to %s", interval)
}

// ContractEventHandler creates an event handler for contract-specific events
func ContractEventHandler(tm *TransactionMonitor, _ any) EventHandler {
	return func(ctx context.Context, event *TransactionEvent) error {
		log.Printf("Contract event handler: %s for contract %s", event.Type, event.ContractID)

		// In a real implementation, this would:
		// 1. Update contract state in database
		// 2. Trigger appropriate contract actions
		// 3. Notify other services
		// 4. Update Merkle proofs if needed

		switch event.Type {
		case "tx_fully_confirmed":
			return tm.handleFullyConfirmedTransaction(ctx, event, nil)
		case "tx_status_changed":
			return tm.handleStatusChange(ctx, event, nil)
		default:
			log.Printf("Unhandled contract event type: %s", event.Type)
		}

		return nil
	}
}

// handleFullyConfirmedTransaction handles fully confirmed transactions
func (tm *TransactionMonitor) handleFullyConfirmedTransaction(_ context.Context, event *TransactionEvent, _ any) error {
	log.Printf("Handling fully confirmed transaction %s for contract %s", event.TxID, event.ContractID)

	// In a real implementation, this would:
	// 1. Update contract status based on transaction type
	// 2. Process payouts if this is a funding transaction
	// 3. Release escrow if conditions are met
	// 4. Update Merkle proofs

	return nil
}

// handleStatusChange handles transaction status changes
func (tm *TransactionMonitor) handleStatusChange(_ context.Context, event *TransactionEvent, _ any) error {
	log.Printf("Handling status change for transaction %s: %s -> %s",
		event.TxID,
		event.Data["old_status"],
		event.Data["new_status"])

	// In a real implementation, this would:
	// 1. Update transaction status in database
	// 2. Notify interested parties
	// 3. Trigger next steps in contract lifecycle

	return nil
}

// Stop gracefully stops the transaction monitor
func (tm *TransactionMonitor) Stop() {
	log.Printf("Stopping transaction monitor")
	// In a real implementation, this would clean up resources
}
