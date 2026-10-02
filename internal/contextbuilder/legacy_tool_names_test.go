package contextbuilder

import (
	"context"
	"encoding/json"
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
