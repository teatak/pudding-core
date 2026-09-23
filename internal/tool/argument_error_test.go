package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

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
