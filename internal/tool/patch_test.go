package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func TestFilePatchArgumentErrorsAreSpecific(t *testing.T) {
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
		{name: "null files", args: `{"scope":"project","files":null}`, kind: "invalid_type", field: "files", expected: "array", hasOffset: true},
		{name: "null file", args: `{"scope":"project","files":[null]}`, kind: "invalid_type", field: "files[0]", expected: "object", hasOffset: true},
		{name: "missing files", args: `{"scope":"project"}`, kind: "missing_field", field: "files", expected: "array"},
		{name: "empty files", args: `{"scope":"project","files":[]}`, kind: "empty_files", field: "files", expected: "non-empty array"},
		{name: "missing path", args: `{"scope":"project","files":[{"action":"delete"}]}`, kind: "path_required", field: "files[0].path", expected: "non-empty string"},
		{name: "removed base revision", args: `{"scope":"project","files":[{"path":"notes.txt","action":"delete","base_revision":"sha256:abc123"}]}`, kind: "unknown_field"},
		{name: "invalid action", args: `{"scope":"project","files":[{"path":"notes.txt","action":"write"}]}`, kind: "invalid_action", field: "files[0].action", expected: "create, replace, edit, or delete"},
		{name: "missing action", args: `{"scope":"project","files":[{"path":"notes.txt"}]}`, kind: "invalid_action", field: "files[0].action", expected: "create, replace, edit, or delete"},
		{name: "null content", args: `{"scope":"project","files":[{"path":"notes.txt","action":"delete","content":null}]}`, kind: "invalid_type", field: "files[0].content", expected: "string", hasOffset: true},
		{name: "null hunks", args: `{"scope":"project","files":[{"path":"notes.txt","action":"delete","hunks":null}]}`, kind: "invalid_type", field: "files[0].hunks", expected: "array", hasOffset: true},
		{name: "null hunk", args: `{"scope":"project","files":[{"path":"notes.txt","action":"edit","hunks":[null]}]}`, kind: "invalid_type", field: "files[0].hunks[0]", expected: "object", hasOffset: true},
		{name: "invalid relocate type", args: `{"scope":"project","files":[{"path":"notes.txt","action":"edit","hunks":[{"start_line":1,"old_lines":["old"],"new_lines":["new"],"relocate":"true"}]}]}`, kind: "invalid_type", field: "files[0].hunks[0].relocate", expected: "boolean", hasOffset: true},
		{name: "missing hunk line", args: `{"scope":"project","files":[{"path":"notes.txt","action":"edit","hunks":[{"old_lines":["old"],"new_lines":["new"]}]}]}`, kind: "hunk_line_invalid", field: "files[0].hunks[0].start_line", expected: "integer >= 1"},
		{name: "missing hunk input", args: `{"scope":"project","files":[{"path":"notes.txt","action":"edit","hunks":[{"start_line":1,"new_lines":["new"]}]}]}`, kind: "hunk_old_lines_required", field: "files[0].hunks[0].old_lines", expected: "array"},
		{name: "missing hunk output", args: `{"scope":"project","files":[{"path":"notes.txt","action":"edit","hunks":[{"start_line":1,"old_lines":["old"]}]}]}`, kind: "hunk_new_lines_required", field: "files[0].hunks[0].new_lines", expected: "array"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NewBuiltinRunner().Call(context.Background(), Call{
				SessionID:   "session_args",
				TurnID:      "turn_args",
				CallID:      "call_args",
				Name:        FilePatch,
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
			if payload["receivedBytes"] != float64(len(test.args)) {
				t.Fatalf("input byte count missing: %+v", payload)
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
		Name:        FilePatch,
		Args:        json.RawMessage(`{"scope":"project","files":"large patch"}`),
		ProjectDirs: []string{t.TempDir()},
	}
	_, err := runner.ApprovalDetails(context.Background(), call)
	if err == nil {
		t.Fatal("invalid approval arguments should fail")
	}
	payload := decodeToolResult(t, ApprovalDetailsFailure(call, err))
	if payload["reason"] != "invalid_arguments" || payload["errorKind"] != "invalid_type" || payload["field"] != "files" || payload["expected"] != "array" || payload["hint"] == "" || payload["receivedBytes"] != float64(len(call.Args)) {
		t.Fatalf("approval argument recovery data was lost: %+v", payload)
	}
}

func TestFilePatchSizeLimitsRemainDistinctFromArgumentErrors(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner()

	tooManyFiles := make([]map[string]any, patchMaxFiles+1)
	for index := range tooManyFiles {
		tooManyFiles[index] = map[string]any{"path": "file-" + strconv.Itoa(index) + ".txt", "action": "create", "content": "content\n"}
	}
	tooMany := patchTestCall(runner, "session_size", root, FilePatch, map[string]any{
		"scope": "project",
		"files": tooManyFiles,
	})
	if tooMany.Ok {
		t.Fatalf("too many files should fail: %+v", tooMany)
	}
	tooManyPayload := decodeToolResult(t, tooMany)
	if tooManyPayload["reason"] != "too_many_files" || tooManyPayload["limit"] != float64(patchMaxFiles) || tooManyPayload["metric"] != "files" || tooManyPayload["unit"] != "files" || tooManyPayload["actual"] != float64(patchMaxFiles+1) || tooManyPayload["count"] != float64(patchMaxFiles+1) {
		t.Fatalf("unexpected file-count error: %+v", tooManyPayload)
	}

	tooLarge := patchTestCall(runner, "session_size", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":    "large.txt",
			"action":  "create",
			"content": strings.Repeat("x", patchMaxFileBytes+1),
		}},
	})
	if tooLarge.Ok || !strings.Contains(tooLarge.Content, `"reason":"file_too_large"`) {
		t.Fatalf("large file content should report its size limit: %+v", tooLarge)
	}
	tooLargePayload := decodeToolResult(t, tooLarge)
	if tooLargePayload["metric"] != "destination_file_bytes" || tooLargePayload["unit"] != "bytes" || tooLargePayload["actual"] != float64(patchMaxFileBytes+1) || tooLargePayload["limit"] != float64(patchMaxFileBytes) || tooLargePayload["errorKind"] != nil {
		t.Fatalf("file size and JSON errors were conflated: %s", tooLarge.Content)
	}
}

