package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type patchResultPayload struct {
	OK          bool            `json:"ok"`
	Status      string          `json:"status"`
	ProjectRoot string          `json:"projectRoot"`
	Files       []patchFileView `json:"files"`
	FileCount   int             `json:"fileCount"`
	Additions   int             `json:"additions"`
	Deletions   int             `json:"deletions"`
}

func TestPatchApplyArgumentErrorsAreSpecific(t *testing.T) {
	tests := []struct {
		name      string
		args      string
		kind      string
		field     string
		expected  string
		hasOffset bool
	}{
		{name: "empty", args: "", kind: "missing_arguments"},
		{name: "truncated", args: `{"scope":"project","files":[`, kind: "truncated_json", hasOffset: true},
		{name: "encoded string", args: `"{\"scope\":\"project\",\"files\":[]}"`, kind: "expected_object", expected: "object"},
		{name: "wrong files type", args: `{"scope":"project","files":"large patch"}`, kind: "invalid_type", field: "files", expected: "array", hasOffset: true},
		{name: "missing files", args: `{"scope":"project"}`, kind: "missing_field", field: "files", expected: "array"},
		{name: "empty files", args: `{"scope":"project","files":[]}`, kind: "empty_files", field: "files", expected: "non-empty array"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NewBuiltinRunner().Call(context.Background(), Call{
				SessionID:   "session_args",
				TurnID:      "turn_args",
				CallID:      "call_args",
				Name:        PatchApply,
				Args:        json.RawMessage(test.args),
				ProjectDirs: []string{t.TempDir()},
			})
			if result.Ok {
				t.Fatalf("invalid arguments should fail: %+v", result)
			}
			payload := decodeToolResult(t, result)
			if payload["reason"] != "invalid_arguments" || payload["errorKind"] != test.kind {
				t.Fatalf("unexpected argument error: %+v", payload)
			}
			if test.field != "" && payload["field"] != test.field {
				t.Fatalf("field=%v want %q: %+v", payload["field"], test.field, payload)
			}
			if test.expected != "" && payload["expected"] != test.expected {
				t.Fatalf("expected=%v want %q: %+v", payload["expected"], test.expected, payload)
			}
			if test.hasOffset {
				if offset, ok := payload["offset"].(float64); !ok || offset <= 0 {
					t.Fatalf("parse offset missing: %+v", payload)
				}
			}
			if payload["hint"] == "" {
				t.Fatalf("argument recovery hint missing: %+v", payload)
			}
		})
	}
}

func TestPatchApprovalDetailsFailurePreservesArgumentRecoveryData(t *testing.T) {
	runner := NewBuiltinRunner()
	call := Call{
		SessionID:   "session_args",
		TurnID:      "turn_args",
		CallID:      "call_args",
		Name:        PatchApply,
		Args:        json.RawMessage(`{"scope":"project","files":"large patch"}`),
		ProjectDirs: []string{t.TempDir()},
	}
	_, err := runner.ApprovalDetails(context.Background(), call)
	if err == nil {
		t.Fatal("invalid approval arguments should fail")
	}
	payload := decodeToolResult(t, ApprovalDetailsFailure(call, err))
	if payload["reason"] != "invalid_arguments" || payload["errorKind"] != "invalid_type" || payload["field"] != "files" || payload["expected"] != "array" || payload["hint"] == "" {
		t.Fatalf("approval argument recovery data was lost: %+v", payload)
	}
}

