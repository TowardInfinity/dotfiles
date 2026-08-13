//go:build !darwin && !linux

package providers

import "os/exec"

func configureProcessGroup(_ *exec.Cmd, _ bool) {}

func terminateProcess(cmd *exec.Cmd, _, _ bool) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
