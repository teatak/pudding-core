package turnfiles

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestReplayerUndoRedoWholeTurn(t *testing.T) {
	root := t.TempDir()
	writeReplayTestFile(t, root, "modified.txt", []byte("new\n"))
	writeReplayTestFile(t, root, "added.bin", []byte{0, 1, 2})
	writeReplayTestFile(t, root, "renamed.txt", []byte("renamed\n"))

	changes := []store.TurnFileChangeInput{
		replayTestChange(root, "modified.txt", "", store.FileChangeModified, []byte("old\n"), []byte("new\n"), false),
		replayTestChange(root, "added.bin", "", store.FileChangeAdded, nil, []byte{0, 1, 2}, true),
		replayTestChange(root, "deleted.txt", "", store.FileChangeDeleted, []byte("deleted\n"), nil, false),
		replayTestChange(root, "renamed.txt", "original.txt", store.FileChangeRenamed, []byte("renamed\n"), []byte("renamed\n"), false),
	}
	mem := replayTestStore(t, root, changes)
	replayer := NewReplayer(mem)

	state, err := replayer.Apply(context.Background(), "session", "turn", ReplayUndo, []string{root})
	if err != nil || state != store.TurnFileChangesUndone {
		t.Fatalf("undo state=%q err=%v", state, err)
	}
	assertReplayTestFile(t, root, "modified.txt", []byte("old\n"))
	assertReplayTestMissing(t, root, "added.bin")
	assertReplayTestFile(t, root, "deleted.txt", []byte("deleted\n"))
	assertReplayTestMissing(t, root, "renamed.txt")
	assertReplayTestFile(t, root, "original.txt", []byte("renamed\n"))

	state, err = replayer.Apply(context.Background(), "session", "turn", ReplayRedo, []string{root})
	if err != nil || state != store.TurnFileChangesApplied {
		t.Fatalf("redo state=%q err=%v", state, err)
	}
	assertReplayTestFile(t, root, "modified.txt", []byte("new\n"))
	assertReplayTestFile(t, root, "added.bin", []byte{0, 1, 2})
	assertReplayTestMissing(t, root, "deleted.txt")
	assertReplayTestMissing(t, root, "original.txt")
	assertReplayTestFile(t, root, "renamed.txt", []byte("renamed\n"))
}

func TestReplayerConflictIsAllOrNothing(t *testing.T) {
	root := t.TempDir()
	writeReplayTestFile(t, root, "one.txt", []byte("new one"))
	writeReplayTestFile(t, root, "two.txt", []byte("externally changed"))
	changes := []store.TurnFileChangeInput{
		replayTestChange(root, "one.txt", "", store.FileChangeModified, []byte("old one"), []byte("new one"), false),
		replayTestChange(root, "two.txt", "", store.FileChangeModified, []byte("old two"), []byte("new two"), false),
	}
	mem := replayTestStore(t, root, changes)
	_, err := NewReplayer(mem).Apply(context.Background(), "session", "turn", ReplayUndo, []string{root})
	if !errors.Is(err, ErrReplayConflict) {
		t.Fatalf("undo error=%v, want conflict", err)
	}
	assertReplayTestFile(t, root, "one.txt", []byte("new one"))
	assertReplayTestFile(t, root, "two.txt", []byte("externally changed"))
	turn, err := mem.GetConversationTurn(context.Background(), "session", "turn")
	if err != nil || turn.FileChangeState != store.TurnFileChangesApplied {
		t.Fatalf("state=%q err=%v", turn.FileChangeState, err)
	}
}

