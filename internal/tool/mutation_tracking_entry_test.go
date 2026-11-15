package tool

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/turnfiles"
)

func TestFileDeleteTracksSymlinkEntry(t *testing.T) {
	root := projectContractDir(t)
	writePatchTestFile(t, filepath.Join(root, "source.txt"), "keep\n")
	if err := os.Symlink("source.txt", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	call := projectContractCall(FileDelete, []string{root}, map[string]any{"scope": "project", "path": "alias"})
	tracking, ok := MutationTrackingForCall(call)
	if !ok || !reflect.DeepEqual(tracking.Targets, []string{filepath.Join(root, "alias")}) {
		t.Fatalf("delete tracking followed the link: %+v", tracking)
	}
	tracker := turnfiles.New()
	if err := tracker.BeginCall("turn", "call", call.ProjectDirs, tracking.Targets); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	if result := runner.Call(context.Background(), call); !result.Ok {
		t.Fatal(result.Content)
	}
	if err := tracker.EndCall("turn", "call"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn")
	if err != nil || len(changes) != 1 || changes[0].Path != "alias" || changes[0].Kind != store.FileChangeDeleted || changes[0].OldType != "symlink" {
		t.Fatalf("link deletion receipt: %+v %v", changes, err)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "source.txt")); got != "keep\n" {
		t.Fatalf("referent changed: %q", got)
	}
}

func TestFileMoveTracksSameBytesTypeChange(t *testing.T) {
	root := projectContractDir(t)
	writePatchTestFile(t, filepath.Join(root, "source.txt"), "target.txt")
	writePatchTestFile(t, filepath.Join(root, "target.txt"), "keep\n")
	if err := os.Symlink("target.txt", filepath.Join(root, "destination")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	call := projectContractCall(FileMove, []string{root}, map[string]any{"scope": "project", "from_path": "source.txt", "to_path": "destination", "overwrite": true})
	tracking, ok := MutationTrackingForCall(call)
	if !ok {
		t.Fatal("move tracking unavailable")
	}
	tracker := turnfiles.New()
	if err := tracker.BeginCall("turn", "call", call.ProjectDirs, tracking.Targets); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	if result := runner.Call(context.Background(), call); !result.Ok {
		t.Fatal(result.Content)
	}
	if err := tracker.EndCall("turn", "call"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn")
	if err != nil || len(changes) != 2 {
		t.Fatalf("move omitted same-digest type change: %+v %v", changes, err)
	}
	byPath := make(map[string]store.TurnFileChangeInput)
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if change := byPath["destination"]; change.Kind != store.FileChangeModified || change.OldType != "symlink" || change.NewType != "file" || change.OldDigest != change.NewDigest {
		t.Fatalf("destination type change missing: %+v", change)
	}
	if change := byPath["source.txt"]; change.Kind != store.FileChangeDeleted || change.OldType != "file" {
		t.Fatalf("source deletion missing: %+v", change)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "target.txt")); got != "keep\n" {
		t.Fatalf("referent changed: %q", got)
	}
}

func TestCommandTrackingPreservesSymlinkEntryAndReferent(t *testing.T) {
	for _, declared := range []bool{false, true} {
		for _, operation := range []string{"unlink", "write"} {
			t.Run(operation+"/declared="+map[bool]string{false: "false", true: "true"}[declared], func(t *testing.T) {
				if runtime.GOOS == "windows" && !declared {
					t.Skip("static command observation uses POSIX shell syntax")
				}
				root := projectContractDir(t)
				writePatchTestFile(t, filepath.Join(root, "source.txt"), "before\n")
				link := filepath.Join(root, "alias")
				if err := os.Symlink("source.txt", link); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				command := "rm alias"
				if operation == "write" {
					command = "printf after > alias"
				}
				args := map[string]any{"scope": "project", "command": command}
				if declared {
					args["command"] = "opaque-command"
					args["mutation_paths"] = []string{"alias"}
				}
				call := projectContractCall(CommandRun, []string{root}, args)
				tracking, ok := MutationTrackingForCall(call)
				if !ok {
					t.Fatal("command observation unavailable")
				}
				tracker := turnfiles.New()
				if err := tracker.BeginCallWithOrigin("turn", "call", call.ProjectDirs, tracking.Targets, tracking.Origin, tracking.ExactFiles...); err != nil {
					t.Fatal(err)
				}
				// Exercise the two filesystem effects a command can have on the
				// same explicit operand without needing an external CLI fixture.
				if operation == "unlink" {
					if err := os.Remove(link); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(link, []byte("after"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := tracker.EndCall("turn", "call"); err != nil {
					t.Fatal(err)
				}
				changes, err := tracker.Finish("turn")
				if err != nil || len(changes) != 1 || changes[0].Origin != store.FileChangeOriginCommandObserved {
					t.Fatalf("command receipt: %+v %v", changes, err)
				}
				if operation == "unlink" {
					if changes[0].Path != "alias" || changes[0].Kind != store.FileChangeDeleted || changes[0].OldType != "symlink" || readPatchTestFile(t, filepath.Join(root, "source.txt")) != "before\n" {
						t.Fatalf("unlink attributed to referent: %+v", changes)
					}
				} else if changes[0].Path != "source.txt" || changes[0].Kind != store.FileChangeModified || changes[0].NewContent != "after" {
					t.Fatalf("referent write omitted: %+v", changes)
				}
			})
		}
	}
}

func TestCommandMutationPathsPreserveWhitespace(t *testing.T) {
	for _, name := range []string{" note.txt", "note.txt "} {
		if runtime.GOOS == "windows" && strings.HasSuffix(name, " ") {
			continue
		}
		root := projectContractDir(t)
		writePatchTestFile(t, filepath.Join(root, name), "selected\n")
		writePatchTestFile(t, filepath.Join(root, strings.TrimSpace(name)), "neighbor\n")
		for _, declared := range []bool{false, true} {
			if runtime.GOOS == "windows" && !declared {
				continue
			}
			args := map[string]any{"scope": "project", "command": "rm -- " + quoteShellArg(name)}
			if declared {
				args["command"] = "opaque-command"
				args["mutation_paths"] = []string{name}
			}
			tracking, ok := MutationTrackingForCall(projectContractCall(CommandRun, []string{root}, args))
			paths := append(append([]string(nil), tracking.Targets...), tracking.ExactFiles...)
			if !ok || !reflect.DeepEqual(paths, []string{filepath.Join(root, name)}) {
				t.Fatalf("command path %q changed: %+v", name, tracking)
			}
		}
	}
}
