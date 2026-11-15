//go:build !windows

package turnfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestTrackerUnreadableBaselineCannotDeleteOriginalOnUndo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the file read permissions exercised by this fixture")
	}
	for _, scope := range []string{"structured", "command-exact", "directory"} {
		for _, action := range []string{"readable", "overwrite", "still-unreadable"} {
			t.Run(scope+"/"+action, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "files", "secret.txt")
				neighbor := filepath.Join(root, "files", "neighbor.txt")
				writeTestFile(t, root, "files/secret.txt", "original\n")
				writeTestFile(t, root, "files/neighbor.txt", "neighbor before\n")
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
					t.Fatalf("fixture must reject reads: %v", err)
				}
				tracker := New()
				targets := []string{path, neighbor}
				var exact []string
				origin := store.FileChangeOriginStructured
				switch scope {
				case "command-exact":
					targets, exact, origin = nil, targets, store.FileChangeOriginCommandObserved
				case "directory":
					targets = []string{filepath.Dir(path)}
				}
				if err := tracker.BeginCallWithOrigin("tracked", "change", []string{root}, targets, origin, exact...); err != nil {
					t.Fatal(err)
				}
				want := "original\n"
				if action == "readable" {
					if err := os.Chmod(path, 0o600); err != nil {
						t.Fatal(err)
					}
				} else {
					// Atomic copy/move replacement only needs permission on the
					// parent. It can replace a file whose contents were unreadable.
					want = "replaced\n"
					replacement := filepath.Join(root, "replacement")
					if err := os.WriteFile(replacement, []byte(want), 0o600); err != nil {
						t.Fatal(err)
					}
					if action == "still-unreadable" {
						if err := os.Chmod(replacement, 0); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Rename(replacement, path); err != nil {
						t.Fatal(err)
					}
				}
				writeTestFile(t, root, "files/neighbor.txt", "neighbor after\n")
				if err := tracker.EndCall("tracked", "change"); err != nil {
					t.Fatal(err)
				}
				changes, err := tracker.Finish("tracked")
				if err != nil || len(changes) != 2 {
					t.Fatalf("incomplete file change lost: %+v %v", changes, err)
				}
				change := changes[1]
				if change.Path != "files/secret.txt" || change.Kind != store.FileChangeModified || change.OldType != "file" || change.OldDigest != "" || change.OldMode != 0 || change.OldSize != int64(len("original\n")) || change.Additions != 0 || change.Deletions != 0 {
					t.Fatalf("unreadable baseline was treated as absent or complete: %+v", change)
				}
				if action == "still-unreadable" && change.NewDigest != "" {
					t.Fatalf("unreadable result acquired a digest: %+v", change)
				}
				mem := replayTestStore(t, root, changes)
				turn, err := mem.GetConversationTurn(context.Background(), "session", "turn")
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, summary := range turn.FileChanges {
					if summary.Path == change.Path {
						found = true
						detail, err := mem.GetTurnFileChange(context.Background(), "session", "turn", summary.ID)
						if err != nil || summary.Reversible || detail.Reversible {
							t.Fatalf("incomplete snapshot became reversible: %+v %+v %v", summary, detail, err)
						}
					}
				}
				if !found {
					t.Fatal("persisted turn omitted the incomplete snapshot")
				}
				if _, err := NewReplayer(mem).Apply(context.Background(), "session", "turn", ReplayUndo, []string{root}, replayTestRevision(t, mem)); !errors.Is(err, ErrReplayNotReversible) {
					t.Fatalf("unsafe Undo was accepted: %v", err)
				}
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
				assertReplayTestFile(t, root, "files/secret.txt", []byte(want))
				assertReplayTestFile(t, root, "files/neighbor.txt", []byte("neighbor after\n"))
			})
		}
	}
}
