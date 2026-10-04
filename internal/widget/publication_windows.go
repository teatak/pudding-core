package widget

import "golang.org/x/sys/windows"

func renamePublication(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// File contents were flushed before close. Windows does not support fsync
	// on an os.Open directory handle; use a write-through move to publish it.
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func syncPublication(string) error { return nil } // renamePublication is write-through.