func TestFilePatchResourceDiagnosticsLeaveFilesUnchanged(t *testing.T) {
	for _, test := range []struct {
		name, reason, metric, unit string
		limit, wantActual          int
		setup                      func(*testing.T, string) []map[string]any
	}{
		{
			name: "source file", reason: "file_too_large", metric: "source_file_bytes", unit: "bytes", limit: patchMaxFileBytes, wantActual: patchMaxFileBytes + 1,
			setup: func(t *testing.T, root string) []map[string]any {
				writePatchTestFile(t, filepath.Join(root, "notes.txt"), strings.Repeat("x", patchMaxFileBytes+1))
				return []map[string]any{{"path": "notes.txt", "action": "delete"}}
			},
		},
		{
			name: "edited file", reason: "file_too_large", metric: "destination_file_bytes", unit: "bytes", limit: patchMaxFileBytes, wantActual: patchMaxFileBytes + 6,
			setup: func(t *testing.T, root string) []map[string]any {
				writePatchTestFile(t, filepath.Join(root, "notes.txt"), "old\n")
				return []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{{"start_line": 1, "old_lines": []string{"old"}, "new_lines": []string{strings.Repeat("x", patchMaxFileBytes), "tail"}}}}}
			},
		},
		{
			name: "hunks", reason: "too_many_hunks", metric: "hunks", unit: "hunks", limit: patchMaxHunksPerFile, wantActual: patchMaxHunksPerFile + 1,
			setup: func(t *testing.T, root string) []map[string]any {
				writePatchTestFile(t, filepath.Join(root, "notes.txt"), "old\n")
				hunks := make([]map[string]any, patchMaxHunksPerFile+1)
				for i := range hunks {
					hunks[i] = map[string]any{"start_line": 1, "old_lines": []string{"old"}, "new_lines": []string{"new"}}
				}
				return []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": hunks}}
			},
		},
		{
			name: "combined text", reason: "patch_too_large", metric: "combined_source_destination_bytes", unit: "bytes", limit: patchMaxTotalBytes, wantActual: 3 * 2 * 400004,
			setup: func(t *testing.T, root string) []map[string]any {
				content := "old\n" + strings.Repeat(strings.Repeat("x", 99)+"\n", 4000)
				var files []map[string]any
				for i := 0; i < 3; i++ {
					path := "notes-" + strconv.Itoa(i) + ".txt"
					writePatchTestFile(t, filepath.Join(root, path), content)
					files = append(files, map[string]any{"path": path, "action": "edit", "hunks": []map[string]any{{"start_line": 1, "old_lines": []string{"old"}, "new_lines": []string{"new"}}}})
				}
				return files
			},
		},
		{
			name: "review diff", reason: "patch_diff_too_large", metric: "review_diff_bytes", unit: "bytes", limit: patchMaxDiffBytes,
			setup: func(t *testing.T, root string) []map[string]any {
				writePatchTestFile(t, filepath.Join(root, "notes.txt"), strings.Repeat("a", 140<<10)+"\n")
				return []map[string]any{{"path": "notes.txt", "action": "replace", "content": strings.Repeat("b", 140<<10) + "\n", "expected_hash": patchContentHash([]byte(strings.Repeat("a", 140<<10) + "\n"))}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			files := test.setup(t, root)
			before := make(map[string]string)
			for _, file := range files {
				path := filepath.Join(root, file["path"].(string))
				before[path] = readPatchTestFile(t, path)
			}
			result := patchTestCall(NewBuiltinRunner(), "session_limits", root, FilePatch, map[string]any{"scope": "project", "files": files})
			payload := decodeToolResult(t, result)
			if result.Ok || payload["reason"] != test.reason || payload["metric"] != test.metric || payload["unit"] != test.unit || payload["limit"] != float64(test.limit) {
				t.Fatalf("wrong resource diagnostic: %s", result.Content)
			}
			if actual, ok := payload["actual"].(float64); !ok || actual <= float64(test.limit) {
				t.Fatalf("missing measured overage: %s", result.Content)
			}
			if test.wantActual > 0 && payload["actual"] != float64(test.wantActual) {
				t.Fatalf("wrong measured size: %s", result.Content)
			}
			if payload["errorKind"] != nil {
				t.Fatalf("resource limit reported as argument syntax: %s", result.Content)
			}
			for path, content := range before {
				if got := readPatchTestFile(t, path); got != content {
					t.Fatalf("rejected patch changed %s", path)
				}
			}
		})
	}
}

