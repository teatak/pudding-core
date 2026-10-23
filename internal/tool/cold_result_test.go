package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestColdResultKeepsCoordinatesAndExemptsProtectedResults(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"ok": true, "path": "a.go", "start": 1, "end": 100, "lines": 100, "numberedContent": strings.Repeat("code\n", 1500)})
	got := ColdResultContent(FileSlice, true, string(raw), "turn", "call")
	var stub map[string]any
	if err := json.Unmarshal([]byte(got), &stub); err != nil {
		t.Fatal(err)
	}
	if stub["cold_only"] != true || stub["path"] != "a.go" || stub["start"] != float64(1) || stub["result_ref"] == nil || stub["numberedContent"] != nil {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		name, content, turn, call string
		ok                        bool
	}{
		{FileSlice, string(raw), "turn", "call", false},
		{FileSlice, `{"content":"small"}`, "turn", "call", true},
		{HistoryGetMessage, string(raw), "turn", "call", true},
		{RequestUserInput, string(raw), "turn", "call", true},
		{SkillRead, string(raw), "turn", "call", true},
		{FileSlice, string(raw), "", "call", true},
		{FileSlice, string(raw), "turn", "", true},
	} {
		want := tc.content
		if got := ColdResultContent(tc.name, tc.ok, tc.content, tc.turn, tc.call); got != want {
			t.Fatalf("changed exempt result %s", tc.name)
		}
	}
}
