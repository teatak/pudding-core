//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureCommandProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func configureCommandPTY(cmd *exec.Cmd) {
	// creack/pty makes the child a session leader and controlling-terminal owner.
	// Setpgid cannot be combined with Setsid on macOS.
	cmd.SysProcAttr = nil
}

func terminateCommandProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func requestCommandProcessStop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
