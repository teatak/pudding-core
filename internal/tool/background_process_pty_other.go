//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package tool

import (
	"errors"
	"os"
)

func prepareBackgroundProcessPTY(master *os.File) (*os.File, error) {
	_ = master.Close()
	return nil, errors.New("interactive command sessions are unavailable on this platform")
}
