//go:build !darwin

package tool

import "os/exec"

func terminateBackgroundProcessGroup(cmd *exec.Cmd, _ bool) error {
	return terminateCommandProcess(cmd)
}
