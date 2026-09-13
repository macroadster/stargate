package agents

import (
	"context"
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
