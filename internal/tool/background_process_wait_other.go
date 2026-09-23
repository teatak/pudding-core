//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package tool

import (
	"errors"
	"os/exec"
)

const backgroundProcessOwnsGroup = false

func waitForBackgroundProcessExit(*exec.Cmd) error {
	return errors.New("interactive command sessions are unavailable on this platform")
}
