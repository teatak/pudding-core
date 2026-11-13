package tool

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReadAndCodeToolsRejectMalformedArguments(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	for _, name := range []string{FileList, FileStat, FileRead, FileSlice, FileSearch, CodeSymbols, CodeDefinition, CodeReferences, CodeDiagnostics, CodeRename} {
		for _, tc := range []struct{ raw, kind, field string }{
			{`{"scope":"project","start_line":100}`, "unknown_field", "start_line"},
			{`{"Scope":"project"}`, "unknown_field", "Scope"},
			{`{"scope":null}`, "invalid_type", "scope"},
			{`{"scope":17}`, "invalid_type", "scope"},
			{`[]`, "expected_object", ""},
			{`{"scope":"project"}{}`, "invalid_json", ""},
			{`{"scope":"project`, "truncated_json", ""},
		} {
			t.Run(name+"/"+tc.kind+"/"+tc.field, func(t *testing.T) {
				result := runner.Call(context.Background(), Call{Name: name, SessionID: "argument-check", ProjectDirs: []string{root}, Args: json.RawMessage(tc.raw)})
				fields := decodeToolResult(t, result)
				if result.Ok || fields["reason"] != "invalid_arguments" || fields["errorKind"] != tc.kind || fields["hint"] == nil {
					t.Fatalf("%s accepted or misreported malformed args: %s", name, result.Content)
				}
				if tc.field != "" && fields["field"] != tc.field {
					t.Fatalf("incorrect field: %s", result.Content)
				}
				if tc.kind == "unknown_field" {
					allowed := fields["allowedFields"].([]any)
					found := false
					for _, field := range allowed {
						found = found || field == "scope"
					}
					if !found {
						t.Fatalf("missing legal parameters: %s", result.Content)
					}
				}
			})
		}
	}
}

func TestToolArgumentObjectValidatesPromotedAndNestedFields(t *testing.T) {
	var references codeReferencesArgs
	if err := decodeToolArgumentObject(json.RawMessage(`{"scope":"project","path":"main.go","line":1,"column":2,"include_declaration":false,"max_results":10}`), &references); err != nil || references.Path != "main.go" || references.Line != 1 || references.IncludeDeclaration == nil || *references.IncludeDeclaration {
		t.Fatalf("valid embedded code arguments rejected: %+v, %v", references, err)
	}
	var rename codeRenameArgs
	if err := decodeToolArgumentObject(json.RawMessage(`{"scope":"project","path":"main.go","line":1,"column":2,"new_name":"renamed"}`), &rename); err != nil || rename.NewName != "renamed" || rename.Column != 2 {
		t.Fatalf("valid rename arguments rejected: %+v, %v", rename, err)
	}
	var batch struct {
		Items []codePositionArgs `json:"items"`
	}
	err := decodeToolArgumentObject(json.RawMessage(`{"items":[{"scope":"project","line_number":12}]}`), &batch)
	if err == nil || err.field != "items[0].line_number" || !reflect.DeepEqual(err.allowedFields, []string{"column", "language", "line", "path", "scope"}) {
		t.Fatalf("nested error lost field path or legal names: %+v", err)
	}
}

func TestToolArgumentObjectErrorsAreSpecificAndDoNotRetainInput(t *testing.T) {
	for _, test := range []struct {
		name, raw, kind, field, expected string
	}{
		{"empty", " \n ", "missing_arguments", "", ""},
		{"syntax EOF", " \n {\"content\":\"private补丁", "truncated_json", "", ""},
		{"syntax before unknown field", `{"path":"note.txt","extra":true,"content":`, "truncated_json", "", ""},
		{"encoded object", `"{\"path\":\"note.txt\"}"`, "expected_object", "", "object"},
		{"array", `[]`, "expected_object", "", "object"},
		{"multiple values", `{"path":"note.txt","content":"private补丁"}{}`, "invalid_json", "", ""},
		{"wrong content type", `{"path":"note.txt","content":[]}`, "invalid_type", "content", "string"},
		{"unknown field", `{"path":"note.txt","content":"private补丁","payload":true}`, "unknown_field", "payload", ""},
		{"missing path", `{"content":"private补丁"}`, "missing_field", "path", "string"},
		{"missing content", `{"path":"note.txt"}`, "missing_field", "content", "string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var args struct {
				Path    string  `json:"path"`
				Content *string `json:"content"`
			}
			err := decodeToolArgumentObject(json.RawMessage(test.raw), &args, toolArgumentField{"path", "string"}, toolArgumentField{"content", "string"})
			if err == nil {
				t.Fatal("invalid arguments accepted")
			}
			result := toolArgumentFailure(Result{}, err)
			payload := decodeToolResult(t, result)
			if result.Ok || payload["reason"] != "invalid_arguments" || payload["errorKind"] != test.kind || payload["receivedBytes"] != float64(len(test.raw)) {
				t.Fatalf("incorrect argument diagnostics: %s", result.Content)
			}
			if test.field != "" && payload["field"] != test.field || test.expected != "" && payload["expected"] != test.expected {
				t.Fatalf("wrong field/type: %s", result.Content)
			}
			if test.kind == "truncated_json" && payload["offset"] != float64(len(test.raw)) {
				t.Fatalf("offset must use original input bytes: %s", result.Content)
			}
			if strings.Contains(result.Content, "private补丁") {
				t.Fatal("diagnostics retained the input body")
			}
		})
	}
}

func TestToolArgumentObjectAllowsExplicitEmptyContent(t *testing.T) {
	var args struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	if err := decodeToolArgumentObject(json.RawMessage(` {"path":"note.txt","content":""} `), &args, toolArgumentField{"path", "string"}, toolArgumentField{"content", "string"}); err != nil || args.Content == nil || *args.Content != "" || args.Path != "note.txt" {
		t.Fatalf("explicit empty content rejected: args=%+v err=%v", args, err)
	}
}
