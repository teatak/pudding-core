//go:build windows

package projectfs

import (
	"errors"

	"golang.org/x/sys/windows"
)

func renameNoReplace(source, target string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	err = windows.MoveFile(sourcePtr, targetPtr)
	if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return ErrConflict
	}
	return err
}
