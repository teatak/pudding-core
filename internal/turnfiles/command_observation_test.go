package turnfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestTrackerCommandReconcilesOnlyExistingTrackedFiles(t *testing.T) {
	for _, action := range []string{"delete", "restore", "modify", "replace-with-directory"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "tracked.txt")
			writeTestFile(t, root, "tracked.txt", "original\n")
			writeTestFile(t, root, "unrelated.txt", "user original\n")
			tracker := New()
			if err := tracker.BeginCall("turn", "edit", []string{root}, []string{path}); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, root, "tracked.txt", "edited\n")
			if err := tracker.EndCall("turn", "edit"); err != nil {
				t.Fatal(err)
			}
			if err := tracker.BeginCallWithOrigin("turn", "command", []string{root}, nil, store.FileChangeOriginCommandObserved); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, root, "unrelated.txt", "unrelated command result\n")
			switch action {
			case "delete", "replace-with-directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if action == "replace-with-directory" {
					writeTestFile(t, root, "tracked.txt/never-scan.txt", "unrelated child\n")
				}
			case "restore":
				writeTestFile(t, root, "tracked.txt", "original\n")
			case "modify":
				writeTestFile(t, root, "tracked.txt", "command final\n")
			}
			if err := tracker.EndCall("turn", "command"); err != nil {
				t.Fatal(err)
			}
			changes, err := tracker.Finish("turn")
			if err != nil {
				t.Fatal(err)
			}
			if action == "restore" {
				if len(changes) != 0 {
					t.Fatalf("restored file has a net change: %+v", changes)
				}
				return
			}
			if len(changes) != 1 || changes[0].Path != "tracked.txt" || changes[0].OldContent != "original\n" || changes[0].Origin != store.FileChangeOriginCommandObserved {
				t.Fatalf("unexpected changes: %+v", changes)
			}
			if action == "modify" {
				if changes[0].Kind != store.FileChangeModified || changes[0].NewContent != "command final\n" {
					t.Fatalf("stale modified file: %+v", changes)
				}
			} else if changes[0].Kind != store.FileChangeDeleted {
				t.Fatalf("preexisting file deletion was lost: %+v", changes)
			}
		})
	}
}
