package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestFileWriteRejectsCaseAliasBeforeWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, "original\n")
	call := Call{SessionID: "schema_review", TurnID: "turn_review", CallID: "call_review", Name: FileWrite, ProjectDirs: []string{root}, Args: json.RawMessage(`{"scope":"project","path":"notes.txt","content":"requested\n","Content":""}`)}
	result := NewBuiltinRunner().Call(context.Background(), call)
	assertSchemaArgumentFailure(t, call, result, "unknown_field", "Content", "")
	actual := readPatchTestFile(t, path)
	if result.Ok || actual != "original\n" {
		t.Fatalf("schema-unknown Content alias executed: ok=%v actual=%q result=%s", result.Ok, actual, result.Content)
	}
}

func TestFilePatchRejectsNullLineElementsWithoutChangingBatch(t *testing.T) {
	for _, tc := range []struct{ name, original, hunk string }{
		{"new_lines", "original\n", `{"start_line":1,"old_lines":["original"],"new_lines":[null]}`},
		{"old_lines", "\n", `{"start_line":1,"old_lines":[null],"new_lines":["replacement"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "notes.txt")
			sibling := filepath.Join(root, "sibling.txt")
			writePatchTestFile(t, path, tc.original)
			writePatchTestFile(t, sibling, "untouched\n")
			runner := NewBuiltinRunner()
			call := Call{SessionID: "schema_review", TurnID: "turn_review", CallID: "call_review", Name: FilePatch, ProjectDirs: []string{root}, Args: json.RawMessage(" \n " + `{"scope":"project","files":[{"path":"sibling.txt","action":"replace","content":"private补丁\n"},{"path":"notes.txt","action":"edit","hunks":[` + tc.hunk + `]}]}`)}
			_, err := runner.ApprovalDetails(context.Background(), call)
			results := []Result{runner.Call(context.Background(), call)}
			if err == nil {
				t.Error("null line element was accepted for approval")
			} else {
				results = append(results, ApprovalDetailsFailure(call, err))
			}
			for _, result := range results {
				payload := assertSchemaArgumentFailure(t, call, result, "invalid_type", "files[1].hunks[0]."+tc.name+"[0]", "string")
				if payload["offset"] != float64(bytes.Index(call.Args, []byte("null"))+len("null")) {
					t.Errorf("null offset must use original UTF-8 input bytes: %s", result.Content)
				}
				if bytes.Contains([]byte(result.Content), []byte("private补丁")) {
					t.Error("argument diagnostic retained private content")
				}
			}
			actual, other := readPatchTestFile(t, path), readPatchTestFile(t, sibling)
			if actual != tc.original || other != "untouched\n" {
				t.Fatalf("schema-invalid null %s element changed batch: actual=%q sibling=%q", tc.name, actual, other)
			}
		})
	}
}

func TestFilePatchRejectsNestedCaseAliasesBeforeApproval(t *testing.T) {
	for _, tc := range []struct{ field, file string }{
		{"Path", `{"Path":"notes.txt","action":"delete"}`},
		{"Action", `{"path":"notes.txt","Action":"delete"}`},
		{"New_Lines", `{"path":"notes.txt","action":"edit","hunks":[{"start_line":1,"old_lines":["original"],"New_Lines":["replacement"]}]}`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "notes.txt")
			writePatchTestFile(t, path, "original\n")
			runner := NewBuiltinRunner()
			call := Call{SessionID: "schema_review", TurnID: "turn_review", CallID: "call_review", Name: FilePatch, ProjectDirs: []string{root}, Args: json.RawMessage(`{"scope":"project","files":[` + tc.file + `]}`)}
			_, err := runner.ApprovalDetails(context.Background(), call)
			if err == nil {
				t.Error("case alias was accepted for approval")
			} else {
				assertSchemaArgumentFailure(t, call, ApprovalDetailsFailure(call, err), "unknown_field", tc.field, "")
			}
			assertSchemaArgumentFailure(t, call, runner.Call(context.Background(), call), "unknown_field", tc.field, "")
			if actual := readPatchTestFile(t, path); actual != "original\n" {
				t.Fatalf("case alias changed file: %q", actual)
			}
		})
	}
}

func TestFilePatchPreservesExplicitEmptyLineElements(t *testing.T) {
	for _, tc := range []struct {
		name, original, want string
		oldLines, newLines   []string
	}{
		{"replace empty line", "\n", "replacement\n", []string{""}, []string{"replacement"}},
		{"write empty line", "original\n", "\n", []string{"original"}, []string{""}},
		{"delete empty line", "\n", "", []string{""}, []string{}},
		{"insert empty line", "original\n", "\noriginal\n", []string{}, []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "notes.txt")
			writePatchTestFile(t, path, tc.original)
			result := patchTestCall(NewBuiltinRunner(), "session_blank", root, FilePatch, map[string]any{
				"scope": "project",
				"files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{{
					"start_line": 1, "old_lines": tc.oldLines, "new_lines": tc.newLines,
				}}}},
			})
			if !result.Ok {
				t.Fatalf("explicit empty lines rejected: %s", result.Content)
			}
			if actual := readPatchTestFile(t, path); actual != tc.want {
				t.Fatalf("empty array and empty line were confused: got %q want %q", actual, tc.want)
			}
		})
	}
}

func assertSchemaArgumentFailure(t *testing.T, call Call, result Result, kind, field, expected string) map[string]any {
	t.Helper()
	payload := decodeToolResult(t, result)
	if result.Ok || payload["reason"] != "invalid_arguments" || payload["errorKind"] != kind || payload["field"] != field || payload["receivedBytes"] != float64(len(call.Args)) {
		t.Errorf("incorrect argument diagnostic: %s", result.Content)
	}
	if expected != "" && payload["expected"] != expected {
		t.Errorf("incorrect expected type: %s", result.Content)
	}
	return payload
}
