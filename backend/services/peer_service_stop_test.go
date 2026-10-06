package services

import (
	"sync"
	"testing"
)

// Same check-then-act race as ContractCache.Stop, same panic (stargate-ard).
// Run with -race.
func TestPeerServiceStopIsIdempotentUnderConcurrentCallers(t *testing.T) {
	for i := 0; i < 200; i++ {
		ps := NewPeerService()
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ps.Stop()
			}()
		}
		wg.Wait()
	}
}

func TestPeerServiceStopIsIdempotentSerially(t *testing.T) {
	ps := NewPeerService()
	ps.Stop()
	ps.Stop()
	ps.Stop()
}

// Guards against a sync.Once wrapped around a body that closes nothing.
func TestPeerServiceStopEndsTheCleanupGoroutine(t *testing.T) {
	ps := NewPeerService()
	ps.Stop()

	select {
	case <-ps.done:
	default:
		t.Fatal("done is still open after Stop; the cleanup goroutine has no reason to exit")
	}
}
