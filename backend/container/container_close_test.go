package container

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"stargate-backend/storage"
)

// countGoroutines reports how many live goroutines have needle in their stack.
//
// Total goroutine counts are useless here: other tests in this package build
// containers and stores of their own and, before this change, leaked a cleanup
// goroutine each, so a baseline taken at the top of a test is not stable. Naming
// the two functions under test measures only them.
func countGoroutines(needle string) int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, needle) {
			n++
		}
	}
	return n
}

// waitForGoroutines polls until needle appears want times, or gives up.
// The goroutines exit asynchronously once their done channel closes, so an
// immediate read after Close would race the scheduler.
func waitForGoroutines(t *testing.T, needle string, want int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	got := countGoroutines(needle)
	for got != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		got = countGoroutines(needle)
	}
	return got
}

const (
	cacheCleanup = "ContractCache).startCleanup"
	peerCleanup  = "PeerService).cleanup"
)

// Close stops the two goroutines its comment names.
//
// The assertion is that the goroutines are gone, not that Stop was called. A
// test that only checked Stop had run would pass against a Stop that closed
// nothing, which is most of what was wrong here: the methods existed and were
// never reached.
func TestCloseStopsTheGoroutinesItDocuments(t *testing.T) {
	t.Setenv("STARGATE_STORAGE", "memory")
	t.Setenv("BLOCKS_DIR", t.TempDir())
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())

	cacheBefore := countGoroutines(cacheCleanup)
	peerBefore := countGoroutines(peerCleanup)

	cfg := storage.LoadStorageConfigFromEnv()
	cfg.MCPDBPath = t.TempDir() + "/mcp.db"
	cfg.APIKeysDBPath = t.TempDir() + "/keys.db"
	cfg.IngestionsDBPath = t.TempDir() + "/ingestions.db"
	stores, err := storage.NewAllStores(cfg)
	if err != nil {
		t.Fatalf("NewAllStores: %v", err)
	}
	c := NewContainer(stores)

	// Positive control. Without this the test could pass on a build where the
	// goroutines were never started, and would then say nothing about Close.
	if got := waitForGoroutines(t, cacheCleanup, cacheBefore+1); got != cacheBefore+1 {
		t.Fatalf("cache cleanup goroutines after construction = %d, want %d; nothing to stop, rest of test is vacuous", got, cacheBefore+1)
	}
	if got := waitForGoroutines(t, peerCleanup, peerBefore+1); got != peerBefore+1 {
		t.Fatalf("peer cleanup goroutines after construction = %d, want %d; nothing to stop, rest of test is vacuous", got, peerBefore+1)
	}

	c.Close()

	if got := waitForGoroutines(t, cacheCleanup, cacheBefore); got != cacheBefore {
		t.Errorf("cache cleanup goroutines after Close = %d, want %d (still running)", got, cacheBefore)
	}
	if got := waitForGoroutines(t, peerCleanup, peerBefore); got != peerBefore {
		t.Errorf("peer cleanup goroutines after Close = %d, want %d (still running)", got, peerBefore)
	}
}

// "Safe to call multiple times" is what the comment has always promised. It was
// not true concurrently for either Stop: both were a select on done with a
// default that closed it, so two callers could both reach the close and the
// second panicked. Run with -race.
func TestCloseIsSafeConcurrentlyAndRepeatedly(t *testing.T) {
	t.Setenv("STARGATE_STORAGE", "memory")
	t.Setenv("BLOCKS_DIR", t.TempDir())
	t.Setenv("STARGATE_DATA_DIR", t.TempDir())

	for i := 0; i < 50; i++ {
		cfg := storage.LoadStorageConfigFromEnv()
		cfg.MCPDBPath = t.TempDir() + "/mcp.db"
		cfg.APIKeysDBPath = t.TempDir() + "/keys.db"
		cfg.IngestionsDBPath = t.TempDir() + "/ingestions.db"
		stores, err := storage.NewAllStores(cfg)
		if err != nil {
			t.Fatalf("NewAllStores: %v", err)
		}
		c := NewContainer(stores)

		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c.Close()
			}()
		}
		wg.Wait()
		c.Close()
	}
}
