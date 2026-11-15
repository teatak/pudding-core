package tool

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/teatak/pudding-core/internal/projectfs"
)

var errFileWriteConflict = errors.New("file changed before writing")

func regularFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", projectfs.ErrNotFile, path)
	}
	return info, nil
}

// Callers authorize the target and serialize version checks. Staging in its
// parent keeps partial writes invisible and allows a same-filesystem rename.
func writeFileAtomically(target string, content io.Reader, expectedHash string, createOnly bool) error {
	mode := os.FileMode(0o600)
	if info, err := regularFileInfo(target); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".pudding-write-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}()
	if _, err := io.Copy(temp, content); err != nil {
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// A long write may overlap an editor save after the initial version check.
	if expectedHash != "" {
		current, err := hashFileContent(target)
		if errors.Is(err, os.ErrNotExist) {
			return errFileWriteConflict
		}
		if err != nil {
			return err
		}
		if current != expectedHash {
			return errFileWriteConflict
		}
	}
	if createOnly {
		if err := projectfs.RenameNoReplace(temp.Name(), target); err != nil {
			if errors.Is(err, projectfs.ErrConflict) {
				return errFileWriteConflict
			}
			return err
		}
		return nil
	}
	return os.Rename(temp.Name(), target)
}
