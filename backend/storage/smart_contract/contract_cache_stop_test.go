package smart_contract

import (
	"sync"
	"testing"
	"time"
)

// Stop was a select on done with a default that closed it: check-then-act with
// no lock. Concurrent callers could both see done open, both take the default,
// and the second close panicked with "close of closed channel" (stargate-ard).
//
// Nothing outside tests called Stop, so this never fired in production. It would
// have on the first shutdown path that called it from two places, which is what
// Container.Close now makes possible.
func TestStopIsIdempotentUnderConcurrentCallers(t *testing.T) {
	for i := 0; i < 200; i++ {
		cache := NewContractCache(time.Minute, 10)
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cache.Stop()
			}()
		}
		wg.Wait()
	}
}

// Serial repetition worked before and must keep working.
func TestStopIsIdempotentSerially(t *testing.T) {
	cache := NewContractCache(time.Minute, 10)
	cache.Stop()
	cache.Stop()
	cache.Stop()
}

// Stop has to actually stop the cleaner, not merely avoid panicking. A sync.Once
// wrapped around a body that closed nothing would satisfy the tests above.
func TestStopEndsTheCleanupGoroutine(t *testing.T) {
	cache := NewContractCache(time.Minute, 10)
	cache.Stop()

	select {
	case <-cache.done:
	default:
		t.Fatal("done is still open after Stop; the cleanup goroutine has no reason to exit")
	}
}