func TestFilePatchMissingPathRejectsWholeBatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "original\n")
	args := map[string]any{"scope": "project", "files": []map[string]any{
		{"path": "notes.txt", "action": "replace", "content": "changed\n", "expected_hash": patchContentHash([]byte("original\n"))},
		{"action": "create", "content": "must not be written\n"},
	}}
	result := patchTestCall(NewBuiltinRunner(), "session_missing_path", root, FilePatch, args)
	payload := decodeToolResult(t, result)
	if result.Ok || payload["errorKind"] != "path_required" || payload["field"] != "files[1].path" || payload["receivedBytes"] != float64(len(resultJSON(args))) {
		t.Fatalf("missing path was not diagnosed before approval: %s", result.Content)
	}
	if got := readPatchTestFile(t, path); got != "original\n" {
		t.Fatalf("invalid batch changed the first file: %q", got)
	}
}

func TestFilePatchUsesApprovalSnapshotAndWritesAtomicBatch(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "update.txt"), "old line\n")
	writePatchTestFile(t, filepath.Join(root, "delete.txt"), "remove me\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": "update.txt", "action": "replace", "content": "new line\n", "expected_hash": patchContentHash([]byte("old line\n"))},
			{"path": "nested/create.txt", "action": "create", "content": "created\n"},
			{"path": "delete.txt", "action": "delete"},
		},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_batch", Name: FilePatch, Args: args, ProjectDirs: []string{root}}
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

func TestFilePatchSupportsMultipleHunks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	original := "package main\n\nfunc message() string {\n\treturn \"old\"\n}\n"
	writePatchTestFile(t, path, original)
	runner := NewBuiltinRunner()
	applied := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "main.go",
			"action": "edit",
			"hunks": []map[string]any{
				{"start_line": 3, "old_lines": []string{"func message() string {"}, "new_lines": []string{"func greeting() string {"}},
				{"start_line": 4, "old_lines": []string{"\treturn \"old\""}, "new_lines": []string{"\treturn \"new\""}},
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

func TestFilePatchHunksUseOriginalLinesAndPreserveLineEndings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "one\r\ntwo\r\nthree")
	runner := NewBuiltinRunner()
	applied := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "notes.txt",
			"action": "edit",
			"hunks": []map[string]any{
				{"start_line": 1, "old_lines": []string{"one"}, "new_lines": []string{"ONE", "inserted"}},
				{"start_line": 3, "old_lines": []string{"three"}, "new_lines": []string{"THREE"}},
			},
		}},
	})
	if !applied.Ok {
		t.Fatalf("multi-hunk apply failed: %+v", applied)
	}
	if got := readPatchTestFile(t, path); got != "ONE\r\ninserted\r\ntwo\r\nTHREE" {
		t.Fatalf("original line coordinates or CRLF ending were not preserved: %q", got)
	}
}

