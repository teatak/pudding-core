package turnfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestReplayerRejectsSameBytesSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	writeReplayTestFile(t, root, "source.txt", []byte("target.txt"))
	writeReplayTestFile(t, root, "target.txt", []byte("keep\n"))
	if err := os.Symlink("target.txt", filepath.Join(root, "destination")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	tracker := New()
	if err := tracker.BeginCall("tracked", "move", []string{root}, []string{filepath.Join(root, "source.txt"), filepath.Join(root, "destination")}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "source.txt"), filepath.Join(root, "destination")); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("tracked", "move"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("tracked")
	if err != nil || len(changes) != 2 {
		t.Fatalf("same-digest type change was omitted: %+v %v", changes, err)
	}
	mem := replayTestStore(t, root, changes)
	if _, err := NewReplayer(mem).Apply(context.Background(), "session", "turn", ReplayUndo, []string{root}, replayTestRevision(t, mem)); !errors.Is(err, ErrReplayNotReversible) {
		t.Fatalf("partially replayed a symlink replacement: %v", err)
	}
	assertReplayTestMissing(t, root, "source.txt")
	assertReplayTestFile(t, root, "destination", []byte("target.txt"))
	assertReplayTestFile(t, root, "target.txt", []byte("keep\n"))
}

func TestTrackerDoesNotPairDifferentTypesAsRename(t *testing.T) {
	root := t.TempDir()
	writeReplayTestFile(t, root, "target.txt", []byte("keep\n"))
	if err := os.Symlink("target.txt", filepath.Join(root, "old")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	tracker := New()
	if err := tracker.BeginCall("tracked", "replace", []string{root}, []string{filepath.Join(root, "old"), filepath.Join(root, "new")}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	writeReplayTestFile(t, root, "new", []byte("target.txt"))
	if err := tracker.EndCall("tracked", "replace"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("tracked")
	if err != nil || len(changes) != 2 {
		t.Fatalf("different file types were paired as a rename: %+v %v", changes, err)
	}
	for _, change := range changes {
		if change.Kind == store.FileChangeRenamed {
			t.Fatalf("different file types were paired as a rename: %+v", change)
		}
	}
}

func TestReplayerPreservesFilenameWhitespace(t *testing.T) {
	for _, name := range []string{" note.txt", "note.txt "} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.HasSuffix(name, " ") {
				t.Skip("Windows does not preserve trailing filename spaces")
			}
			root := t.TempDir()
			neighbor := strings.TrimSpace(name)
			// Equal current bytes make retargeting dangerous: a digest check
			// alone cannot distinguish the selected file from its neighbor.
			writeReplayTestFile(t, root, name, []byte("old\n"))
			writeReplayTestFile(t, root, neighbor, []byte("new\n"))
			tracker := New()
			if err := tracker.BeginCall("tracked", "edit", []string{root}, []string{filepath.Join(root, name)}); err != nil {
				t.Fatal(err)
			}
			writeReplayTestFile(t, root, name, []byte("new\n"))
			if err := tracker.EndCall("tracked", "edit"); err != nil {
				t.Fatal(err)
			}
			changes, err := tracker.Finish("tracked")
			if err != nil || len(changes) != 1 || changes[0].Path != name {
				t.Fatalf("tracking changed the path: %+v %v", changes, err)
			}
			mem := replayTestStore(t, root, changes)
			replayer := NewReplayer(mem)
			for _, direction := range []ReplayDirection{ReplayUndo, ReplayRedo} {
				if _, err := replayer.Apply(context.Background(), "session", "turn", direction, []string{root}, replayTestRevision(t, mem)); err != nil {
					t.Fatal(err)
				}
				want := "old\n"
				if direction == ReplayRedo {
					want = "new\n"
				}
				assertReplayTestFile(t, root, name, []byte(want))
				assertReplayTestFile(t, root, neighbor, []byte("new\n"))
			}
		})
	}
}

func TestReplayerPreservesRenamedFilenameWhitespace(t *testing.T) {
	root := t.TempDir()
	original, renamed := " original.txt", " renamed.txt"
	writeReplayTestFile(t, root, original, []byte("selected\n"))
	for _, neighbor := range []string{"original.txt", "renamed.txt"} {
		writeReplayTestFile(t, root, neighbor, []byte("neighbor\n"))
	}
	tracker := New()
	if err := tracker.BeginCall("tracked", "move", []string{root}, []string{filepath.Join(root, original), filepath.Join(root, renamed)}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, original), filepath.Join(root, renamed)); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("tracked", "move"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("tracked")
	if err != nil || len(changes) != 1 || changes[0].Kind != store.FileChangeRenamed {
		t.Fatalf("rename tracking: %+v %v", changes, err)
	}
	mem := replayTestStore(t, root, changes)
	replayer := NewReplayer(mem)
	if _, err := replayer.Apply(context.Background(), "session", "turn", ReplayUndo, []string{root}, replayTestRevision(t, mem)); err != nil {
		t.Fatal(err)
	}
	assertReplayTestFile(t, root, original, []byte("selected\n"))
	assertReplayTestMissing(t, root, renamed)
	if _, err := replayer.Apply(context.Background(), "session", "turn", ReplayRedo, []string{root}, replayTestRevision(t, mem)); err != nil {
		t.Fatal(err)
	}
	assertReplayTestMissing(t, root, original)
	assertReplayTestFile(t, root, renamed, []byte("selected\n"))
	for _, neighbor := range []string{"original.txt", "renamed.txt"} {
		assertReplayTestFile(t, root, neighbor, []byte("neighbor\n"))
	}
}
