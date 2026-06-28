//go:build windows

package agents

import "os/exec"

func configureIsolatedProcess(cmd *exec.Cmd) {}

func killIsolatedProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}