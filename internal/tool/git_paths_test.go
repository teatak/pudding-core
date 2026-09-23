package tool

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitRepositoryAuthorizationDoesNotDependOnNestedRootOrder(t *testing.T) {
	root := newGitTestRepository(t, false)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "sub")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	for _, tc := range []struct {
		name    string
		roots   []string
		allowed bool
	}{
		{"nested_first", []string{nested, root}, true},
		{"parent_first", []string{root, nested}, true},
		{"only_nested", []string{nested}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statusCall := projectContractCall(GitStatus, tc.roots, map[string]any{"scope": "project", "cwd": nested})
			status := runner.Call(context.Background(), statusCall)
			stageCall := projectContractCall(GitStage, tc.roots, map[string]any{"scope": "project", "cwd": nested, "paths": []string{"note.txt"}})
			details, approvalErr := runner.ApprovalDetails(context.Background(), stageCall)
			stage := runner.Call(context.Background(), stageCall)
			if !tc.allowed {
				if status.Ok || stage.Ok || approvalErr == nil {
					t.Fatalf("subdirectory grant authorized its parent repository: status=%+v stage=%+v approval=%v", status, stage, approvalErr)
				}
				for _, result := range []Result{status, stage, ApprovalDetailsFailure(stageCall, approvalErr)} {
					if decodeToolResult(t, result)["reason"] != "repository_outside_project" {
						t.Fatalf("unexpected parent repository rejection: %+v", result)
					}
				}
				return
			}
			if !status.Ok || !stage.Ok || approvalErr != nil {
				t.Fatalf("authorized repository rejected: status=%+v stage=%+v approval=%v", status, stage, approvalErr)
			}
			if details["repoRoot"] != canonicalRoot || decodeToolResult(t, status)["repoRoot"] != canonicalRoot {
				t.Fatalf("approval and execution chose different repositories: details=%v status=%s", details, status.Content)
			}
		})
	}
}

