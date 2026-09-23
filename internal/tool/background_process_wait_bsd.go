//go:build darwin || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/unix"
)

const backgroundProcessOwnsGroup = true

// NOTE_EXIT observes completion without reaping our child. Unlike a later
// PID lookup, the unreaped child keeps the process group identity reserved.
func waitForBackgroundProcessExit(cmd *exec.Cmd) error {
	kq, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(kq)
	unix.CloseOnExec(kq)
	change := unix.Kevent_t{Fflags: unix.NOTE_EXIT}
	unix.SetKevent(&change, cmd.Process.Pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	for {
		_, err = unix.Kevent(kq, []unix.Kevent_t{change}, nil, nil)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	// macOS can no longer register an exiting/zombie child. It has not
	// been reaped here, so its PID still reserves our group identity.
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	events := make([]unix.Kevent_t, 1)
	for {
		// Retry interrupted waits without re-registering the one-shot event.
		n, err := unix.Kevent(kq, nil, events, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 {
			if events[0].Flags&unix.EV_ERROR != 0 {
				if events[0].Data == int64(unix.ESRCH) {
					return nil
				}
				return unix.Errno(events[0].Data)
			}
			return nil
		}
	}
}
