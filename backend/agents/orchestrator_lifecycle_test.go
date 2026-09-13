package agents

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestOrchestratorStopWaitsAndAllowsRestart(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		PollInterval: time.Hour,
		MaxCycles:    100,
	}
	o := NewOrchestrator(cfg, nil, NewStubExecutor(t.TempDir()))

	o.Start(context.Background())
	if !o.IsRunning() {
		t.Fatal("started orchestrator reports stopped")
	}

	o.Stop()
	if o.IsRunning() {
		t.Fatal("stopped orchestrator reports running")
	}

	// Stop is idempotent, and cleanup resets the lifecycle for another start.
	o.Stop()
	o.Start(context.Background())
	if !o.IsRunning() {
		t.Fatal("restarted orchestrator reports stopped")
	}
	o.Stop()
}

func TestOrchestratorConcurrentStartStop(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		PollInterval: time.Hour,
		MaxCycles:    100,
	}
	o := NewOrchestrator(cfg, nil, NewStubExecutor(t.TempDir()))

	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for range 25 {
				o.Start(context.Background())
				o.Stop()
			}
		}()
	}
	callers.Wait()

	if o.IsRunning() {
		t.Fatal("orchestrator still running after concurrent stops")
	}
}