func TestPatchApplySizeLimitsRemainDistinctFromArgumentErrors(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner()

	tooManyFiles := make([]map[string]any, patchMaxFiles+1)
	for index := range tooManyFiles {
		tooManyFiles[index] = map[string]any{"path": "file-" + strconv.Itoa(index) + ".txt", "new_text": "content\n"}
	}
	tooMany := patchTestCall(runner, "session_size", root, PatchApply, map[string]any{
		"scope": "project",
		"files": tooManyFiles,
	})
	if tooMany.Ok {
		t.Fatalf("too many files should fail: %+v", tooMany)
	}
	tooManyPayload := decodeToolResult(t, tooMany)
	if tooManyPayload["reason"] != "too_many_files" || tooManyPayload["limit"] != float64(patchMaxFiles) {
		t.Fatalf("unexpected file-count error: %+v", tooManyPayload)
	}

	tooLarge := patchTestCall(runner, "session_size", root, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":     "large.txt",
			"new_text": strings.Repeat("x", patchMaxFileBytes+1),
		}},
	})
	if tooLarge.Ok || !strings.Contains(tooLarge.Content, `"reason":"file_too_large"`) {
		t.Fatalf("large file content should report its size limit: %+v", tooLarge)
	}
}

func TestPatchApplyUsesApprovalSnapshotAndWritesAtomicBatch(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "update.txt"), "old line\n")
	writePatchTestFile(t, filepath.Join(root, "delete.txt"), "remove me\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": "update.txt", "new_text": "new line\n"},
			{"path": "nested/create.txt", "new_text": "created\n"},
			{"path": "delete.txt", "delete": true},
		},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_batch", Name: PatchApply, Args: args, ProjectDirs: []string{root}}
	details, err := runner.ApprovalDetails(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	diff, _ := details["diff"].(string)
	if details["fileCount"] != 3 || details["destructive"] != true || !strings.Contains(diff, "-old line") || !strings.Contains(diff, "+new line") {
		t.Fatalf("approval details must carry the exact review diff: %+v", details)
	}
	if !strings.Contains(diff, "--- /dev/null") || !strings.Contains(diff, "+++ b/nested/create.txt") || !strings.Contains(diff, "+++ /dev/null") {
		t.Fatalf("create or delete diff missing: %s", diff)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "update.txt")); got != "old line\n" {
		t.Fatalf("approval preview changed update target: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "create.txt")); !os.IsNotExist(err) {
		t.Fatalf("approval preview created a file: %v", err)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "delete.txt")); got != "remove me\n" {
		t.Fatalf("approval preview deleted content: %q", got)
	}
	paths, _ := details["paths"].([]string)
	if len(paths) != 3 {
		t.Fatalf("approval paths missing: %+v", details["paths"])
	}

	applied := runner.Call(context.Background(), call)
	appliedPayload := decodePatchPayload(t, applied)
	if !applied.Ok || appliedPayload.Status != "applied" || appliedPayload.FileCount != 3 {
		t.Fatalf("unexpected apply result: result=%+v payload=%+v", applied, appliedPayload)
	}
	if strings.Contains(applied.Content, `"diff"`) || strings.Contains(applied.Content, "proposalID") {
		t.Fatalf("apply result should rely on Turn Diff instead of returning proposal state: %s", applied.Content)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "update.txt")); got != "new line\n" {
		t.Fatalf("update not applied: %q", got)
	}
	if got := readPatchTestFile(t, filepath.Join(root, "nested", "create.txt")); got != "created\n" {
		t.Fatalf("create not applied: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete not applied: %v", err)
	}
	assertNoPatchTempFiles(t, root)
}

func TestPatchApplySupportsOrderedEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	original := "package main\n\nfunc message() string {\n\treturn \"old\"\n}\n"
	writePatchTestFile(t, path, original)
	runner := NewBuiltinRunner()
	applied := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path": "main.go",
			"edits": []map[string]any{
				{"old_text": "func message() string", "new_text": "func greeting() string"},
				{"old_text": "return \"old\"", "new_text": "return \"new\""},
			},
		}},
	})
	if !applied.Ok {
		t.Fatalf("ordered edit apply failed: %+v", applied)
	}
	if got := readPatchTestFile(t, path); !strings.Contains(got, "func greeting() string") || !strings.Contains(got, "return \"new\"") {
		t.Fatalf("ordered edits not applied: %q", got)
	}
}

