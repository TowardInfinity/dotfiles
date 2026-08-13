//go:build darwin || linux

package providers

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd, grouped bool) {
	if grouped {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

func terminateProcess(cmd *exec.Cmd, grouped, force bool) {
	if cmd.Process == nil {
		return
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	pid := cmd.Process.Pid
	if grouped {
		// A negative PID addresses the process group created above. Ignore ESRCH:
		// the command may have completed naturally between cancellation and signal.
		pid = -pid
	}
	_ = syscall.Kill(pid, signal)
}
