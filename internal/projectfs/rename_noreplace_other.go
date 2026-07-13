//go:build !darwin && !linux && !windows

package projectfs

import (
	"errors"
	"os"
)

func renameNoReplace(source, target string) error {
	if _, err := os.Lstat(target); err == nil {
		return ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}
