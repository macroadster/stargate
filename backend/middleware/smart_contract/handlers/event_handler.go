package handlers

import (
	"net/http"
	"strings"

	"stargate-backend/core/smart_contract"
	smartstore "stargate-backend/middleware/smart_contract"
	"stargate-backend/middleware/smart_contract/middleware"
)

// EventHandler handles event-related HTTP endpoints
type EventHandler struct {
	store     smartstore.Store
	eventChan chan smart_contract.Event
}

// NewEventHandler creates a new event handler
func NewEventHandler(store smartstore.Store) *EventHandler {
	return &EventHandler{
		store:     store,
		eventChan: make(chan smart_contract.Event, 100),
	}
}

// Events handles GET /events
func (h *EventHandler) Events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// TODO: Implement event listing logic
	// This needs to be extracted from the original handleEvents function
	middleware.Error(w, http.StatusNotImplemented, "event listing not yet extracted")
}

// GetEventChannel returns the event channel for broadcasting
func (h *EventHandler) GetEventChannel() chan smart_contract.Event {
	return h.eventChan
}

// BroadcastEvent broadcasts an event to all listeners
func (h *EventHandler) BroadcastEvent(evt smart_contract.Event) {
	select {
	case h.eventChan <- evt:
	default:
		// Channel full, drop event
	}
}

// splitCSV splits comma-separated values
func (h *EventHandler) splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(value), ",")
}
