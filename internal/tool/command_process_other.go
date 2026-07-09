//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package tool

import "os/exec"

func configureCommandProcess(_ *exec.Cmd) {}

func terminateCommandProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
