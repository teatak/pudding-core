//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package tool

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// prepareBackgroundProcessPTY consumes master and returns an independently
// owned, pollable descriptor. creack/pty's master may be blocking, notably on
// macOS; simply closing that File need not interrupt an in-progress Read.
func prepareBackgroundProcessPTY(master *os.File) (*os.File, error) {
	defer master.Close()
	fd, err := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	// NewFile sees O_NONBLOCK and registers this fd with Go's netpoller.
	// The old File owns a different fd and is closed before any reader starts.
	pollable := os.NewFile(uintptr(fd), master.Name())
	if err := pollable.SetReadDeadline(time.Time{}); err != nil {
		_ = pollable.Close()
		return nil, err
	}
	return pollable, nil
}