func TestFilePatchHunkDeletesLinesWithExplicitEmptyNewLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "keep\nremove\n")
	runner := NewBuiltinRunner()
	applied := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "notes.txt",
			"action": "edit",
			"hunks":  []map[string]any{{"start_line": 2, "old_lines": []string{"remove"}, "new_lines": []string{}}},
		}},
	})
	if !applied.Ok {
		t.Fatalf("line deletion failed: %+v", applied)
	}
	if got := readPatchTestFile(t, path); got != "keep\n" {
		t.Fatalf("line deletion changed the remaining ending: %q", got)
	}
}

func TestFilePatchHunkInsertsAndAppendsLines(t *testing.T) {
	tests := []struct {
		name      string
		original  string
		startLine int
		newLines  []string
		want      string
	}{
		{name: "insert before existing line", original: "one\ntwo\n", startLine: 2, newLines: []string{"inserted"}, want: "one\ninserted\ntwo\n"},
		{name: "append without final newline", original: "one", startLine: 2, newLines: []string{"two"}, want: "one\ntwo"},
		{name: "append with final newline", original: "one\n", startLine: 2, newLines: []string{"two"}, want: "one\ntwo\n"},
		{name: "insert into empty file", original: "", startLine: 1, newLines: []string{"one"}, want: "one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "notes.txt")
			writePatchTestFile(t, path, test.original)
			runner := NewBuiltinRunner()
			applied := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
				"scope": "project",
				"files": []map[string]any{{
					"path":   "notes.txt",
					"action": "edit",
					"hunks":  []map[string]any{{"start_line": test.startLine, "old_lines": []string{}, "new_lines": test.newLines}},
				}},
			})
			if !applied.Ok {
				t.Fatalf("line insertion failed: %+v", applied)
			}
			if got := readPatchTestFile(t, path); got != test.want {
				t.Fatalf("line insertion result=%q want %q", got, test.want)
			}
		})
	}
}

func TestFilePatchHunkRequiresExplicitNewLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	result := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "notes.txt",
			"action": "edit",
			"hunks":  []map[string]any{{"start_line": 1, "old_lines": []string{"old"}}},
		}},
	})
	if result.Ok || !strings.Contains(result.Content, `"errorKind":"hunk_new_lines_required"`) {
		t.Fatalf("missing new_lines should fail: %+v", result)
	}
	if got := readPatchTestFile(t, path); got != "old\n" {
		t.Fatalf("invalid hunk changed worktree: %q", got)
	}
}

func TestFilePatchTargetsDuplicateTextByLine(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "same\nsame\n")
	runner := NewBuiltinRunner()

	applied := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "notes.txt",
			"action": "edit",
			"hunks":  []map[string]any{{"start_line": 2, "old_lines": []string{"same"}, "new_lines": []string{"changed"}}},
		}},
	})
	if !applied.Ok {
		t.Fatalf("positioned duplicate edit should apply: %+v", applied)
	}
	if got := readPatchTestFile(t, path); got != "same\nchanged\n" {
		t.Fatalf("wrong duplicate occurrence was changed: %q", got)
	}
}

func TestFilePatchRejectsMismatchedOrOverlappingHunks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "first\nsecond\nthird\n")
	runner := NewBuiltinRunner()

	mismatched := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "notes.txt",
			"action": "edit",
			"hunks":  []map[string]any{{"start_line": 2, "old_lines": []string{"third"}, "new_lines": []string{"changed"}}},
		}},
	})
	if mismatched.Ok || !strings.Contains(mismatched.Content, `"reason":"hunk_lines_mismatch"`) {
		t.Fatalf("mismatched positioned edit should fail: %+v", mismatched)
	}

	overlapping := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":   "notes.txt",
			"action": "edit",
			"hunks": []map[string]any{
				{"start_line": 1, "old_lines": []string{"first", "second"}, "new_lines": []string{"one"}},
				{"start_line": 2, "old_lines": []string{"second"}, "new_lines": []string{"two"}},
			},
		}},
	})
	if overlapping.Ok || !strings.Contains(overlapping.Content, `"reason":"hunk_overlap"`) {
		t.Fatalf("overlapping hunks should fail: %+v", overlapping)
	}
	if got := readPatchTestFile(t, path); got != "first\nsecond\nthird\n" {
		t.Fatalf("failed hunks changed worktree: %q", got)
	}
}

func TestFilePatchRequiresOneFileOperation(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "old\n")
	runner := NewBuiltinRunner()
	result := patchTestCall(runner, "session_a", root, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{
			"path":    "notes.txt",
			"action":  "edit",
			"content": "new\n",
			"hunks":   []map[string]any{{"start_line": 1, "old_lines": []string{"old"}, "new_lines": []string{"new"}}},
		}},
	})
	if result.Ok || !strings.Contains(result.Content, `"reason":"invalid_arguments"`) {
		t.Fatalf("multiple file operations should fail: %+v", result)
	}
}

func TestFilePatchRejectsDriftWithoutPartialWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	writePatchTestFile(t, first, "first old\n")
	writePatchTestFile(t, second, "second old\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": "first.txt", "action": "replace", "content": "first new\n", "expected_hash": patchContentHash([]byte("first old\n"))},
			{"path": "second.txt", "action": "replace", "content": "second new\n", "expected_hash": patchContentHash([]byte("second old\n"))},
		},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_drift", Name: FilePatch, Args: args, ProjectDirs: []string{root}}
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

func TestFilePatchFailsClosedWithoutApprovalSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "action": "replace", "content": "approved\n", "expected_hash": patchContentHash([]byte("old\n"))}},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_missing", Name: FilePatch, Args: args, ProjectDirs: []string{root}}
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

func TestFilePatchRejectsChangedArgumentsAfterApproval(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	approvedArgs, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "action": "replace", "content": "approved\n", "expected_hash": patchContentHash([]byte("old\n"))}},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_changed", Name: FilePatch, Args: approvedArgs, ProjectDirs: []string{root}}
	if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	call.Args, _ = json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "action": "replace", "content": "different\n", "expected_hash": patchContentHash([]byte("old\n"))}},
	})

	result := runner.Call(context.Background(), call)
	if result.Ok || !strings.Contains(result.Content, `"reason":"patch_arguments_changed"`) {
		t.Fatalf("changed arguments should invalidate approval: %+v", result)
	}
	if got := readPatchTestFile(t, path); got != "old\n" {
		t.Fatalf("changed arguments modified the file: %q", got)
	}
}

func TestFilePatchRejectsExpiredApprovalSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "old\n")
	runner := NewBuiltinRunner()
	args, _ := json.Marshal(map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "notes.txt", "action": "replace", "content": "new\n", "expected_hash": patchContentHash([]byte("old\n"))}},
	})
	call := Call{SessionID: "session_a", TurnID: "turn_patch", CallID: "call_expired", Name: FilePatch, Args: args, ProjectDirs: []string{root}}
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

func TestFilePatchRejectsCrossRootAndNoop(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	first := filepath.Join(firstRoot, "first.txt")
	second := filepath.Join(secondRoot, "second.txt")
	writePatchTestFile(t, first, "same\n")
	writePatchTestFile(t, second, "old\n")
	runner := NewBuiltinRunner()

	noop := patchTestCallWithRoots(runner, "session_a", []string{firstRoot}, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{{"path": "first.txt", "action": "replace", "content": "same\n", "expected_hash": patchContentHash([]byte("same\n"))}},
	})
	if noop.Ok || !strings.Contains(noop.Content, `"reason":"no_changes"`) {
		t.Fatalf("no-op patch should fail: %+v", noop)
	}

	crossRoot := patchTestCallWithRoots(runner, "session_a", []string{firstRoot, secondRoot}, FilePatch, map[string]any{
		"scope": "project",
		"files": []map[string]any{
			{"path": first, "action": "replace", "content": "first new\n", "expected_hash": patchContentHash([]byte("same\n"))},
			{"path": second, "action": "replace", "content": "second new\n", "expected_hash": patchContentHash([]byte("old\n"))},
		},
	})
	if crossRoot.Ok || !strings.Contains(crossRoot.Content, `"reason":"cross_root_patch"`) {
		t.Fatalf("cross-root patch should fail: %+v", crossRoot)
	}
}

func TestFilePatchReplacementRequiresReadVersion(t *testing.T) {
	for _, scenario := range []string{"missing", "changed", "deleted", "current", "invalid hash"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file.txt")
			writePatchTestFile(t, path, "original\n")
			runner := NewBuiltinRunner()
			defer runner.Close()
			read := runner.Call(context.Background(), Call{Name: FileRead, ProjectDirs: []string{root}, Args: resultJSON(map[string]any{"scope": "project", "path": path})})
			if !read.Ok {
				t.Fatal(read.Content)
			}
			hash := decodeToolResult(t, read)["contentHash"].(string)
			file := map[string]any{"path": path, "action": "replace", "content": "replacement\n", "expected_hash": hash}
			want, reason := "original\n", ""
			switch scenario {
			case "missing":
				delete(file, "expected_hash")
				reason = "file_version_required"
			case "changed":
				want, reason = "user edit\n", "file_conflict"
				writePatchTestFile(t, path, want)
			case "deleted":
				reason = "file_conflict"
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "invalid hash":
				file["expected_hash"], reason = "not a hash", "invalid_arguments"
			case "current":
				want = "replacement\n"
			}
			result := patchTestCall(runner, "versions", root, FilePatch, map[string]any{"scope": "project", "files": []map[string]any{
				{"path": "created.txt", "action": "create", "content": "new\n"}, file,
			}})
			if result.Ok != (reason == "") || reason != "" && decodeToolResult(t, result)["reason"] != reason {
				t.Fatalf("version outcome: %s", result.Content)
			}
			if scenario == "deleted" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("deleted file recreated: %v", err)
				}
			} else if got := readPatchTestFile(t, path); got != want {
				t.Fatalf("content = %q, want %q", got, want)
			}
			if reason != "" {
				if _, err := os.Stat(filepath.Join(root, "created.txt")); !os.IsNotExist(err) {
					t.Fatalf("failed batch partially wrote: %v", err)
				}
			}
		})
	}
}

