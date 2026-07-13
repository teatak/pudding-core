package projectfs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestProjectFileLifecycle(t *testing.T) {
	root := t.TempDir()
	dir, err := Create(root, ".", "docs", "dir")
	if err != nil || dir.Path != "docs" || dir.Type != "dir" {
		t.Fatalf("create dir = %+v, %v", dir, err)
	}
	file, err := Create(root, "docs", "guide.md", "file")
	if err != nil || file.Path != "docs/guide.md" {
		t.Fatalf("create file = %+v, %v", file, err)
	}
	emptyRevision := Revision(nil)
	saved, err := Save(root, file.Path, []byte("# Guide\n"), emptyRevision)
	if err != nil || saved.Revision == emptyRevision || saved.Size != 8 {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	if _, err := Save(root, file.Path, []byte("stale"), emptyRevision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale save error = %v", err)
	}
	renamed, err := Rename(root, file.Path, "intro.md")
	if err != nil || renamed.Path != "docs/intro.md" {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	if err := Remove(root, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed directory stat error = %v", err)
	}
}

func TestProjectFileMutationsRejectUnsafePaths(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, ".", "../escape", "file"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid name error = %v", err)
	}
	if _, err := Create(root, "..", "escape", "file"); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("parent traversal error = %v", err)
	}
	if err := Remove(root, "."); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("root remove error = %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "outside", "escape", "file"); !errors.Is(err, ErrPathNotAllowed) && !errors.Is(err, ErrSymlink) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestConcurrentSavesAllowOnlyOneWriterPerRevision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared.txt")
	base := []byte("base")
	if err := os.WriteFile(path, base, 0o644); err != nil {
		t.Fatal(err)
	}

	const writers = 24
	start := make(chan struct{})
	results := make(chan error, writers)
	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, err := Save(root, "shared.txt", []byte{byte(index)}, Revision(base))
			results <- err
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)

	succeeded := 0
	conflicted := 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRevisionConflict):
			conflicted++
		default:
			t.Fatalf("unexpected save error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != writers-1 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}

func TestRenameDoesNotReplaceExistingTarget(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(source, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(source, target); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename error = %v", err)
	}
	for path, expected := range map[string]string{source: "source", target: "target"} {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != expected {
			t.Fatalf("%s = %q, %v", path, content, err)
		}
	}
}