func TestReplayerHandlesTextBinaryConversion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mixed.dat")
	writeReplayTestFile(t, root, "mixed.dat", []byte("text\n"))
	tracker := New()
	if err := tracker.BeginCall("tracked", "call", []string{root}, []string{path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tracker.EndCall("tracked", "call"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("tracked")
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	mem := replayTestStore(t, root, changes)
	replayer := NewReplayer(mem)
	if _, err := replayer.Apply(context.Background(), "session", "turn", ReplayUndo, []string{root}); err != nil {
		t.Fatal(err)
	}
	assertReplayTestFile(t, root, "mixed.dat", []byte("text\n"))
	if _, err := replayer.Apply(context.Background(), "session", "turn", ReplayRedo, []string{root}); err != nil {
		t.Fatal(err)
	}
	assertReplayTestFile(t, root, "mixed.dat", []byte{0, 1, 2})
}

func TestReplayerRejectsOldSnapshotsAndFormerRoots(t *testing.T) {
	root := t.TempDir()
	writeReplayTestFile(t, root, "file.txt", []byte("new"))
	change := replayTestChange(root, "file.txt", "", store.FileChangeModified, []byte("old"), []byte("new"), false)
	change.SnapshotVersion = 0
	mem := replayTestStore(t, root, []store.TurnFileChangeInput{change})
	_, err := NewReplayer(mem).Apply(context.Background(), "session", "turn", ReplayUndo, []string{root})
	if !errors.Is(err, ErrReplayNotReversible) {
		t.Fatalf("old snapshot error=%v", err)
	}

	change.SnapshotVersion = 1
	mem = replayTestStore(t, root, []store.TurnFileChangeInput{change})
	_, err = NewReplayer(mem).Apply(context.Background(), "session", "turn", ReplayUndo, []string{t.TempDir()})
	if !errors.Is(err, ErrReplayUnauthorized) {
		t.Fatalf("former root error=%v", err)
	}
}

func TestReplayerRejectsActionWhileSessionTurnIsRunning(t *testing.T) {
	root := t.TempDir()
	writeReplayTestFile(t, root, "file.txt", []byte("new"))
	change := replayTestChange(root, "file.txt", "", store.FileChangeModified, []byte("old"), []byte("new"), false)
	mem := replayTestStore(t, root, []store.TurnFileChangeInput{change})
	if _, err := mem.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID: "session", TurnID: "running", UserMessageID: "running_user", ClientMessageID: "running_client", UserText: "running",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := NewReplayer(mem).Apply(context.Background(), "session", "turn", ReplayUndo, []string{root})
	if !errors.Is(err, store.ErrTurnRunning) {
		t.Fatalf("running turn error=%v", err)
	}
	assertReplayTestFile(t, root, "file.txt", []byte("new"))
}

func replayTestStore(t *testing.T, root string, changes []store.TurnFileChangeInput) *memstore.Memstore {
	t.Helper()
	ctx := context.Background()
	mem := memstore.New()
	if err := mem.CreateProject(ctx, &store.Project{ID: "project", Name: "project", RootDirs: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateSession(ctx, &store.Session{ID: "session", Title: "session", Provider: "test", Model: "test", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "test", Provider: "test", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted, FileChanges: changes}); err != nil {
		t.Fatal(err)
	}
	return mem
}

func replayTestChange(root, path, original string, kind store.FileChangeKind, oldData, newData []byte, binary bool) store.TurnFileChangeInput {
	change := store.TurnFileChangeInput{
		RootPath: root, Path: path, OriginalPath: original, Kind: kind, Binary: binary, SnapshotVersion: 1,
		OldMode: 0o644, NewMode: 0o644,
	}
	if oldData != nil {
		change.OldType = "file"
		change.OldDigest = fmt.Sprintf("%x", sha256.Sum256(oldData))
		change.OldSize = int64(len(oldData))
		if binary {
			change.OldBinary = true
			change.OldData = append([]byte(nil), oldData...)
		} else {
			change.OldContent = string(oldData)
		}
	}
	if newData != nil {
		change.NewType = "file"
		change.NewDigest = fmt.Sprintf("%x", sha256.Sum256(newData))
		change.NewSize = int64(len(newData))
		if binary {
			change.NewBinary = true
			change.NewData = append([]byte(nil), newData...)
		} else {
			change.NewContent = string(newData)
		}
	}
	return change
}

func writeReplayTestFile(t *testing.T, root, path string, data []byte) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertReplayTestFile(t *testing.T, root, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, path))
	if err != nil || string(got) != string(want) {
		t.Fatalf("%s=%q err=%v, want %q", path, got, err, want)
	}
}

func assertReplayTestMissing(t *testing.T, root, path string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still exists: %v", path, err)
	}
}
