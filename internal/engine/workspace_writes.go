package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type workspaceWriteLease struct{ roots []string }

type workspaceWriteLocks struct {
	mu      sync.Mutex
	queue   []*workspaceWriteLease
	changed chan struct{}
}

// AcquireWorkspaceWrite serializes daemon-owned writes and their before/after
// snapshots across tools, file/Git APIs and turn replay. Overlapping roots share
// a lease regardless of session or symlink spelling. Unrelated roots can proceed.
// Callers still own authorization and must check file revisions after acquiring.
func (e *Engine) AcquireWorkspaceWrite(ctx context.Context, roots []string) (func(), error) {
	canonical := make([]string, 0, len(roots))
	for _, root := range roots {
		path, err := canonicalWriteRoot(root)
		if err != nil {
			return nil, err
		}
		canonical = append(canonical, path)
	}
	locks := &e.workspaceWrites
	lease := &workspaceWriteLease{roots: canonical}
	locks.mu.Lock()
	if locks.changed == nil {
		locks.changed = make(chan struct{})
	}
	locks.queue = append(locks.queue, lease)
	for {
		if err := ctx.Err(); err != nil {
			locks.remove(lease)
			locks.mu.Unlock()
			return nil, err
		}
		blocked := false
		for _, earlier := range locks.queue {
			if earlier == lease {
				break
			}
			if writeRootsOverlap(earlier.roots, canonical) {
				blocked = true
				break
			}
		}
		if !blocked {
			locks.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { locks.mu.Lock(); defer locks.mu.Unlock(); locks.remove(lease) }) }, nil
		}
		changed := locks.changed
		locks.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		locks.mu.Lock()
	}
}

func (locks *workspaceWriteLocks) remove(lease *workspaceWriteLease) {
	for i, item := range locks.queue {
		if item == lease {
			locks.queue = append(locks.queue[:i], locks.queue[i+1:]...)
			break
		}
	}
	close(locks.changed)
	locks.changed = make(chan struct{})
}

func writeRootsOverlap(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			if a == b || strings.HasPrefix(a, strings.TrimRight(b, string(filepath.Separator))+string(filepath.Separator)) || strings.HasPrefix(b, strings.TrimRight(a, string(filepath.Separator))+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// Resolve the existing prefix as well: deleted roots must retain their identity
// while the actual file operation reports the missing path using its own contract.
func canonicalWriteRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := abs
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			if runtime.GOOS == "windows" {
				resolved = strings.ToLower(resolved)
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(path) == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = filepath.Dir(path)
	}
}