func TestGitAbsoluteAliasOperandsPreserveGitEntries(t *testing.T) {
	for _, aliasKind := range []string{"root", "ancestor"} {
		t.Run(aliasKind, func(t *testing.T) {
			root := newGitTestRepository(t, true)
			canonicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			linkTarget := canonicalRoot
			if aliasKind == "ancestor" {
				linkTarget = filepath.Dir(canonicalRoot)
			}
			if err := os.Symlink(linkTarget, alias); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if aliasKind == "ancestor" {
				alias = filepath.Join(alias, filepath.Base(canonicalRoot))
			}
			deleted := filepath.Join(root, "deleted", "gone.txt")
			if err := os.MkdirAll(filepath.Dir(deleted), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(deleted, []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGitTest(t, root, "add", "deleted/gone.txt")
			runGitTest(t, root, "commit", "-m", "file to delete")
			if err := os.RemoveAll(filepath.Dir(deleted)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "existing"), []byte("must not stage this content"), 0o600); err != nil {
				t.Fatal(err)
			}
			links := map[string]string{"existing-link": filepath.Join(outside, "existing"), "dangling-link": filepath.Join(outside, "missing")}
			for name, target := range links {
				if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			runner := NewBuiltinRunner()
			t.Cleanup(func() { _ = runner.Close() })
			paths := []string{"note.txt", filepath.Join(alias, "note.txt"), filepath.Join(alias, "existing-link"), filepath.Join(alias, "dangling-link"), filepath.Join(alias, "deleted", "gone.txt")}
			wantPaths := []string{"dangling-link", "deleted/gone.txt", "existing-link", "note.txt"}
			for _, name := range []string{GitStage, GitUnstage} {
				call := projectContractCall(name, []string{alias}, map[string]any{"scope": "project", "cwd": alias, "paths": paths})
				details, err := runner.ApprovalDetails(context.Background(), call)
				if err != nil {
					t.Fatalf("%s alias pre-approval: %v", name, err)
				}
				if !reflect.DeepEqual(details["paths"], wantPaths) {
					t.Fatalf("%s approval paths = %#v, want %#v", name, details["paths"], wantPaths)
				}
				result := runner.Call(context.Background(), call)
				if !result.Ok {
					t.Fatalf("%s alias execution: %+v", name, result)
				}
				payload := decodeGitPayload[gitWritePayload](t, result)
				if !reflect.DeepEqual(payload.Paths, wantPaths) {
					t.Fatalf("%s execution paths = %#v, want %#v", name, payload.Paths, wantPaths)
				}
				if name == GitStage {
					for link, target := range links {
						entry := runGit(context.Background(), canonicalRoot, 4096, "ls-files", "--stage", "--", link)
						blob := runGit(context.Background(), canonicalRoot, 4096, "show", ":"+link)
						if entry.err != nil || !strings.HasPrefix(entry.stdout.String(), "120000 ") || blob.err != nil || blob.stdout.String() != target {
							t.Fatalf("final symlink was followed: entry=%q blob=%q errs=%v/%v", entry.stdout.String(), blob.stdout.String(), entry.err, blob.err)
						}
					}
					gone := runGit(context.Background(), canonicalRoot, 4096, "ls-files", "--", "deleted/gone.txt")
					if gone.err != nil || gone.stdout.String() != "" {
						t.Fatalf("missing parent prevented staging deletion: %q %v", gone.stdout.String(), gone.err)
					}
				}
			}
			staged := runGit(context.Background(), canonicalRoot, 4096, "diff", "--cached", "--name-only")
			if staged.err != nil || staged.stdout.String() != "" {
				t.Fatalf("alias unstage did not restore the index: %q %v", staged.stdout.String(), staged.err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(outside, "existing"), filepath.Join(alias, "escape", "existing"), filepath.Join(alias, "escape", "missing")} {
				call := projectContractCall(GitStage, []string{alias}, map[string]any{"scope": "project", "cwd": alias, "paths": []string{path}})
				_, err := runner.ApprovalDetails(context.Background(), call)
				result := runner.Call(context.Background(), call)
				if err == nil || result.Ok || decodeToolResult(t, result)["reason"] != "path_not_authorized" {
					t.Fatalf("escaping parent accepted: path=%q approval=%v result=%+v", path, err, result)
				}
			}
		})
	}
}

func TestGitMetadataAuthorizationUsesAllProjectRoots(t *testing.T) {
	for _, layout := range []string{"separate_git_dir", "linked_worktree"} {
		t.Run(layout, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "work")
			metadata := filepath.Join(parent, "metadata")
			if layout == "separate_git_dir" {
				if err := os.Mkdir(root, 0o700); err != nil {
					t.Fatal(err)
				}
				runGitTest(t, root, "init", "--separate-git-dir="+metadata)
			} else {
				mainRoot := newGitTestRepository(t, true)
				if err := os.Rename(mainRoot, metadata); err != nil {
					t.Fatal(err)
				}
				runGitTest(t, metadata, "worktree", "add", "-b", "metadata-boundary", root)
			}
			nested := filepath.Join(root, "sub")
			if err := os.Mkdir(nested, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner()
			t.Cleanup(func() { _ = runner.Close() })
			for _, tc := range []struct {
				name   string
				roots  []string
				reason string
			}{
				{"missing_metadata_grant", []string{root}, "git_metadata_outside_project"},
				{"missing_repository_grant", []string{nested, metadata}, "repository_outside_project"},
				{"narrow_first", []string{root, parent}, ""},
				{"parent_first", []string{parent, root}, ""},
				{"separate_grants", []string{root, metadata}, ""},
				{"reversed_separate_grants", []string{metadata, root}, ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					call := projectContractCall(GitStage, tc.roots, map[string]any{"scope": "project", "cwd": nested, "paths": []string{"note.txt"}})
					_, err := runner.ApprovalDetails(context.Background(), call)
					result := runner.Call(context.Background(), call)
					if tc.reason == "" {
						if err != nil || !result.Ok {
							t.Fatalf("authorized metadata rejected: approval=%v result=%+v", err, result)
						}
						return
					}
					if err == nil || result.Ok {
						t.Fatalf("missing grant accepted: approval=%v result=%+v", err, result)
					}
					for _, failed := range []Result{result, ApprovalDetailsFailure(call, err)} {
						if decodeToolResult(t, failed)["reason"] != tc.reason {
							t.Fatalf("missing grant reason: got %+v, want %s", failed, tc.reason)
						}
					}
				})
			}
		})
	}
}
