package turnfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestTrackerKeepsFirstBaselineAcrossParentGrantsAndReplay(t *testing.T) {
	for _, observation := range []string{"structured", "command-known"} {
		for _, grant := range []string{"add-parent", "replace-with-parent"} {
			t.Run(observation+"/"+grant, func(t *testing.T) {
				outer, inner := identityTestRoots(t)
				path := filepath.Join(inner, "file.txt")
				writeReplayTestFile(t, inner, "file.txt", []byte("initial\n"))
				tracker := New()
				if err := tracker.BeginCall("tracked", "first", []string{inner}, []string{path}); err != nil {
					t.Fatal(err)
				}
				writeReplayTestFile(t, inner, "file.txt", []byte("middle\n"))
				if err := tracker.EndCall("tracked", "first"); err != nil {
					t.Fatal(err)
				}
				roots := []string{inner, outer}
				if grant == "replace-with-parent" {
					roots = []string{outer}
				}
				targets, origin := []string{path}, store.FileChangeOriginStructured
				if observation == "command-known" {
					targets, origin = nil, store.FileChangeOriginCommandObserved
				}
				if err := tracker.BeginCallWithOrigin("tracked", "second", roots, targets, origin); err != nil {
					t.Fatal(err)
				}
				writeReplayTestFile(t, inner, "file.txt", []byte("final\n"))
				if err := tracker.EndCall("tracked", "second"); err != nil {
					t.Fatal(err)
				}
				changes, err := tracker.Finish("tracked")
				if err != nil || len(changes) != 1 {
					t.Fatalf("one entry acquired multiple identities: %+v %v", changes, err)
				}
				change := changes[0]
				if change.RootPath != inner || change.Path != "file.txt" || change.OldContent != "initial\n" || change.NewContent != "final\n" || change.Origin != origin {
					t.Fatalf("first baseline or latest result changed: %+v", change)
				}
				st := replayTestStore(t, inner, changes)
				replayer := NewReplayer(st)
				unauthorized := []string{t.TempDir()}
				for _, direction := range []ReplayDirection{ReplayUndo, ReplayRedo} {
					revision := replayTestRevision(t, st)
					if _, err := replayer.Apply(context.Background(), "session", "turn", direction, unauthorized, revision); !errors.Is(err, ErrReplayUnauthorized) {
						t.Fatalf("revoked authority allowed %s: %v", direction, err)
					}
					if got := replayTestRevision(t, st); got != revision {
						t.Fatal("rejected replay advanced the persisted state")
					}
					authorized := []string{outer}
					wantBefore, wantAfter := "final\n", "initial\n"
					if direction == ReplayRedo {
						authorized = []string{inner}
						wantBefore, wantAfter = "initial\n", "final\n"
					}
					assertReplayTestFile(t, inner, "file.txt", []byte(wantBefore))
					if _, err := replayer.Apply(context.Background(), "session", "turn", direction, authorized, revision); err != nil {
						t.Fatal(err)
					}
					assertReplayTestFile(t, inner, "file.txt", []byte(wantAfter))
				}
			})
		}
	}
}

func TestTrackerRootChangePreservesNetRevert(t *testing.T) {
	outer, inner := identityTestRoots(t)
	path := filepath.Join(inner, "file.txt")
	writeReplayTestFile(t, inner, "file.txt", []byte("initial\n"))
	tracker := New()
	for index, content := range []string{"middle\n", "initial\n"} {
		roots, call := []string{inner}, "first"
		if index == 1 {
			roots, call = []string{outer}, "second"
		}
		if err := tracker.BeginCall("turn", call, roots, []string{path}); err != nil {
			t.Fatal(err)
		}
		writeReplayTestFile(t, inner, "file.txt", []byte(content))
		if err := tracker.EndCall("turn", call); err != nil {
			t.Fatal(err)
		}
	}
	if changes, err := tracker.Finish("turn"); err != nil || len(changes) != 0 {
		t.Fatalf("returning to the first baseline left false changes: %+v %v", changes, err)
	}
}

