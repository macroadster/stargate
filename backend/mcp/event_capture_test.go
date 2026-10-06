package mcp

import (
	"sync"
	"testing"
	"time"

	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/core/smart_contract"
)

// RegisterEventSink appends to a process-global slice and offers no way to
// unregister, so a test that registers its own sink leaks it into every test
// that runs after. One sink is registered for the whole package instead, and it
// fans out to whoever is currently listening.
var (
	eventTapOnce sync.Once
	eventTapMu   sync.Mutex
	eventTaps    []chan smart_contract.Event
)

// captureEvents returns a channel of events published while the test runs.
func captureEvents(t *testing.T) <-chan smart_contract.Event {
	t.Helper()

	eventTapOnce.Do(func() {
		scmiddleware.RegisterEventSink(func(evt smart_contract.Event) {
			eventTapMu.Lock()
			taps := append([]chan smart_contract.Event{}, eventTaps...)
			eventTapMu.Unlock()
			for _, tap := range taps {
				// A test that has stopped reading must not block the code
				// emitting the event.
				select {
				case tap <- evt:
				default:
				}
			}
		})
	})

	tap := make(chan smart_contract.Event, 32)
	eventTapMu.Lock()
	eventTaps = append(eventTaps, tap)
	eventTapMu.Unlock()

	t.Cleanup(func() {
		eventTapMu.Lock()
		defer eventTapMu.Unlock()
		for i, existing := range eventTaps {
			if existing == tap {
				eventTaps = append(eventTaps[:i], eventTaps[i+1:]...)
				return
			}
		}
	})

	return tap
}

// awaitEvent returns the first event of the given type naming entityID, and
// fails if none arrives.
func awaitEvent(t *testing.T, events <-chan smart_contract.Event, eventType, entityID string) smart_contract.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Type == eventType && evt.EntityID == entityID {
				return evt
			}
		case <-deadline:
			t.Fatalf("no %s event was recorded for %s", eventType, entityID)
		}
	}
}
