package bitcoin

import (
	"testing"
	"time"
)

func TestBlockMonitorStartStopLifecycle(t *testing.T) {
	t.Setenv("BLOCK_MONITOR_TRACK_TIP_ONLY", "true")
	bm := &BlockMonitor{
		checkInterval: time.Hour,
		blocksDir:     t.TempDir(),
	}

	if bm.IsRunning() {
		t.Fatal("new monitor reports running")
	}
	if err := bm.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !bm.IsRunning() {
		t.Fatal("started monitor reports stopped")
	}
	if err := bm.Start(); err == nil {
		t.Fatal("second start should fail while running")
	}

	bm.Stop()
	if bm.IsRunning() {
		t.Fatal("stopped monitor reports running")
	}

	// Stop is idempotent, and a fully stopped monitor can be restarted.
	bm.Stop()
	if err := bm.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	bm.Stop()
}

func TestBlockMonitorStopInterruptsReconcileStartupDelay(t *testing.T) {
	t.Setenv("BLOCK_MONITOR_TRACK_TIP_ONLY", "false")
	t.Setenv("BLOCK_MONITOR_RECONCILE_WINDOW", "1")
	bm := &BlockMonitor{
		checkInterval: time.Hour,
		blocksDir:     t.TempDir(),
	}

	if err := bm.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	started := time.Now()
	bm.Stop()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stop waited %s for reconcile startup delay", elapsed)
	}
}
