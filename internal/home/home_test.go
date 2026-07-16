package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodeScratchLifecycle(t *testing.T) {
	homeDir := t.TempDir()
	path, err := PrepareCodeScratch(homeDir, "sess_code_1")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(homeDir, "temp", ".code", "sess_code_1")
	want, err = filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("scratch path = %q, want %q", path, want)
	}
	if err := os.WriteFile(filepath.Join(path, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCodeScratch(homeDir, "sess_code_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("scratch still exists after removal: %v", err)
	}
}

func TestCodeScratchSanitizesUnexpectedSessionID(t *testing.T) {
	homeDir := t.TempDir()
	path := CodeScratchPath(homeDir, "../outside")
	root := filepath.Join(homeDir, "temp", ".code")
	if path == "" || filepath.Dir(path) != root {
		t.Fatalf("unsafe scratch path: %q", path)
	}
}

func TestCodeScratchRejectsSymlinkedManagedRoot(t *testing.T) {
	homeDir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, "temp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(homeDir, "temp", ".code")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	outsideSession := filepath.Join(outside, "sess_code_1")
	if err := os.MkdirAll(outsideSession, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outsideSession, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := PrepareCodeScratch(homeDir, "sess_code_1"); err == nil {
		t.Fatal("symlinked scratch root should be rejected")
	}
	if err := RemoveCodeScratch(homeDir, "sess_code_1"); err == nil {
		t.Fatal("cleanup through a symlinked scratch root should be rejected")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("outside file changed: %q %v", data, err)
	}
}
