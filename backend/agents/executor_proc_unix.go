//go:build unix

package agents

import (
	"os/exec"
	"syscall"
)

// configureIsolatedProcess puts the executor child in its own process group so
// signals sent to the group (e.g. on timeout or tool cleanup) do not reach stargate.
func configureIsolatedProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killIsolatedProcess terminates the executor and any child processes it spawned.
func killIsolatedProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Negative PID targets the child's process group (Setpgid makes child the leader).
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}