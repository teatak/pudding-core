package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Force a real removal failure without adding fault-injection hooks to production.
func blockDirectoryRemoval(t *testing.T, path string) func() {
	t.Helper()
	if runtime.GOOS == "windows" {
		file, err := os.Open(path) // Windows cannot delete a directory with this open handle.
		if err != nil {
			t.Fatal(err)
		}
		release := func() { _ = file.Close() }
		t.Cleanup(release)
		return release
	}
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	release := func() {
		if err := os.Chmod(parent, info.Mode().Perm()); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(release)
	return release
}
