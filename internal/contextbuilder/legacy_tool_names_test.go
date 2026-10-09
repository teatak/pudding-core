package contextbuilder

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

// Calls stored before plugins and widgets were renamed stay in the request
// under the current names of the same tools.
func TestHistoricalPluginAndWidgetCallsUseCurrentToolNames(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	history := []*store.Message{{ID: "m", SessionID: "s", TurnID: "t", Role: store.RoleAssistant, Parts: []store.ContentPart{
		{Type: store.ContentPartToolUse, CallID: "mcp", Name: "app_mcp__github__list_issues", Args: json.RawMessage(`{}`)},
		{Type: store.ContentPartToolResult, CallID: "mcp", Name: "app_mcp__github__list_issues", Ok: true, Content: `{"issues":[]}`},
		{Type: store.ContentPartToolUse, CallID: "inspect", Name: "canvas_inspect", Args: json.RawMessage(`{"id":"canvas_1"}`)},
		{Type: store.ContentPartToolResult, CallID: "inspect", Name: "canvas_inspect", Ok: true, Content: `{"ok":true}`},
	}}}
	b := New(st, nil)
	tools := []provider.ToolDef{{Name: "plugin_mcp__github__list_issues"}, {Name: "widget_inspect"}}
	req, err := b.BuildForProviderWithHistory(ctx, "s", "mock", "mock", "work", tools, history, provider.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, message := range req.Messages {
		for _, part := range message.Parts {
			if part.Type == provider.PartToolUse || part.Type == provider.PartToolResult {
				names = append(names, part.CallID+"="+part.Name)
			}
		}
	}
	want := []string{"mcp=plugin_mcp__github__list_issues", "mcp=plugin_mcp__github__list_issues", "inspect=widget_inspect", "inspect=widget_inspect"}
	if len(names) != len(want) {
		t.Fatalf("tool parts = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool parts = %v, want %v", names, want)
		}
	}
}

func TestRenamedArtifactAndSubtaskHistoryRetainsCallsAndResults(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock", ActiveMode: store.ModeWork}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"builtin_studio_list", "builtin_artifact_list"},
		{"builtin_studio_open", "builtin_artifact_open"},
		{"builtin_collaboration_list", "builtin_subtask_list"},
		{"builtin_collaboration_dispatch", "builtin_subtask_dispatch"},
		{"builtin_collaboration_send", "builtin_subtask_send"},
		{"builtin_collaboration_wait", "builtin_subtask_wait"},
		{"builtin_collaboration_stop", "builtin_subtask_stop"},
	} {
		t.Run(pair[0], func(t *testing.T) {
			history := []*store.Message{{ID: "m", SessionID: "s", TurnID: "t", Role: store.RoleAssistant, Parts: []store.ContentPart{
				{Type: store.ContentPartToolUse, CallID: "call", Name: pair[0], Args: json.RawMessage(`{"session_id":"child-1"}`)},
				{Type: store.ContentPartToolResult, CallID: "call", Name: pair[0], Ok: true, Content: `{"id":"child-1","result":"saved"}`},
			}}}
			before, _ := json.Marshal(history)
			req, err := New(st, nil).BuildForProviderWithHistory(ctx, "s", "mock", "mock", "work", []provider.ToolDef{{Name: pair[1]}}, history, provider.ModelConfig{})
			if err != nil {
				t.Fatal(err)
			}
			var parts []provider.Part
			for _, m := range req.Messages {
				for _, p := range m.Parts {
					if p.Type == provider.PartToolUse || p.Type == provider.PartToolResult {
						parts = append(parts, p)
					}
				}
			}
			if len(parts) != 2 {
				t.Fatalf("lost historical call/result: %+v", parts)
			}
			for _, p := range parts {
				if p.Name != pair[1] || p.CallID != "call" {
					t.Fatalf("wrong identity: %+v", p)
				}
			}
			if string(parts[0].Args) != `{"session_id":"child-1"}` || parts[1].Content != `{"id":"child-1","result":"saved"}` {
				t.Fatalf("history payload changed: %+v", parts)
			}
			after, _ := json.Marshal(history)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("canonical history was mutated")
			}
		})
	}
}
