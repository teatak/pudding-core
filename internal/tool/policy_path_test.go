package tool

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFileRiskPreservesFilenameWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want []string
	}{
		{FileWrite, `{"scope":"project","path":" note.txt ","content":"new"}`, []string{" note.txt "}},
		{FileDelete, `{"scope":"project","path":" note.txt "}`, []string{" note.txt "}},
		{FileMove, `{"scope":"project","from_path":" note.txt ","to_path":"note.txt"}`, []string{" note.txt ", "note.txt"}},
		{FileCopy, `{"from":{"scope":"project","path":"source"},"to":{"scope":"project","path":" note.txt "}}`, []string{" note.txt "}},
		{FilePatch, `{"scope":"project","files":[{"path":" note.txt ","action":"delete"}]}`, []string{" note.txt "}},
	} {
		risk, ok := ClassifyToolCall(tc.name, json.RawMessage(tc.args))
		if !ok || !reflect.DeepEqual(risk.Paths, tc.want) {
			t.Fatalf("%s changed approval paths: %+v", tc.name, risk)
		}
	}
	if got := compactRiskPaths("", " \t", " note.txt ", "note.txt", " note.txt "); !reflect.DeepEqual(got, []string{" note.txt ", "note.txt"}) {
		t.Fatalf("blank filtering or exact deduplication changed: %q", got)
	}
}
