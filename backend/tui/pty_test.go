//go:build linux || darwin

package tui

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunOnPTYQuits(t *testing.T) {
	master, slave, err := openTestPTY()
	if err != nil {
		t.Skipf("pty: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	_ = unix.SetNonblock(int(master.Fd()), true)

	hub := NewHub()
	hub.SetVersion("stargate test")
	hub.SetPhase(PhaseReady, "listening on :3001")
	hub.ApplyStatus(Snapshot{
		UIURL:        "http://127.0.0.1:3001",
		DocsURL:      "http://127.0.0.1:3001/docs",
		HealthStatus: "healthy",
		Network:      "testnet4",
		ChainMode:    "managed",
		HTTPPort:     "3001",
		Reachable:    true,
	})

	// Drain the master for the whole run. If the test stops reading, the pty
	// buffer fills and the console blocks in its next paint, never seeing q.
	var (
		mu  sync.Mutex
		got bytes.Buffer
	)
	stopDrain := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				got.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				select {
				case <-stopDrain:
					return
				default:
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()
	defer close(stopDrain)

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), hub, slave, slave, Options{Color: true})
	}()

	deadline := time.Now().Add(2 * time.Second)
	var snap []byte
	for time.Now().Before(deadline) {
		mu.Lock()
		snap = append([]byte(nil), got.Bytes()...)
		mu.Unlock()
		if containsVisible(snap, "http://127.0.0.1:3001") && containsVisible(snap, "Open this in your browser") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !containsVisible(snap, "http://127.0.0.1:3001") || !containsVisible(snap, "Open this in your browser") {
		t.Fatalf("pty frame missing welcome (%d bytes)", len(snap))
	}

	if _, err := master.Write([]byte{'q'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("console did not exit after q")
	}
}

func containsVisible(frame []byte, needle string) bool {
	out := make([]byte, 0, len(frame))
	for i := 0; i < len(frame); i++ {
		if frame[i] == 0x1b && i+1 < len(frame) && frame[i+1] == '[' {
			i += 2
			for i < len(frame) && !((frame[i] >= 'A' && frame[i] <= 'Z') || (frame[i] >= 'a' && frame[i] <= 'z')) {
				i++
			}
			continue
		}
		out = append(out, frame[i])
	}
	return strings.Contains(string(out), needle)
}
