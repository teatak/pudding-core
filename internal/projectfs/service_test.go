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

func TestCopyAndMoveProjectEntries(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceRoot, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "docs", "nested", "guide.md"), []byte("guide"), 0o640); err != nil {
		t.Fatal(err)
	}

	copied, err := Copy(sourceRoot, "docs", sourceRoot, ".", "", true)
	if err != nil || copied.Path != "docs copy" || copied.Type != "dir" {
		t.Fatalf("copy = %+v, %v", copied, err)
	}
	content, err := os.ReadFile(filepath.Join(sourceRoot, "docs copy", "nested", "guide.md"))
	if err != nil || string(content) != "guide" {
		t.Fatalf("copied content = %q, %v", content, err)
	}

	moved, err := Move(sourceRoot, "docs/nested/guide.md", targetRoot, ".", "moved.md")
	if err != nil || moved.Path != "moved.md" || moved.Type != "file" {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, "docs", "nested", "guide.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains after move: %v", err)
	}
	content, err = os.ReadFile(filepath.Join(targetRoot, "moved.md"))
	if err != nil || string(content) != "guide" {
		t.Fatalf("moved content = %q, %v", content, err)
	}
}

func TestCrossRootMoveKeepsCompletedCopyWhenSourceRemovalFails(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()
	source := filepath.Join(sourceRoot, "guide.md")
	target := filepath.Join(targetRoot, "guide.md")
	if err := os.WriteFile(source, []byte("guide"), 0o640); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	removeErr := errors.New("source removal failed")
	err = moveAcrossRoots(source, target, info, func(path string) error {
		if path != source {
			t.Fatalf("remove source path = %q", path)
		}
		return removeErr
	})
	if !errors.Is(err, removeErr) {
		t.Fatalf("move error = %v", err)
	}
	for _, path := range []string{source, target} {
		content, readErr := os.ReadFile(path)
		if readErr != nil || string(content) != "guide" {
			t.Fatalf("%s = %q, %v", path, content, readErr)
		}
	}
}

func TestCopyAndMoveRejectUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(root, "docs", root, "docs/nested", "docs"); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("descendant move error = %v", err)
	}
	if _, err := Copy(root, "docs", root, "docs/nested", "copy", false); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("descendant copy error = %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(t.TempDir(), filepath.Join(root, "docs", "outside")); err != nil {
			t.Fatal(err)
		}
		if _, err := Copy(root, "docs", root, ".", "docs-copy", false); !errors.Is(err, ErrSymlink) {
			t.Fatalf("nested symlink copy error = %v", err)
		}
	}
}
