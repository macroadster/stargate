//go:build unix

package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigureIsolatedProcess_Setpgid(t *testing.T) {
	cmd := exec.Command("true")
	configureIsolatedProcess(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("expected Setpgid=true on isolated executor command")
	}
}

func TestKillIsolatedProcess_DoesNotSignalParentGroup(t *testing.T) {
	// Simulates AI CLIs that broadcast SIGTERM to their process group on exit.
	script := filepath.Join(t.TempDir(), "signal_bomb.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'kill -TERM 0 2>/dev/null || true' EXIT\nsleep 60\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(script)
	configureIsolatedProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	killIsolatedProcess(cmd)
	_ = cmd.Wait()
	// Reaching here means the parent test process was not taken down by the trap.
}