func TestTrackerRootChangePreservesRename(t *testing.T) {
	outer, inner := identityTestRoots(t)
	oldPath, newPath := filepath.Join(inner, "old.txt"), filepath.Join(inner, "new.txt")
	writeReplayTestFile(t, inner, "old.txt", []byte("same\n"))
	tracker := New()
	if err := tracker.BeginCall("turn", "first", []string{inner}, []string{oldPath}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("turn", "first"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.BeginCall("turn", "rename", []string{outer}, []string{oldPath, newPath}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("turn", "rename"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn")
	if err != nil || len(changes) != 1 || changes[0].Kind != store.FileChangeRenamed || changes[0].RootPath != inner || changes[0].Path != "new.txt" || changes[0].OriginalPath != "old.txt" {
		t.Fatalf("root expansion lost the rename: %+v %v", changes, err)
	}
}

func TestTrackerKnownFilesAndReplayUseCurrentAuthority(t *testing.T) {
	outer, inner := identityTestRoots(t)
	inside, outside := filepath.Join(inner, "file.txt"), filepath.Join(outer, "outside.txt")
	writeReplayTestFile(t, inner, "file.txt", []byte("initial\n"))
	writeReplayTestFile(t, outer, "outside.txt", []byte("initial outside\n"))
	tracker := New()
	if err := tracker.BeginCall("tracked", "first", []string{outer}, []string{inside, outside}); err != nil {
		t.Fatal(err)
	}
	writeReplayTestFile(t, inner, "file.txt", []byte("middle\n"))
	writeReplayTestFile(t, outer, "outside.txt", []byte("tracked outside\n"))
	if err := tracker.EndCall("tracked", "first"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.BeginCallWithOrigin("tracked", "narrowed", []string{inner}, nil, store.FileChangeOriginCommandObserved); err != nil {
		t.Fatal(err)
	}
	writeReplayTestFile(t, inner, "file.txt", []byte("final\n"))
	// Model an unrelated external writer after access to this sibling was revoked.
	writeReplayTestFile(t, outer, "outside.txt", []byte("external outside\n"))
	if err := tracker.EndCall("tracked", "narrowed"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("tracked")
	if err != nil || len(changes) != 2 {
		t.Fatalf("narrowed scope lost tracked files: %+v %v", changes, err)
	}
	for _, change := range changes {
		switch change.Path {
		case "sub/file.txt":
			if change.OldContent != "initial\n" || change.NewContent != "final\n" {
				t.Fatalf("authorized known file lost its baseline: %+v", change)
			}
		case "outside.txt":
			if change.NewContent != "tracked outside\n" {
				t.Fatalf("revoked file was still observed: %+v", change)
			}
		default:
			t.Fatalf("unexpected tracked path: %+v", change)
		}
	}
	st := replayTestStore(t, outer, changes)
	if _, err := NewReplayer(st).Apply(context.Background(), "session", "turn", ReplayUndo, []string{inner}, replayTestRevision(t, st)); !errors.Is(err, ErrReplayUnauthorized) {
		t.Fatalf("historical parent root granted replay access: %v", err)
	}
	assertReplayTestFile(t, inner, "file.txt", []byte("final\n"))
	assertReplayTestFile(t, outer, "outside.txt", []byte("external outside\n"))
}

func TestReplayerAuthorizesActualFileUnderCurrentChildRoot(t *testing.T) {
	outer, inner := identityTestRoots(t)
	writeReplayTestFile(t, inner, "file.txt", []byte("after\n"))
	change := replayTestChange(t, outer, "sub/file.txt", "", store.FileChangeModified, []byte("before\n"), []byte("after\n"), false)
	st := replayTestStore(t, outer, []store.TurnFileChangeInput{change})
	replayer := NewReplayer(st)
	if _, err := replayer.Apply(context.Background(), "session", "turn", ReplayUndo, []string{inner}, replayTestRevision(t, st)); err != nil {
		t.Fatal(err)
	}
	assertReplayTestFile(t, inner, "file.txt", []byte("before\n"))
	if _, err := replayer.Apply(context.Background(), "session", "turn", ReplayRedo, []string{inner}, replayTestRevision(t, st)); err != nil {
		t.Fatal(err)
	}
	assertReplayTestFile(t, inner, "file.txt", []byte("after\n"))
}

func TestReplayerParentGrantDoesNotFollowFinalSymlink(t *testing.T) {
	outer, inner := identityTestRoots(t)
	writeReplayTestFile(t, outer, "referent.txt", []byte("after\n"))
	if err := os.Symlink(filepath.Join(outer, "referent.txt"), filepath.Join(inner, "file.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	change := replayTestChange(t, inner, "file.txt", "", store.FileChangeModified, []byte("before\n"), []byte("after\n"), false)
	st := replayTestStore(t, inner, []store.TurnFileChangeInput{change})
	if _, err := NewReplayer(st).Apply(context.Background(), "session", "turn", ReplayUndo, []string{outer}, replayTestRevision(t, st)); !errors.Is(err, ErrReplayUnauthorized) {
		t.Fatalf("parent grant allowed replay through a replacement link: %v", err)
	}
	assertReplayTestFile(t, outer, "referent.txt", []byte("after\n"))
	if info, err := os.Lstat(filepath.Join(inner, "file.txt")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("rejected replay replaced the link: %v", err)
	}
}

func identityTestRoots(t *testing.T) (string, string) {
	t.Helper()
	outer, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "sub")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	return outer, inner
}
