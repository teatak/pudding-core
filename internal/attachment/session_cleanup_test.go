package attachment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionCleanupDoesNotFollowSymlinks(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "attachments", "sessions")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "orphan")); err != nil {
		t.Fatal(err)
	}
	svc := NewService(dir)
	if err := svc.DeleteSession("orphan"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"..", "../outside", "/tmp", "draft", ""} {
		if err := svc.DeleteSession(id); err == nil {
			t.Fatalf("invalid session accepted: %q", id)
		}
	}
	if err := os.Remove(base); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, base); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSession("keep.txt"); err == nil {
		t.Fatal("symlinked managed root accepted")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
}
