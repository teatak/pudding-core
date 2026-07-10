//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package terminal

import "os/exec"

func terminateTerminalProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
