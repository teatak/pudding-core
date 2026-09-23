package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectPathContractRejectsRelativeMultiRootTargetsWithoutWrites(t *testing.T) {
	first, second := projectContractDir(t), projectContractDir(t)
	secondFile := filepath.Join(second, "same.txt")
	if err := os.WriteFile(secondFile, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	tests := []struct {
		name string
		args map[string]any
	}{
		{FileRead, map[string]any{"scope": " project ", "path": "same.txt"}},
		{FileWrite, map[string]any{"scope": "project", "path": "same.txt", "content": "changed"}},
		{FileStat, map[string]any{"scope": "project", "path": "same.txt"}},
		{FileSearch, map[string]any{"scope": "project", "path": ".", "query": "second"}},
		{FileSlice, map[string]any{"scope": "project", "path": "same.txt"}},
		{FileDelete, map[string]any{"scope": "project", "path": "same.txt"}},
		{FileMove, map[string]any{"scope": "project", "from_path": secondFile, "to_path": "moved.txt"}},
		{FileCopy, map[string]any{"from": map[string]any{"scope": "project", "path": secondFile}, "to": map[string]any{"scope": "project", "path": "copy.txt"}}},
		{FilePatch, map[string]any{"scope": "project", "files": []map[string]any{{"path": "same.txt", "action": "replace", "content": "changed"}}}},
		{CommandRun, map[string]any{"scope": "project", "command": "echo changed > same.txt"}},
		{GitStatus, map[string]any{"scope": "project"}},
		{GitStage, map[string]any{"scope": "project", "paths": []string{"same.txt"}}},
		{CodeSymbols, map[string]any{"scope": "project", "path": ".", "query": "main"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := projectContractCall(test.name, []string{first, second}, test.args)
			var result Result
			if test.name == FilePatch {
				_, err := runner.ApprovalDetails(context.Background(), call)
				if err == nil {
					t.Fatal("ambiguous patch was prepared for approval")
				}
				result = ApprovalDetailsFailure(call, err)
			} else {
				result = runner.Call(context.Background(), call)
			}
			if result.Ok || decodeToolResult(t, result)["reason"] != "absolute_path_required" {
				t.Fatalf("ambiguous target was not rejected: %+v", result)
			}
			payload := decodeToolResult(t, result)
			roots, hasRoots := payload["projectRoots"].([]any)
			if !hasRoots || len(roots) != 2 || roots[0] != first || roots[1] != second || payload["hint"] == "" {
				t.Fatalf("ambiguous target lost actionable path details: %+v", payload)
			}
			if tracking, ok := MutationTrackingForCall(call); ok && len(tracking.Targets) > 0 {
				t.Fatalf("rejected call acquired mutation targets: %+v", tracking)
			}
		})
	}
	if data, err := os.ReadFile(secondFile); err != nil || string(data) != "second" {
		t.Fatalf("source changed after rejected calls: data=%q err=%v", data, err)
	}
	if entries, err := os.ReadDir(first); err != nil || len(entries) != 0 {
		t.Fatalf("rejected calls wrote into the first root: entries=%v err=%v", entries, err)
	}
	write := runner.Call(context.Background(), projectContractCall(FileWrite, []string{first, second}, map[string]any{"scope": " project ", "path": secondFile, "content": "explicit second"}))
	if !write.Ok || decodeToolResult(t, write)["scope"] != "project" {
		t.Fatalf("explicit second-root write failed: %+v", write)
	}
	read := runner.Call(context.Background(), projectContractCall(FileRead, []string{first, second}, map[string]any{"scope": "project", "path": secondFile}))
	if !read.Ok || decodeToolResult(t, read)["content"] != "explicit second" {
		t.Fatalf("read and write did not select the same target: %+v", read)
	}
	list := runner.Call(context.Background(), projectContractCall(FileList, []string{first, second}, map[string]any{"scope": " project ", "path": "."}))
	if !list.Ok || decodeToolResult(t, list)["rootCount"] != float64(2) {
		t.Fatalf("root enumeration must remain available: %+v", list)
	}
}

func TestProjectPathContractSeparatesScopeExistenceAndAuthorization(t *testing.T) {
	root, outside := projectContractDir(t), projectContractDir(t)
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	for _, test := range []struct{ scope, path, reason string }{
		{"invalid", "missing.txt", "invalid_scope"},
		{" project ", "missing.txt", "path_not_found"},
		{"project", filepath.Join(outside, "missing.txt"), "path_not_authorized"},
	} {
		result := runner.Call(context.Background(), projectContractCall(FileRead, []string{root}, map[string]any{"scope": test.scope, "path": test.path}))
		if result.Ok || decodeToolResult(t, result)["reason"] != test.reason {
			t.Fatalf("scope=%q path=%q: %+v", test.scope, test.path, result)
		}
	}
	stat := runner.Call(context.Background(), projectContractCall(FileStat, []string{root}, map[string]any{"scope": " project ", "path": "new/missing.txt"}))
	if !stat.Ok || decodeToolResult(t, stat)["exists"] != false {
		t.Fatalf("stat of a missing authorized path: %+v", stat)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	write := runner.Call(context.Background(), projectContractCall(FileWrite, []string{root}, map[string]any{"scope": "project", "path": "escape/new.txt", "content": "must not escape"}))
	if write.Ok || decodeToolResult(t, write)["reason"] != "path_not_authorized" {
		t.Fatalf("symlink escape was not rejected: %+v", write)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file was created: %v", err)
	}
}

func TestProjectPathContractCommandApprovalExecutionAndTrackingAgree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("command fixture uses POSIX shell syntax")
	}
	first, second := projectContractDir(t), projectContractDir(t)
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	args := map[string]any{"scope": "project", "command": "printf second > target.txt"}
	call := projectContractCall(CommandRun, []string{first, second}, args)
	if _, err := commandApprovalDetails(call); !errors.Is(err, errProjectAbsolutePathRequired) {
		t.Fatalf("pre-approval did not reject omitted multi-root cwd: %v", err)
	} else if result := ApprovalDetailsFailure(call, err); decodeToolResult(t, result)["reason"] != "absolute_path_required" {
		t.Fatalf("pre-approval changed path failure: %+v", result)
	}
	args["cwd"] = second
	call = projectContractCall(CommandRun, []string{first, second}, args)
	details, err := commandApprovalDetails(call)
	if err != nil || details["cwd"] != second {
		t.Fatalf("pre-approval cwd=%v err=%v", details, err)
	}
	tracking, ok := MutationTrackingForCall(call)
	if !ok || len(tracking.Targets) != 1 || tracking.Targets[0] != filepath.Join(second, "target.txt") {
		t.Fatalf("command tracking chose a different target: %+v", tracking)
	}
	result := runner.Call(context.Background(), call)
	if !result.Ok || decodeToolResult(t, result)["exitCode"] != float64(0) {
		t.Fatalf("explicit-cwd command failed: %+v", result)
	}
	if data, err := os.ReadFile(tracking.Targets[0]); err != nil || string(data) != "second" {
		t.Fatalf("command did not write its tracked target: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(first, "target.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command wrote to the first root: %v", err)
	}
}

func TestProjectPathContractGitPathsRemainRepositoryRelative(t *testing.T) {
	first := newGitTestRepository(t, false)
	second := newGitTestRepository(t, false)
	if err := os.WriteFile(filepath.Join(second, "picked.txt"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	call := projectContractCall(GitStage, []string{first, second}, map[string]any{"scope": " project ", "cwd": second, "paths": []string{"picked.txt"}})
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatalf("explicit Git cwd pre-approval: %v", err)
	}
	result := runner.Call(context.Background(), call)
	if !result.Ok {
		t.Fatalf("repo-relative Git path was rejected: %+v", result)
	}
	staged := runGit(context.Background(), second, 4096, "diff", "--cached", "--name-only")
	if staged.err != nil || strings.TrimSpace(staged.stdout.String()) != "picked.txt" {
		t.Fatalf("wrong Git stage result: %q err=%v", staged.stdout.String(), staged.err)
	}
	firstStaged := runGit(context.Background(), first, 4096, "diff", "--cached", "--name-only")
	if firstStaged.err != nil || strings.TrimSpace(firstStaged.stdout.String()) != "" {
		t.Fatalf("first repository index changed: %q err=%v", firstStaged.stdout.String(), firstStaged.err)
	}
}

func TestProjectPathContractCommandRejectsFileCWDConsistently(t *testing.T) {
	root := projectContractDir(t)
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	for _, background := range []bool{false, true} {
		call := projectContractCall(CommandRun, []string{root}, map[string]any{"scope": "project", "cwd": file, "command": "printf changed > target.txt", "background": background})
		_, err := runner.ApprovalDetails(context.Background(), call)
		if !errors.Is(err, errCommandCWDNotDirectory) {
			t.Fatalf("background=%v: approval accepted a file cwd: %v", background, err)
		}
		for _, result := range []Result{ApprovalDetailsFailure(call, err), runner.Call(context.Background(), call)} {
			if result.Ok || decodeToolResult(t, result)["reason"] != "cwd_not_directory" {
				t.Fatalf("background=%v: inconsistent cwd failure: %+v", background, result)
			}
		}
		if tracking, ok := MutationTrackingForCall(call); ok && len(tracking.Targets) != 0 {
			t.Fatalf("invalid cwd acquired mutation targets: %+v", tracking)
		}
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
		t.Fatalf("invalid cwd call changed the root: entries=%v err=%v", entries, err)
	}
}

func TestProjectPathContractCommandPreapprovalKeepsArgumentFailures(t *testing.T) {
	for _, test := range []struct {
		scope, command, reason string
	}{
		{"temp", "true", "invalid_scope"},
		{"project", "", "invalid_arguments"},
	} {
		call := projectContractCall(CommandRun, []string{projectContractDir(t)}, map[string]any{"scope": test.scope, "command": test.command})
		_, err := commandApprovalDetails(call)
		if err == nil {
			t.Fatal("invalid command was prepared for approval")
		}
		result := ApprovalDetailsFailure(call, err)
		if result.Ok || decodeToolResult(t, result)["reason"] != test.reason {
			t.Fatalf("invalid command became a path error: %+v", result)
		}
	}
}

func TestProjectPathContractManagedCommandOperandsDoNotSelectCWD(t *testing.T) {
	root, artifacts := projectContractDir(t), projectContractDir(t)
	artifact := filepath.Join(artifacts, "result.txt")
	if err := os.WriteFile(artifact, []byte("result"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := projectContractCall(CommandRun, []string{root}, map[string]any{"scope": "project", "command": joinShellCommand([]string{"cat", artifact})})
	risk, ok := ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs, artifacts)
	if !ok || !risk.LowRisk || len(risk.requiredProjectPaths) != 0 {
		t.Fatalf("current-session artifact was not an allowed operand: %+v", risk)
	}
	withoutArtifacts, ok := ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs)
	if !ok || len(withoutArtifacts.requiredProjectPaths) == 0 {
		t.Fatalf("unregistered artifact directory was accepted: %+v", withoutArtifacts)
	}
	outsideCWD := projectContractCall(CommandRun, []string{root}, map[string]any{"scope": "project", "cwd": artifacts, "command": "cat result.txt"})
	if _, err := commandApprovalDetails(outsideCWD); !errors.Is(err, errProjectPathNotAllowed) {
		t.Fatalf("artifact directory selected a project cwd: %v", err)
	}
	if commandExecutableAllowedForAuto(artifact, root, []string{root}) {
		t.Fatal("artifact operand authorization also authorized an executable")
	}
	for _, operation := range []string{"cd", "pushd"} {
		command := joinShellCommand([]string{operation, artifacts}) + " && ./run.sh"
		call := projectContractCall(CommandRun, []string{root}, map[string]any{"scope": "project", "command": command})
		risk, ok := ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs, artifacts)
		if !ok || risk.LowRisk || len(risk.requiredProjectPaths) != 1 || risk.requiredProjectPaths[0] != artifacts {
			t.Fatalf("%s artifact directory was accepted as cwd: %+v", operation, risk)
		}
	}
	if err := os.Symlink(artifacts, filepath.Join(root, "artifact-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, operation := range []string{"cd", "pushd"} {
		call := projectContractCall(CommandRun, []string{root}, map[string]any{"scope": "project", "command": operation + " artifact-link && ./run.sh"})
		risk, ok := ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs, artifacts)
		if !ok || risk.LowRisk || len(risk.requiredProjectPaths) == 0 {
			t.Fatalf("%s artifact symlink was accepted as cwd: %+v", operation, risk)
		}
	}
}

func projectContractCall(name string, roots []string, args map[string]any) Call {
	raw, _ := json.Marshal(args)
	return Call{SessionID: "project_path_contract", CallID: "contract", Name: name, Args: raw, ProjectDirs: roots}
}

func projectContractDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