func TestPatchApplyRejectsAmbiguousOrConflictingEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "same\nsame\n")
	runner := NewBuiltinRunner()

	ambiguous := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":  "notes.txt",
			"edits": []map[string]any{{"old_text": "same", "new_text": "changed"}},
		}},
	})
	if ambiguous.Ok || !strings.Contains(ambiguous.Content, `"reason":"edit_text_ambiguous"`) {
		t.Fatalf("ambiguous edit should fail: %+v", ambiguous)
	}

	conflicting := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path": "notes.txt",
			"edits": []map[string]any{
				{"old_text": "same", "new_text": "changed", "replace_all": true},
				{"old_text": "missing", "new_text": "never"},
			},
		}},
	})
	if conflicting.Ok || !strings.Contains(conflicting.Content, `"reason":"edit_text_not_found"`) {
		t.Fatalf("conflicting edit should fail: %+v", conflicting)
	}
	if got := readPatchTestFile(t, path); got != "same\nsame\n" {
		t.Fatalf("failed edits changed worktree: %q", got)
	}
}

func TestPatchApplyRequiresOneFileOperation(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "old\n")
	runner := NewBuiltinRunner()
	result := patchTestCall(runner, "session_a", root, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":     "notes.txt",
			"new_text": "new\n",
			"edits":    []map[string]any{{"old_text": "old", "new_text": "new"}},
		}},
	})
	if result.Ok || !strings.Contains(result.Content, `"reason":"invalid_arguments"`) {
		t.Fatalf("multiple file operations should fail: %+v", result)
	}
}

func TestPatchApplyRejectsDriftWithoutPartialWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	writePatchTestFile(t, first, "first old\n")
	writePatchTestFile(t, second, "second old\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": "first.txt", "new_text": "first new\n"},
			{"path": "second.txt", "new_text": "second new\n"},
		},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_drift", Name: PatchApply, Args: args, ProjectDirs: []string{root}}
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	writePatchTestFile(t, second, "external edit\n")

	apply := runner.Call(context.Background(), call)
	if apply.Ok || !strings.Contains(apply.Content, `"reason":"patch_stale"`) {
		t.Fatalf("stale approved patch should fail: %+v", apply)
	}
	if got := readPatchTestFile(t, first); got != "first old\n" {
		t.Fatalf("first file was partially applied: %q", got)
	}
	if got := readPatchTestFile(t, second); got != "external edit\n" {
		t.Fatalf("external edit was overwritten: %q", got)
	}
	assertNoPatchTempFiles(t, root)
}

func TestPatchApplyFailsClosedWithoutApprovalSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "new_text": "approved\n"}},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_missing", Name: PatchApply, Args: args, ProjectDirs: []string{root}}
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	runner.patchMu.Lock()
	delete(runner.preparedPatches, preparedPatchKey(call))
	runner.patchMu.Unlock()
	writePatchTestFile(t, path, "external\n")

	result := runner.Call(context.Background(), call)
	if result.Ok || !strings.Contains(result.Content, `"reason":"patch_not_prepared"`) {
		t.Fatalf("missing approval snapshot should fail closed: %+v", result)
	}
	if got := readPatchTestFile(t, path); got != "external\n" {
		t.Fatalf("missing approval snapshot changed the file: %q", got)
	}
}

func TestPatchApplyRejectsChangedArgumentsAfterApproval(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	approvedArgs, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "new_text": "approved\n"}},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_changed", Name: PatchApply, Args: approvedArgs, ProjectDirs: []string{root}}
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	call.Args, _ = json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "new_text": "different\n"}},
	})

	result := runner.Call(context.Background(), call)
	if result.Ok || !strings.Contains(result.Content, `"reason":"patch_arguments_changed"`) {
		t.Fatalf("changed arguments should invalidate approval: %+v", result)
	}
	if got := readPatchTestFile(t, path); got != "old\n" {
		t.Fatalf("changed arguments modified the file: %q", got)
	}
}

