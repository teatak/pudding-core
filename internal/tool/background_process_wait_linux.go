package tool

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/unix"
)

const backgroundProcessOwnsGroup = true

// Wait without reaping: the leader's PID reserves the process group identity
// until wait() has terminated its remaining members and calls exec.Cmd.Wait.
func waitForBackgroundProcessExit(cmd *exec.Cmd) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
