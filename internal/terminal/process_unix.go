//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package terminal

import (
	"errors"
	"os/exec"
	"syscall"
)

func terminateTerminalProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
