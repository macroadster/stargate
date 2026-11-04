package tui

import (
	"bytes"
	"sync"
	"time"
)

const logCap = 200

// Hub is the in-process status sample plus a log ring.
// The standard logger can write to it. Snapshot is safe to call while logs arrive.
type Hub struct {
	mu        sync.Mutex
	snap      Snapshot
	logs      []string
	partial   bytes.Buffer
	format    func(string) string
	startedAt time.Time
}

// NewHub returns an empty hub whose uptime starts now.
func NewHub() *Hub {
	return &Hub{startedAt: time.Now()}
}

// SetVersion records the process version string.
func (h *Hub) SetVersion(v string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.snap.Version = v
	h.mu.Unlock()
}

// SetPhase updates the lifecycle line. The first call also stamps StartedAt
// when the hub was not given one.
func (h *Hub) SetPhase(p Phase, detail string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap.Phase = p
	h.snap.Detail = detail
	if h.snap.StartedAt.IsZero() {
		if !h.startedAt.IsZero() {
			h.snap.StartedAt = h.startedAt
		} else {
			h.snap.StartedAt = time.Now()
		}
	}
}

// Phase returns the current lifecycle phase.
func (h *Hub) Phase() Phase {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap.Phase
}

// SetLineFormatter formats or drops complete log lines.
// An empty result drops the line. The function must not call back into the hub.
func (h *Hub) SetLineFormatter(fn func(string) string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.format = fn
	h.mu.Unlock()
}

// ApplyStatus replaces status fields and keeps the log ring.
// An empty phase, detail (while starting or stopping), version, or start time
// keeps the previous value so a collector cannot wipe a startup line.
func (h *Hub) ApplyStatus(s Snapshot) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.snap
	if s.Phase == "" {
		s.Phase = prev.Phase
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = prev.StartedAt
	}
	if s.Version == "" {
		s.Version = prev.Version
	}
	if s.Phase == PhaseStarting || s.Phase == PhaseStopping {
		s.Detail = prev.Detail
	}
	h.snap = s
}

// Snapshot copies the latest sample and the log ring.
func (h *Hub) Snapshot() Snapshot {
	if h == nil {
		return Snapshot{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.snap
	if len(h.logs) > 0 {
		s.Logs = append([]string(nil), h.logs...)
	}
	if h.partial.Len() > 0 {
		s.Logs = append(s.Logs, h.partial.String())
	}
	s.SampledAt = time.Now()
	return s
}

// Write implements io.Writer for the process logger.
func (h *Hub) Write(p []byte) (int, error) {
	if h == nil {
		return len(p), nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.partial.Write(p)
	for {
		b := h.partial.Bytes()
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break
		}
		line := string(b[:i])
		h.partial.Next(i + 1)
		h.appendLocked(line)
	}
	if h.partial.Len() > 8192 {
		h.appendLocked(h.partial.String())
		h.partial.Reset()
	}
	return len(p), nil
}

func (h *Hub) appendLocked(line string) {
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if h.format != nil {
		line = h.format(line)
	}
	if line == "" {
		return
	}
	h.logs = append(h.logs, line)
	if len(h.logs) > logCap {
		h.logs = h.logs[len(h.logs)-logCap:]
	}
}