func TestUnifiedPatchPreservesTextBytes(t *testing.T) {
	for _, tc := range []struct{ name, before, body, after string }{
		{"LF", "old\n", "@@ -1 +1 @@\n-old\n+new\n", "new\n"},
		{"CRLF", "old\r\nkeep\r\n", "@@ -1,2 +1,2 @@\n-old\r\n+new\r\n keep\r\n", "new\r\nkeep\r\n"},
		{"no final newline", "old", "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n", "new"},
		{"add final newline", "old", "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+old\n", "old\n"},
		{"remove final newline", "old\n", "@@ -1 +1 @@\n-old\n+old\n\\ No newline at end of file\n", "old"},
		{"bare CR at EOF", "old\r", "@@ -1 +1 @@\n-old\r\n\\ No newline at end of file\n+new\r\n\\ No newline at end of file\n", "new\r"},
		{"blank context and header-like content", "\n-- old\n++ old\n", "@@ -1,3 +1,3 @@\n \n--- old\n-++ old\n+-- new\n+++ new\n", "\n-- new\n++ new\n"},
		{"insert into empty", "", "@@ -0,0 +1 @@\n+new\n", "new\n"},
		{"delete all lines keeps file", "old\n", "@@ -1 +0,0 @@\n-old\n", ""},
		{"multiple hunks with shifted coordinates", "a\nb\nc\nd\ne\n", "@@ -1 +1,2 @@\n a\n+inserted\n@@ -4 +5 @@\n-d\n+D\n", "a\ninserted\nb\nc\nD\ne\n"},
		{"insert after line", "a\nb\n", "@@ -1,0 +2 @@\n+middle\n", "a\nmiddle\nb\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "notes.txt")
			writePatchTestFile(t, path, tc.before)
			result := patchTestCall(NewBuiltinRunner(), "session", root, FilePatch, map[string]any{
				"scope": "project", "diff": "--- a/notes.txt\n+++ b/notes.txt\n" + tc.body,
			})
			if !result.Ok {
				t.Fatalf("apply: %s", result.Content)
			}
			if got := readPatchTestFile(t, path); got != tc.after {
				t.Fatalf("bytes = %q, want %q", got, tc.after)
			}
		})
	}
}

func TestUnifiedPatchGitBatchUsesApprovalAndTracking(t *testing.T) {
	root := newGitTestRepository(t, true)
	before := map[string]string{"notes.txt": "old\r\n", "删除.txt": "remove", "space name.txt": "old\n"}
	for name, content := range before {
		writePatchTestFile(t, filepath.Join(root, name), content)
	}
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "patch baseline")
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "new\r\n")
	writePatchTestFile(t, filepath.Join(root, "space name.txt"), "changed\n")
	writePatchTestFile(t, filepath.Join(root, "created.txt"), "created")
	if err := os.Remove(filepath.Join(root, "删除.txt")); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", ".")
	cmd := exec.Command("git", "-c", "core.quotePath=true", "diff", "--cached", "--no-ext-diff", "--no-textconv", "--no-renames", "HEAD", "--")
	cmd.Dir = root
	diff, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	// Restore only the fixture's worktree; the submitted diff is real Git output.
	for name, content := range before {
		writePatchTestFile(t, filepath.Join(root, name), content)
	}
	if err := os.Remove(filepath.Join(root, "created.txt")); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	call := Call{SessionID: "session", TurnID: "turn", CallID: "patch", Name: FilePatch, ProjectDirs: []string{root}, Args: resultJSON(map[string]any{"scope": "project", "diff": string(diff)})}
	risk, ok := ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs)
	if !ok || risk.Class != RiskClassDestructive || len(risk.Paths) != 4 {
		t.Fatalf("deletion classification: %+v", risk)
	}
	tracking, ok := MutationTrackingForCall(call)
	if !ok || len(tracking.Targets) != 4 {
		t.Fatalf("tracking: %+v", tracking)
	}
	details, err := runner.ApprovalDetails(context.Background(), call)
	if err != nil || details["fileCount"] != 4 || details["destructive"] != true {
		t.Fatalf("approval: %+v %v", details, err)
	}
	for name, want := range before {
		if got := readPatchTestFile(t, filepath.Join(root, name)); got != want {
			t.Fatalf("approval changed %s", name)
		}
	}
	result := runner.Call(context.Background(), call)
	if !result.Ok || decodePatchPayload(t, result).FileCount != 4 {
		t.Fatalf("apply: %s", result.Content)
	}
	for name, want := range map[string]string{"notes.txt": "new\r\n", "space name.txt": "changed\n", "created.txt": "created"} {
		if got := readPatchTestFile(t, filepath.Join(root, name)); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "删除.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete: %v", err)
	}
	assertNoPatchTempFiles(t, root)
}

