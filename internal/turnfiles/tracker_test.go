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

	tracker := New()
	targets := []string{
		filepath.Join(root, "modify.txt"),
		filepath.Join(root, "delete.txt"),
		filepath.Join(root, "old-name.txt"),
		filepath.Join(root, "new-name.txt"),
		filepath.Join(root, "added.txt"),
	}
	if err := tracker.BeginCall("turn_1", "call_1", []string{root}, targets); err != nil {
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
	if err := tracker.EndCall("turn_1", "call_1"); err != nil {
		t.Fatal(err)
	}

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
	for _, change := range changes {
		if change.Origin != store.FileChangeOriginStructured {
			t.Fatalf("origin = %q, change = %+v", change.Origin, change)
		}
	}
}

func TestTrackerAccumulatesCallsAcrossRoots(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeTestFile(t, first, "first.txt", "one\n")
	writeTestFile(t, second, "second.txt", "two\n")

	tracker := New()
	firstPath := filepath.Join(first, "first.txt")
	if err := tracker.BeginCall("turn_2", "call_first", []string{first, second}, []string{firstPath}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, first, "first.txt", "changed\n")
	if err := tracker.EndCall("turn_2", "call_first"); err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(second, "second.txt")
	if err := tracker.BeginCall("turn_2", "call_second", []string{first, second}, []string{secondPath}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, second, "second.txt", "changed\n")
	if err := tracker.EndCall("turn_2", "call_second"); err != nil {
		t.Fatal(err)
	}

	changes, err := tracker.Finish("turn_2")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	for _, change := range changes {
		if change.Origin != store.FileChangeOriginStructured {
			t.Fatalf("origin = %q, change = %+v", change.Origin, change)
		}
	}
}

func TestTrackerPairsDuplicateRenamesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "same\n")
	writeTestFile(t, root, "b.txt", "same\n")

	tracker := New()
	targets := []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "b.txt"),
		filepath.Join(root, "c.txt"),
		filepath.Join(root, "d.txt"),
	}
	if err := tracker.BeginCall("turn_rename", "call_rename", []string{root}, targets); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "a.txt"), filepath.Join(root, "c.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "b.txt"), filepath.Join(root, "d.txt")); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("turn_rename", "call_rename"); err != nil {
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

func TestTrackerEmptyAndRootTargetsDoNotScanProject(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n")

	tracker := New()
	if err := tracker.BeginCall("turn_empty", "call_empty", []string{root}, nil); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "main.go", "package changed\n")
	if err := tracker.EndCall("turn_empty", "call_empty"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn_empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("empty targets scanned project: %+v", changes)
	}

	if err := tracker.BeginCall("turn_root", "call_root", []string{root}, []string{root}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "main.go", "package changed_again\n")
	if err := tracker.EndCall("turn_root", "call_root"); err != nil {
		t.Fatal(err)
	}
	changes, err = tracker.Finish("turn_root")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("project-root target scanned project: %+v", changes)
	}
}

func TestTrackerMarksBinaryAndLargeFilesWithoutContent(t *testing.T) {
	root := t.TempDir()
	binaryPath := filepath.Join(root, "image.bin")
	largePath := filepath.Join(root, "large.txt")
	if err := os.WriteFile(binaryPath, []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(largePath, make([]byte, maxSnapshotContentBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := New()
	targets := []string{binaryPath, largePath}
	if err := tracker.BeginCall("turn_non_text", "call_non_text", []string{root}, targets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte{0, 1, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	large := make([]byte, maxSnapshotContentBytes+2)
	large[len(large)-1] = 1
	if err := os.WriteFile(largePath, large, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("turn_non_text", "call_non_text"); err != nil {
		t.Fatal(err)
	}

	changes, err := tracker.Finish("turn_non_text")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	byPath := make(map[string]store.TurnFileChangeInput, len(changes))
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if change := byPath["image.bin"]; !change.Binary || change.OldContent != "" || change.NewContent != "" {
		t.Fatalf("binary change = %+v", change)
	}
	if change := byPath["large.txt"]; !change.TooLarge || change.OldContent != "" || change.NewContent != "" {
		t.Fatalf("large change = %+v", change)
	}
}

func TestTrackerStructuredCallExcludesUnownedExternalChanges(t *testing.T) {
	root := t.TempDir()
	ownedPath := filepath.Join(root, "owned.txt")
	externalPath := filepath.Join(root, "external.txt")
	writeTestFile(t, root, "owned.txt", "before\n")
	writeTestFile(t, root, "external.txt", "before\n")

	tracker := New()
	if err := tracker.BeginCall("turn_scoped", "call_scoped", []string{root}, []string{ownedPath}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "owned.txt", "tool change\n")
	writeTestFile(t, root, "external.txt", "outside change\n")
	if err := tracker.EndCall("turn_scoped", "call_scoped"); err != nil {
		t.Fatal(err)
	}

	changes, err := tracker.Finish("turn_scoped")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "owned.txt" || changes[0].Origin != store.FileChangeOriginStructured {
		t.Fatalf("changes = %+v", changes)
	}
	if content, err := os.ReadFile(externalPath); err != nil || string(content) != "outside change\n" {
		t.Fatalf("external file = %q, err = %v", content, err)
	}
}

func TestTrackerUsesCallBoundaryInsteadOfFinishWorkspaceState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTestFile(t, root, "main.go", "user before call\n")

	tracker := New()
	if err := tracker.BeginCall("turn_boundary", "call_boundary", []string{root}, []string{path}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "main.go", "structured result\n")
	if err := tracker.EndCall("turn_boundary", "call_boundary"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "main.go", "user after call\n")

	changes, err := tracker.Finish("turn_boundary")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].OldContent != "user before call\n" || changes[0].NewContent != "structured result\n" {
		t.Fatalf("call-boundary changes = %+v", changes)
	}
}

func TestTrackerExplicitDirectoryIncludesGeneratedResources(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	writeTestFile(t, root, "dist/app.js", "before\n")
	writeTestFile(t, root, "dist/assets/icon.svg", "<svg />\n")

	tracker := New()
	if err := tracker.BeginCall("turn_dist", "call_dist", []string{root}, []string{dist}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "dist/app.js", "after\n")
	writeTestFile(t, root, "dist/assets/new.svg", "<svg>new</svg>\n")
	if err := tracker.EndCall("turn_dist", "call_dist"); err != nil {
		t.Fatal(err)
	}

	changes, err := tracker.Finish("turn_dist")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Path != "dist/app.js" || changes[1].Path != "dist/assets/new.svg" {
		t.Fatalf("generated resource changes = %+v", changes)
	}
}

func TestTrackerDiscardPreventsLateCallStateLeak(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTestFile(t, root, "main.go", "package main\n")

	tracker := New()
	if err := tracker.BeginCall("turn_discard", "call_discard", []string{root}, []string{path}); err != nil {
		t.Fatal(err)
	}
	tracker.Discard("turn_discard")
	writeTestFile(t, root, "main.go", "package changed\n")
	if err := tracker.EndCall("turn_discard", "call_discard"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn_discard")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("discarded turn leaked changes: %+v", changes)
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
