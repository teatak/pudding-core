package attachment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionIDsExcludesDraft(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	for _, id := range []string{DraftSessionID, "live"} {
		if _, err := svc.StoreReader(id, "file.txt", "text/plain", strings.NewReader(id)); err != nil {
			t.Fatal(err)
		}
	}
	legacyDraft := filepath.Join(dir, "attachments", "sessions", DraftSessionID, "blobs")
	if err := os.MkdirAll(legacyDraft, 0700); err != nil {
		t.Fatal(err)
	}
	ids, err := svc.SessionIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "live" {
		t.Fatalf("session IDs = %v, want [live]", ids)
	}
	if _, err := os.Stat(legacyDraft); err != nil {
		t.Fatalf("legacy draft attachments were removed: %v", err)
	}
}

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