func TestUnifiedPatchRelocationUsesExactBytes(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		relocate, ok        bool
	}{
		{"opt in", "prefix\nold\n", "prefix\nnew\n", true, true},
		{"default rejects shift", "prefix\nold\n", "prefix\nold\n", false, false},
		{"ambiguous", "prefix\nold\nold\n", "prefix\nold\nold\n", true, false},
		{"endings disambiguate", "old\r\nold\n", "old\r\nnew\n", true, true},
		{"ending mismatch", "old\r\n", "old\r\n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writePatchTestFile(t, filepath.Join(root, "x"), tc.before)
			result := patchTestCall(NewBuiltinRunner(), "session", root, FilePatch, map[string]any{
				"scope": "project", "relocate": tc.relocate, "diff": "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n",
			})
			if result.Ok != tc.ok || readPatchTestFile(t, filepath.Join(root, "x")) != tc.after {
				t.Fatalf("apply: %s", result.Content)
			}
			if tc.ok && len(decodePatchPayload(t, result).Files[0].Relocations) != 1 {
				t.Fatalf("relocation missing: %s", result.Content)
			}
		})
	}
}

func TestUnifiedPatchRejectsMalformedBatchesWithoutWrites(t *testing.T) {
	valid := "--- a/first\n+++ b/first\n@@ -1 +1 @@\n-old\n+new\n"
	header := "--- a/second\n+++ b/second\n"
	for name, invalid := range map[string]string{
		"truncated":              header + "@@ -1 +1 @@\n-old\n",
		"excess":                 header + "@@ -1 +1 @@\n-old\n+new\n+extra\n",
		"coordinates":            header + "@@ -1 +2 @@\n-old\n+new\n",
		"zero coordinates":       header + "@@ -0 +0 @@\n-old\n+new\n",
		"context only":           header + "@@ -1 +1 @@\n old\n",
		"orphan marker":          header + "@@ -1 +1 @@\n\\ No newline at end of file\n-old\n+new\n",
		"double marker":          header + "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n\\ No newline at end of file\n+new\n",
		"after EOF":              header + "@@ -1,2 +1 @@\n-old\n\\ No newline at end of file\n-extra\n+new\n",
		"mismatch":               header + "@@ -1 +1 @@\n-not current\n+new\n",
		"partial delete":         "--- a/second\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n",
		"rename":                 "--- a/second\n+++ b/renamed\n@@ -1 +1 @@\n-old\n+new\n",
		"binary":                 "diff --git a/second b/second\nGIT binary patch\n",
		"mode change":            "diff --git a/second b/second\nold mode 100644\nnew mode 100755\n",
		"executable create":      "diff --git a/second b/second\nnew file mode 100755\n" + header + "@@ -1 +1 @@\n-old\n+new\n",
		"mode mismatch":          "diff --git a/second b/second\nnew file mode 100644\n" + header + "@@ -1 +1 @@\n-old\n+new\n",
		"duplicate target":       valid,
		"unterminated insertion": header + "@@ -2,0 +3 @@\n+new\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writePatchTestFile(t, filepath.Join(root, "first"), "old\n")
			writePatchTestFile(t, filepath.Join(root, "second"), "old\nextra")
			result := patchTestCall(NewBuiltinRunner(), "session", root, FilePatch, map[string]any{"scope": "project", "diff": valid + invalid})
			if result.Ok {
				t.Fatal("invalid batch accepted")
			}
			if readPatchTestFile(t, filepath.Join(root, "first")) != "old\n" || readPatchTestFile(t, filepath.Join(root, "second")) != "old\nextra" {
				t.Fatal("failed batch wrote a file")
			}
			assertNoPatchTempFiles(t, root)
		})
	}
}

