package projectfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const MaxTextBytes = 2 << 20

var (
	ErrRevisionConflict = errors.New("project file revision conflict")
	ErrRevisionRequired = errors.New("project file revision required")
	ErrTooLarge         = errors.New("project file too large")
)

type SavedFile struct {
	ModTime  string
	Path     string
	Revision string
	Size     int64
}

type filePathLock struct {
	refs int
	mu   sync.Mutex
}

var filePathLocks = struct {
	sync.Mutex
	locks map[string]*filePathLock
}{locks: make(map[string]*filePathLock)}

func Revision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func Save(root, path string, content []byte, expectedRevision string) (SavedFile, error) {
	if len(content) > MaxTextBytes {
		return SavedFile{}, ErrTooLarge
	}
	if expectedRevision == "" {
		return SavedFile{}, ErrRevisionRequired
	}
	unlock := lockFilePath(filepath.Join(filepath.Clean(root), filepath.FromSlash(path)))
	defer unlock()
	target, rel, info, err := existingPath(root, path, false)
	if err != nil {
		return SavedFile{}, err
	}
	if !info.Mode().IsRegular() {
		return SavedFile{}, ErrNotFile
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return SavedFile{}, err
	}
	if Revision(current) != expectedRevision {
		return SavedFile{}, ErrRevisionConflict
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".pudding-edit-*")
	if err != nil {
		return SavedFile{}, err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return SavedFile{}, err
	}
	if _, err := temp.Write(content); err != nil {
		return SavedFile{}, err
	}
	if err := temp.Sync(); err != nil {
		return SavedFile{}, err
	}
	if err := temp.Close(); err != nil {
		return SavedFile{}, err
	}
	latest, err := os.ReadFile(target)
	if err != nil {
		return SavedFile{}, err
	}
	if Revision(latest) != expectedRevision {
		return SavedFile{}, ErrRevisionConflict
	}
	if err := os.Rename(tempPath, target); err != nil {
		return SavedFile{}, fmt.Errorf("replace project file: %w", err)
	}
	committed = true
	updated, err := os.Stat(target)
	if err != nil {
		return SavedFile{}, err
	}
	return SavedFile{
		ModTime:  updated.ModTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Path:     rel,
		Revision: Revision(content),
		Size:     updated.Size(),
	}, nil
}

func lockFilePath(path string) func() {
	key := filepath.Clean(path)
	filePathLocks.Lock()
	lock := filePathLocks.locks[key]
	if lock == nil {
		lock = &filePathLock{}
		filePathLocks.locks[key] = lock
	}
	lock.refs++
	filePathLocks.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		filePathLocks.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(filePathLocks.locks, key)
		}
		filePathLocks.Unlock()
	}
}
