package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/turnfiles"
)

func TestFileCopyAndMoveTrackModeOnlyOverwriteAndReplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable permission bits are a POSIX filesystem property")
	}
	for _, operation := range []string{FileCopy, FileMove} {
		t.Run(operation, func(t *testing.T) {
			root := projectContractDir(t)
			for name, mode := range map[string]os.FileMode{"source.sh": 0o755, "destination.sh": 0o644} {
				path := filepath.Join(root, name)
				writePatchTestFile(t, path, "#!/bin/sh\nexit 0\n")
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			args := map[string]any{"scope": "project", "from_path": "source.sh", "to_path": "destination.sh", "overwrite": true}
			if operation == FileCopy {
				args = map[string]any{
					"from": map[string]string{"scope": "project", "path": "source.sh"},
					"to": map[string]string{"scope": "project", "path": "destination.sh"}, "overwrite": true,
				}
			}
			call := projectContractCall(operation, []string{root}, args)
			tracking, ok := MutationTrackingForCall(call)
			if !ok {
				t.Fatal("structured mutation tracking unavailable")
			}
			tracker := turnfiles.New()
			if err := tracker.BeginCall("tracked", "call", call.ProjectDirs, tracking.Targets); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner()
			defer runner.Close()
			if result := runner.Call(context.Background(), call); !result.Ok {
				t.Fatal(result.Content)
			}
			if err := tracker.EndCall("tracked", "call"); err != nil {
				t.Fatal(err)
			}
			changes, err := tracker.Finish("tracked")
			wantCount := 1
			if operation == FileMove {
				wantCount = 2
			}
			if err != nil || len(changes) != wantCount {
				t.Fatalf("mode-only overwrite was not fully recorded: %+v %v", changes, err)
			}
			var destination store.TurnFileChangeInput
			for _, change := range changes {
				if change.Path == "destination.sh" {
					destination = change
				}
			}
			if destination.Kind != store.FileChangeModified || destination.OldMode != 0o644 || destination.NewMode != 0o755 || destination.OldDigest != destination.NewDigest {
				t.Fatalf("incorrect permission change: %+v", destination)
			}
			st := persistFileModeChanges(t, root, changes)
			replayer := turnfiles.NewReplayer(st)
			for _, direction := range []turnfiles.ReplayDirection{turnfiles.ReplayUndo, turnfiles.ReplayRedo} {
				turn, err := st.GetConversationTurn(context.Background(), "session", "turn")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := replayer.Apply(context.Background(), "session", "turn", direction, []string{root}, turn.FileChangeRevision); err != nil {
					t.Fatal(err)
				}
				wantMode := os.FileMode(0o644)
				if direction == turnfiles.ReplayRedo {
					wantMode = 0o755
				}
				if info, err := os.Stat(filepath.Join(root, "destination.sh")); err != nil || info.Mode().Perm() != wantMode {
					t.Fatalf("destination mode after %s: info=%v err=%v", direction, info, err)
				}
				if got := readPatchTestFile(t, filepath.Join(root, "destination.sh")); got != "#!/bin/sh\nexit 0\n" {
					t.Fatalf("permission replay changed content: %q", got)
				}
				info, err := os.Stat(filepath.Join(root, "source.sh"))
				if operation == FileMove && direction == turnfiles.ReplayRedo {
					if !os.IsNotExist(err) {
						t.Fatalf("redo did not remove the move source: %v", err)
					}
				} else if err != nil || info.Mode().Perm() != 0o755 {
					t.Fatalf("source mode changed after %s: info=%v err=%v", direction, info, err)
				}
			}
		})
	}
}

func persistFileModeChanges(t *testing.T, root string, changes []store.TurnFileChangeInput) store.Store {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.CreateProject(ctx, &store.Project{ID: "project", RootDirs: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", UserMessageID: "user", ClientMessageID: "client", UserText: "change executable permissions"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted, FileChanges: changes}); err != nil {
		t.Fatal(err)
	}
	return st
}
