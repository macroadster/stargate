package smart_contract

import (
	"sync"

	"stargate-backend/core/smart_contract"
)

var (
	eventSinksMu sync.Mutex
	eventSinks   []func(smart_contract.Event)
)

// RegisterEventSink adds a callback to receive smart contract events.
func RegisterEventSink(sink func(smart_contract.Event)) {
	if sink == nil {
		return
	}
	eventSinksMu.Lock()
	eventSinks = append(eventSinks, sink)
	eventSinksMu.Unlock()
}

// PublishEvent forwards an event to registered sinks.
func PublishEvent(evt smart_contract.Event) {
	eventSinksMu.Lock()
	sinks := append([]func(smart_contract.Event){}, eventSinks...)
	eventSinksMu.Unlock()
	for _, sink := range sinks {
		sink(evt)
	}
}
