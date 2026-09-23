package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionArtifactsLifecycleAndIsolation(t *testing.T) {
	dir := t.TempDir()
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"session-a", "session-b", "../escape"} {
		root, path, err := OpenSessionArtifacts(dir, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile("keep.txt", []byte(id), 0o600); err != nil {
			t.Fatal(err)
		}
		root.Close()
		if filepath.Dir(path) != filepath.Join(canonicalDir, "temp", "session-artifacts") {
			t.Fatalf("artifact area escaped its root: %s", path)
		}
		if got, exists, err := ExistingSessionArtifacts(dir, id); err != nil || !exists || got != path {
			t.Fatalf("artifact area not reusable: %q %v %v", got, exists, err)
		}
	}
	if err := RemoveSessionArtifacts(dir, "session-a"); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := ExistingSessionArtifacts(dir, "session-a"); err != nil || exists {
		t.Fatalf("deleted session area still exists: %v %v", exists, err)
	}
	if path, exists, err := ExistingSessionArtifacts(dir, "session-b"); err != nil || !exists {
		t.Fatalf("other session area was removed: %s %v %v", path, exists, err)
	}
}

func TestSessionArtifactsRejectManagedSymlinks(t *testing.T) {
	for _, component := range []string{"temp", "temp/session-artifacts", "temp/session-artifacts/session-a"} {
		t.Run(component, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			path := filepath.Join(dir, filepath.FromSlash(component))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if root, _, err := OpenSessionArtifacts(dir, "session-a"); err == nil {
				root.Close()
				t.Fatal("symlinked artifact area accepted")
			}
			if _, _, err := ExistingSessionArtifacts(dir, "session-a"); err == nil {
				t.Fatal("symlinked artifact area authorized")
			}
			_ = RemoveSessionArtifacts(dir, "session-a")
			if data, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(data) != "keep" {
				t.Fatalf("cleanup touched outside file: %q %v", data, err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 {
				t.Fatalf("creation followed symlink: %+v %v", entries, err)
			}
		})
	}
}
