//go:build darwin

package projectfs

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameNoReplace(source, target string) error {
	err := unix.RenamexNp(source, target, unix.RENAME_EXCL)
	if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
		return ErrConflict
	}
	return err
}
