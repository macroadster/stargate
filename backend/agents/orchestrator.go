package agents

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"

	scmiddleware "stargate-backend/app/smart_contract"
	"stargate-backend/core/smart_contract"
)

// Orchestrator manages the built-in autonomous agent loop inside stargate.
// It coordinates Watcher (auditing + task discovery) and Worker (proposals + execution).
type Orchestrator struct {
	cfg      Config
	store    scmiddleware.Store
	executor Executor

	worker  *Worker
	watcher *Watcher // populated when watcher is enabled

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewOrchestrator constructs an orchestrator. If executor is nil, a StubExecutor is used.
func NewOrchestrator(cfg Config, store scmiddleware.Store, executor Executor) *Orchestrator {
	if executor == nil {
		executor = NewAutoDetectExecutor(cfg.UploadsDir)
	}
	o := &Orchestrator{
		cfg:      cfg,
		store:    store,
		executor: executor,
	}
	if cfg.WorkerEnabled {
		o.worker = NewWorker(cfg, store, o.executor)
	}
	if cfg.WatcherEnabled {
		o.watcher = NewWatcher(cfg, store)
	}
	return o
}

// Start launches the agent loop in a background goroutine (if enabled).
// It is safe to call multiple times; subsequent calls are no-ops while running.
func (o *Orchestrator) Start(ctx context.Context) {
	o.mu.Lock()
	if o.running || !o.cfg.Enabled {
		o.mu.Unlock()
		return
	}
	o.running = true
	ctx, cancel := context.WithCancel(ctx)
	o.cancel = cancel
	o.mu.Unlock()

	o.wg.Add(1)
	go o.run(ctx)
	log.Printf("agents: orchestrator started (watcher=%v worker=%v ai=%s poll=%s)",
		o.cfg.WatcherEnabled, o.cfg.WorkerEnabled, o.cfg.AIIdentifier, o.cfg.PollInterval)
}

// Stop requests graceful shutdown and waits for the loop to exit.

func (o *Orchestrator) run(ctx context.Context) {
	defer o.wg.Done()

	cycle := 0
	maxCycles := o.cfg.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 10000
	}

	log.Printf("agents: run loop starting (poll_interval=%s)", o.cfg.PollInterval)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cycle++
		cycleStart := time.Now()
		if cycle%10 == 0 || cycle == 1 {
			log.Printf("agents: cycle %d (goroutines=%d)", cycle, runtime.NumGoroutine())
		}

		workerDur := time.Duration(0)
		if o.worker != nil {
			wStart := time.Now()
			o.worker.ProcessWishes(ctx)
			workerDur = time.Since(wStart)
		}
		watcherDur := time.Duration(0)
		var tasks []smart_contract.Task
		if o.watcher != nil {
			wStart := time.Now()
			tasks = o.watcher.RunOnce(ctx)
			watcherDur = time.Since(wStart)
		}
		taskProcessDur := time.Duration(0)
		if len(tasks) > 0 && o.worker != nil {
			tpStart := time.Now()
			for _, t := range tasks {
				o.worker.ProcessTask(t)
			}
			taskProcessDur = time.Since(tpStart)
		}

		cycleDur := time.Since(cycleStart)
		if cycleDur > 5*time.Second || len(tasks) > 0 || cycle%20 == 0 {
			log.Printf("agents: cycle %d complete in %v (worker=%v watcher=%v tasks=%v processed=%d)",
				cycle, cycleDur, workerDur, watcherDur, taskProcessDur, len(tasks))
		}

		// Sleep with cancellation awareness
		select {
		case <-ctx.Done():
			return
		case <-time.After(o.cfg.PollInterval):
		}

		if cycle >= maxCycles {
			log.Printf("agents: reached max cycles, stopping")
			return
		}
	}
}

// IsRunning reports whether the loop is active.
