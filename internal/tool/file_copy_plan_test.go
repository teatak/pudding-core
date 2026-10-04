package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/home"
)

func copyPathCall(from, to string, roots ...string) Call {
	raw, _ := json.Marshal(map[string]any{"from": map[string]string{"path": from}, "to": map[string]string{"path": to}})
	return Call{Name: FileCopy, SessionID: "session", TurnID: "turn", CallID: "copy", ProjectDirs: roots, Args: raw}
}

func TestFileCopyAbsolutePathsWithoutScope(t *testing.T) {
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	root := t.TempDir()
	seed := runner.Call(context.Background(), Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"temp","path":"page.html","content":"hello"}`)})
	if !seed.Ok {
		t.Fatal(seed.Content)
	}
	temp, _, _ := runner.managedRoot(managedScopeTemp)
	call := copyPathCall(filepath.Join(temp, "page.html"), filepath.Join(root, "page.html"), root)
	result := runner.Call(context.Background(), call)
	payload := decodeToolResult(t, result)
	if !result.Ok || payload["fromScope"] != "temp" || payload["toScope"] != "project" {
		t.Fatal(result.Content)
	}
	call = copyPathCall(filepath.Join(root, "page.html"), filepath.Join(temp, "export.html"), root)
	if result := runner.Call(context.Background(), call); !result.Ok {
		t.Fatal(result.Content)
	}
	call = copyPathCall("page.html", filepath.Join(root, "again.html"), root)
	if result := runner.Call(context.Background(), call); result.Ok || !strings.Contains(result.Content, "scope is required for a relative path") {
		t.Fatal(result.Content)
	}
}

func TestFileCopyGrantIsBoundedAndSingleUse(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	source := filepath.Join(outside, "source.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := copyPathCall(source, filepath.Join(root, "target.txt"), root)
	if result := runner.Call(context.Background(), call); result.Ok || decodeToolResult(t, result)["reason"] != "path_not_authorized" {
		t.Fatal(result.Content)
	}
	for _, field := range []string{"session", "turn", "call", "args"} {
		t.Run(field, func(t *testing.T) {
			details, err := runner.ApprovalDetails(context.Background(), call)
			if err != nil {
				t.Fatal(err)
			}
			changed := call
			changed.FileCopyGrant = FileCopyPlanFromDetails(details)
			switch field {
			case "session":
				changed.SessionID = "other"
			case "turn":
				changed.TurnID = "other"
			case "call":
				changed.CallID = "other"
			case "args":
				changed.Args = copyPathCall(source, filepath.Join(root, "other.txt"), root).Args
			}
			if result := runner.Call(context.Background(), changed); result.Ok || decodeToolResult(t, result)["reason"] != "copy_approval_changed" {
				t.Fatal(result.Content)
			}
		})
	}
	details, err := runner.ApprovalDetails(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	call.FileCopyGrant = FileCopyPlanFromDetails(details)
	if !call.FileCopyGrant.ExternalAccess || !call.FileCopyGrant.From.External || call.FileCopyGrant.To.External {
		t.Fatalf("wrong authority: %+v", call.FileCopyGrant)
	}
	if result := runner.Call(context.Background(), call); !result.Ok {
		t.Fatal(result.Content)
	}
	if result := runner.Call(context.Background(), call); result.Ok || decodeToolResult(t, result)["reason"] != "copy_approval_changed" {
		t.Fatal(result.Content)
	}
}

func TestFileCopyApprovalRejectsChangedPathsAndTargets(t *testing.T) {
	for _, scenario := range []string{"symlink", "overwrite"} {
		t.Run(scenario, func(t *testing.T) {
			root, outside, other := t.TempDir(), t.TempDir(), t.TempDir()
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			source := filepath.Join(root, "source")
			if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(outside, "target")
			if scenario == "symlink" {
				if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(root, "alias", "target")
			} else {
				if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			call := copyPathCall(source, target, root)
			if scenario == "overwrite" {
				var args map[string]any
				_ = json.Unmarshal(call.Args, &args)
				args["overwrite"] = true
				call.Args, _ = json.Marshal(args)
			}
			details, err := runner.ApprovalDetails(context.Background(), call)
			if err != nil {
				t.Fatal(err)
			}
			call.FileCopyGrant = FileCopyPlanFromDetails(details)
			if scenario == "symlink" {
				if err := os.Remove(filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(target, []byte("user-edited-after-approval"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if result := runner.Call(context.Background(), call); result.Ok || decodeToolResult(t, result)["reason"] != "copy_approval_changed" {
				t.Fatal(result.Content)
			}
			if scenario == "overwrite" {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "user-edited-after-approval" {
					t.Fatal("changed target overwritten")
				}
			} else if _, err := os.Stat(filepath.Join(other, "target")); !os.IsNotExist(err) {
				t.Fatal("followed changed symlink")
			}
		})
	}
}

func TestFileCopyAbsoluteManagedPathsKeepRestrictions(t *testing.T) {
	homeDir, root := t.TempDir(), t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(homeDir))
	temp, _, _ := runner.managedRoot(managedScopeTemp)
	if err := os.MkdirAll(filepath.Join(temp, ".reserved"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, ".reserved", "file"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(temp, ".reserved"), filepath.Join(temp, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".reserved/file", "alias/file"} {
		call := copyPathCall(filepath.Join(temp, name), filepath.Join(root, "target"), root)
		if _, err := runner.ApprovalDetails(context.Background(), call); err == nil {
			t.Fatal("reserved managed file accepted")
		}
	}
}

func TestFileCopyAbsoluteAuthorizedScratch(t *testing.T) {
	homeDir := t.TempDir()
	root, err := home.PrepareCodeScratch(homeDir, "session")
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(homeDir))
	source := filepath.Join(root, "source.txt")
	writeCopyFixture(t, source, "source")
	for _, explicitScope := range []bool{false, true} {
		name := "implicit.txt"
		if explicitScope {
			name = "explicit.txt"
		}
		call := copyPathCall(source, filepath.Join(root, name), root)
		if explicitScope {
			var args fileCopyArgs
			_ = json.Unmarshal(call.Args, &args)
			args.From.Scope, args.To.Scope = managedScopeProject, managedScopeProject
			call.Args, _ = json.Marshal(args)
		}
		details, err := runner.ApprovalDetails(context.Background(), call)
		if err != nil {
			t.Fatal(err)
		}
		call.FileCopyGrant = FileCopyPlanFromDetails(details)
		if call.FileCopyGrant.ExternalAccess || call.FileCopyGrant.To.Scope != managedScopeProject {
			t.Fatal("authorized scratch lost its project scope")
		}
		if result := runner.Call(context.Background(), call); !result.Ok {
			t.Fatal(result.Content)
		}
		if tracking, ok := MutationTrackingForCall(call); !ok || len(tracking.Targets) != 1 || tracking.Targets[0] != filepath.Join(root, name) {
			t.Fatalf("scratch copy not tracked: %+v", tracking)
		}
	}
	other, err := home.PrepareCodeScratch(homeDir, "other")
	if err != nil {
		t.Fatal(err)
	}
	writeCopyFixture(t, filepath.Join(other, "source.txt"), "other session")
	for _, call := range []Call{
		copyPathCall(filepath.Join(other, "source.txt"), filepath.Join(root, "other.txt"), root),
		copyPathCall(source, filepath.Join(root, "unauthorized.txt")),
	} {
		if _, err := runner.ApprovalDetails(context.Background(), call); err == nil {
			t.Fatal("ungranted or other-session scratch accepted")
		}
	}
}

func writeCopyFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func overwriteCopyCall(call Call, recursive bool) Call {
	var args fileCopyArgs
	_ = json.Unmarshal(call.Args, &args)
	args.Overwrite, args.Recursive = true, recursive
	call.Args, _ = json.Marshal(args)
	return call
}

func TestFileCopyPlanRejectsReplacementWithSameMetadata(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	writeCopyFixture(t, source, "before")
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	call := copyPathCall(source, target, root)
	args, err := decodeFileCopyArgs(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	before, err := runner.prepareFileCopy(call, args, false)
	if err != nil {
		t.Fatal(err)
	}
	writeCopyFixture(t, source+".new", "after!")
	if err := os.Chtimes(source+".new", before.sourceInfo.ModTime(), before.sourceInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source+".new", source); err != nil {
		t.Fatal(err)
	}
	after, err := runner.prepareFileCopy(call, args, false)
	if err != nil {
		t.Fatal(err)
	}
	if before.matches(after) {
		t.Fatal("replacement with identical metadata retained approval identity")
	}
}

func TestFileCopyApprovalRejectsChangedDirectoryEntries(t *testing.T) {
	for _, endpoint := range []string{"source", "destination"} {
		for _, change := range []string{"edit", "add", "remove", "replace"} {
			t.Run(endpoint+"/"+change, func(t *testing.T) {
				root, external := t.TempDir(), t.TempDir()
				source, target := filepath.Join(root, "source"), filepath.Join(external, "target")
				writeCopyFixture(t, filepath.Join(source, "nested", "file"), "source")
				writeCopyFixture(t, filepath.Join(target, "nested", "file"), "original")
				runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
				call := overwriteCopyCall(copyPathCall(source, target, root), true)
				details, err := runner.ApprovalDetails(context.Background(), call)
				if err != nil {
					t.Fatal(err)
				}
				call.FileCopyGrant = FileCopyPlanFromDetails(details)
				dir := target
				if endpoint == "source" {
					dir = source
				}
				file := filepath.Join(dir, "nested", "file")
				switch change {
				case "edit":
					writeCopyFixture(t, file, "edited while waiting")
					if err := os.Chtimes(file, time.Now(), time.Now().Add(time.Second)); err != nil {
						t.Fatal(err)
					}
				case "add":
					writeCopyFixture(t, filepath.Join(dir, "nested", "added"), "added")
				case "remove":
					if err := os.Remove(file); err != nil {
						t.Fatal(err)
					}
				case "replace":
					writeCopyFixture(t, file+".new", "replacement")
					if err := os.Rename(file+".new", file); err != nil {
						t.Fatal(err)
					}
				}
				if result := runner.Call(context.Background(), call); result.Ok || decodeToolResult(t, result)["reason"] != "copy_approval_changed" {
					t.Fatal(result.Content)
				}
				if endpoint == "source" {
					data, err := os.ReadFile(filepath.Join(target, "nested", "file"))
					if err != nil || string(data) != "original" {
						t.Fatal("destination changed despite stale source")
					}
				}
			})
		}
	}
}

func TestFileCopyFailedOverwritePreservesDestination(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			root, external := t.TempDir(), t.TempDir()
			source, target := filepath.Join(root, "source"), filepath.Join(external, "target")
			unreadable, original := source, target
			if directory {
				writeCopyFixture(t, filepath.Join(source, "a-good"), "staged first")
				unreadable = filepath.Join(source, "z-unreadable")
				original = filepath.Join(target, "keep")
			}
			writeCopyFixture(t, unreadable, "cannot read")
			writeCopyFixture(t, original, "original destination")
			if err := os.Chmod(unreadable, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })
			if f, err := os.Open(unreadable); err == nil {
				f.Close()
				t.Skip("current user can read mode-000 files")
			}
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			call := overwriteCopyCall(copyPathCall(source, target, root), directory)
			details, err := runner.ApprovalDetails(context.Background(), call)
			if err != nil {
				t.Fatal(err)
			}
			call.FileCopyGrant = FileCopyPlanFromDetails(details)
			if result := runner.Call(context.Background(), call); result.Ok {
				t.Fatal("unreadable source copied")
			}
			data, err := os.ReadFile(original)
			if err != nil || string(data) != "original destination" {
				t.Fatalf("failed copy destroyed original destination: %v %q", err, data)
			}
			entries, err := os.ReadDir(external)
			if err != nil || len(entries) != 1 || entries[0].Name() != "target" {
				t.Fatalf("staging files not cleaned up: %v %+v", err, entries)
			}
		})
	}
}
