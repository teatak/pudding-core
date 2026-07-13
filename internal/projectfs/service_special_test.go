//go:build darwin || linux

package projectfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRenameRejectsSpecialFileBeforeMutation(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "pipe")
	if err := unix.Mkfifo(source, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Rename(root, "pipe", "renamed"); !errors.Is(err, ErrNotFile) {
		t.Fatalf("rename error = %v", err)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Fatalf("source moved despite rejected rename: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "renamed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected renamed path: %v", err)
	}
}