func TestUnifiedPatchInputContractAndLimits(t *testing.T) {
	valid := "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n"
	for _, tc := range []struct{ raw, kind, field string }{
		{`{"scope":"project","diff":null}`, "invalid_type", "diff"},
		{`{"scope":"project","diff":[]}`, "invalid_type", "diff"},
		{`{"scope":"project","diff":""}`, "invalid_diff", "diff"},
		{string(resultJSON(map[string]any{"scope": "project", "diff": valid, "files": []any{}})), "conflicting_fields", "diff"},
		{`{"scope":"project","files":[],"relocate":true}`, "invalid_value", "relocate"},
	} {
		_, err := decodeFilePatchArgs([]byte(tc.raw))
		if err == nil || err.kind != tc.kind || err.field != tc.field {
			t.Errorf("%s: %+v", tc.raw, err)
		}
	}
	var tooManyFiles, tooManyHunks strings.Builder
	tooManyHunks.WriteString("--- a/x\n+++ b/x\n")
	for i := 1; i <= patchMaxHunksPerFile+1; i++ {
		fmt.Fprintf(&tooManyHunks, "@@ -%d +%d @@\n-old\n+new\n", i, i)
		if i <= patchMaxFiles+1 {
			fmt.Fprintf(&tooManyFiles, "--- a/f%d\n+++ b/f%d\n@@ -1 +1 @@\n-old\n+new\n", i, i)
		}
	}
	for _, diff := range []string{tooManyFiles.String(), tooManyHunks.String(), strings.Repeat("x", patchMaxTotalBytes+1), "```diff\n" + valid + "```", valid + "\x00", "--- \"bad\"garbage\n+++ x\n"} {
		if _, err := parseUnifiedPatch(diff, false); err == nil || err.kind != "invalid_diff" || err.field != "diff" {
			t.Fatalf("invalid input accepted: size=%d error=%+v", len(diff), err)
		}
	}
}

func TestUnifiedPatchRetainsPathAndApprovalBoundaries(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "x"), "old\n")
	writePatchTestFile(t, filepath.Join(other, "x"), "old\n")
	makeDiff := func(old, next string) string { return "--- " + old + "\n+++ " + next + "\n@@ -1 +1 @@\n-old\n+new\n" }
	for _, tc := range []struct {
		name, diff string
		roots      []string
	}{
		{"ambiguous relative", makeDiff("a/x", "b/x"), []string{root, other}},
		{"outside", makeDiff(filepath.Join(other, "x"), filepath.Join(other, "x")), []string{root}},
		{"traversal", makeDiff("../x", "../x"), []string{root}},
		{"cross root", makeDiff(filepath.Join(root, "x"), filepath.Join(root, "x")) + makeDiff(filepath.Join(other, "x"), filepath.Join(other, "x")), []string{root, other}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := patchTestCallWithRoots(NewBuiltinRunner(), "session", tc.roots, FilePatch, map[string]any{"scope": "project", "diff": tc.diff})
			if result.Ok || readPatchTestFile(t, filepath.Join(root, "x")) != "old\n" || readPatchTestFile(t, filepath.Join(other, "x")) != "old\n" {
				t.Fatalf("boundary bypassed: %s", result.Content)
			}
		})
	}
	for _, drift := range []string{"file", "arguments"} {
		t.Run(drift, func(t *testing.T) {
			path := filepath.Join(root, "x")
			writePatchTestFile(t, path, "old\n")
			runner := NewBuiltinRunner()
			call := Call{SessionID: "session", CallID: "patch", Name: FilePatch, ProjectDirs: []string{root, other}, Args: resultJSON(map[string]any{"scope": "project", "diff": makeDiff(strconv.Quote(path)+"\tdate", strconv.Quote(path)+"\tdate")})}
			if _, err := runner.ApprovalDetails(context.Background(), call); err != nil {
				t.Fatal(err)
			}
			want := "old\n"
			if drift == "file" {
				want = "external edit\n"
				writePatchTestFile(t, path, want)
			} else {
				call.Args = resultJSON(map[string]any{"scope": "project", "diff": makeDiff(path, path), "relocate": true})
			}
			if result := runner.Call(context.Background(), call); result.Ok || readPatchTestFile(t, path) != want {
				t.Fatalf("stale approval applied: %s", result.Content)
			}
		})
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
