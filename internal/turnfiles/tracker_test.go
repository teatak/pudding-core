package turnfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestTrackerCollectsTurnFileChanges(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "modify.txt", "before\n")
	writeTestFile(t, root, "delete.txt", "remove\n")
	writeTestFile(t, root, "old-name.txt", "same\n")
	writeTestFile(t, root, "node_modules/ignored.js", "before\n")

	tracker := New()
	if err := tracker.EnsureBaseline("turn_1", []string{root}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "modify.txt", "after\n")
	if err := os.Remove(filepath.Join(root, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "old-name.txt"), filepath.Join(root, "new-name.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "added.txt", "new\n")
	writeTestFile(t, root, "node_modules/ignored.js", "after\n")

	changes, err := tracker.Finish("turn_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 4 {
		t.Fatalf("changes = %+v", changes)
	}
	byPath := make(map[string]store.TurnFileChangeInput, len(changes))
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if change := byPath["modify.txt"]; change.Kind != store.FileChangeModified || change.OldContent != "before\n" || change.NewContent != "after\n" || change.Additions != 1 || change.Deletions != 1 {
		t.Fatalf("modified change = %+v", change)
	}
	if change := byPath["delete.txt"]; change.Kind != store.FileChangeDeleted || change.Deletions != 1 {
		t.Fatalf("deleted change = %+v", change)
	}
	if change := byPath["added.txt"]; change.Kind != store.FileChangeAdded || change.Additions != 1 {
		t.Fatalf("added change = %+v", change)
	}
	if change := byPath["new-name.txt"]; change.Kind != store.FileChangeRenamed || change.OriginalPath != "old-name.txt" {
		t.Fatalf("renamed change = %+v", change)
	}
}

func TestTrackerAddsNewRootsBeforeTheyAreUsed(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeTestFile(t, first, "first.txt", "one\n")
	writeTestFile(t, second, "second.txt", "two\n")

	tracker := New()
	if err := tracker.EnsureBaseline("turn_2", []string{first}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, first, "first.txt", "changed\n")
	if err := tracker.EnsureBaseline("turn_2", []string{first, second}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, second, "second.txt", "changed\n")

	changes, err := tracker.Finish("turn_2")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestTrackerPairsDuplicateRenamesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "same\n")
	writeTestFile(t, root, "b.txt", "same\n")

	tracker := New()
	if err := tracker.EnsureBaseline("turn_rename", []string{root}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "a.txt"), filepath.Join(root, "c.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "b.txt"), filepath.Join(root, "d.txt")); err != nil {
		t.Fatal(err)
	}

	changes, err := tracker.Finish("turn_rename")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].OriginalPath != "a.txt" || changes[0].Path != "c.txt" || changes[1].OriginalPath != "b.txt" || changes[1].Path != "d.txt" {
		t.Fatalf("rename changes = %+v", changes)
	}
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