func TestPatchApplyRejectsExpiredApprovalSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "new_text": "new\n"}},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_expired", Name: PatchApply, Args: args, ProjectDirs: []string{root}}
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	runner.patchMu.Lock()
	runner.preparedPatches[preparedPatchKey(call)].ExpiresAt = time.Now().Add(-time.Second)
	runner.patchMu.Unlock()

	result := runner.Call(context.Background(), call)
	if result.Ok || !strings.Contains(result.Content, `"reason":"patch_approval_expired"`) {
		t.Fatalf("expired approval snapshot should fail closed: %+v", result)
	}
	if got := readPatchTestFile(t, path); got != "old\n" {
		t.Fatalf("expired approval snapshot modified the file: %q", got)
	}
}

func TestRollbackPatchItemsRestoresWorktree(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing.txt")
	backup := filepath.Join(root, ".pudding-patch-backup-test")
	createdDir := filepath.Join(root, "nested")
	created := filepath.Join(createdDir, "created.txt")
	staged := filepath.Join(root, ".pudding-patch-stage-test")
	writePatchTestFile(t, existing, "new content\n")
	writePatchTestFile(t, backup, "old content\n")
	writePatchTestFile(t, created, "created content\n")
	writePatchTestFile(t, staged, "staged content\n")

	items := []patchApplyItem{
		{file: preparedPatchFile{Target: existing}, backupPath: backup, installed: true},
		{file: preparedPatchFile{Target: created}, installed: true},
		{tempPath: staged},
	}
	if err := rollbackPatchItems(items, map[string]bool{createdDir: true}); err != nil {
		t.Fatal(err)
	}
	if got := readPatchTestFile(t, existing); got != "old content\n" {
		t.Fatalf("existing file was not restored: %q", got)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file survived rollback: %v", err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged file survived rollback: %v", err)
	}
	if _, err := os.Stat(createdDir); !os.IsNotExist(err) {
		t.Fatalf("created directory survived rollback: %v", err)
	}
	assertNoPatchTempFiles(t, root)
}

func TestPatchApplyRejectsCrossRootAndNoop(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	first := filepath.Join(firstRoot, "first.txt")
	second := filepath.Join(secondRoot, "second.txt")
	writePatchTestFile(t, first, "same\n")
	writePatchTestFile(t, second, "old\n")
	runner := NewBuiltinRunner()

	noop := patchTestCallWithRoots(runner, "session_a", []string{firstRoot}, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "first.txt", "new_text": "same\n"}},
	})
	if noop.Ok || !strings.Contains(noop.Content, `"reason":"no_changes"`) {
		t.Fatalf("no-op patch should fail: %+v", noop)
	}

	crossRoot := patchTestCallWithRoots(runner, "session_a", []string{firstRoot, secondRoot}, PatchApply, map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": first, "new_text": "first new\n"},
			{"path": second, "new_text": "second new\n"},
		},
	})
	if crossRoot.Ok || !strings.Contains(crossRoot.Content, `"reason":"cross_root_patch"`) {
		t.Fatalf("cross-root patch should fail: %+v", crossRoot)
	}
}

func patchTestCall(runner *BuiltinRunner, sessionID, root, name string, args map[string]any) Result {
	return patchTestCallWithRoots(runner, sessionID, []string{root}, name, args)
}

func patchTestCallWithRoots(runner *BuiltinRunner, sessionID string, roots []string, name string, args map[string]any) Result {
	raw, _ := json.Marshal(args)
	call := Call{
		SessionID:   sessionID,
		TurnID:      "turn_patch",
		CallID:      "call_patch",
		Name:        name,
		Args:        raw,
		ProjectDirs: roots,
	}
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		return ApprovalDetailsFailure(call, err)
	}
	return runner.Call(context.Background(), call)
}

func decodePatchPayload(t *testing.T, result Result) patchResultPayload {
	t.Helper()
	var payload patchResultPayload
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode patch result: %v content=%q", err, result.Content)
	}
	return payload
}

func writePatchTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readPatchTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoPatchTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".pudding-patch-") {
			t.Errorf("patch temp file was not cleaned: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
