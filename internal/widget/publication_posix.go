//go:build !windows

package widget

import "os"

func renamePublication(source, destination string) error { return os.Rename(source, destination) }

func syncPublication(parent string) error {
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
