package contextbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
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
		{"builtin_doc_create", "builtin_page_create"},
		{"builtin_table_read", "builtin_sheet_read"},
		{"builtin_table_update", "builtin_sheet_edit"},
		{"builtin_doc_read", "builtin_page_read"},
		{"builtin_doc_edit", "builtin_page_edit"},
		{"builtin_note_create", "builtin_page_create"},
		{"builtin_note_read", "builtin_page_read"},
		{"builtin_note_edit", "builtin_page_edit"},
		{"builtin_page_create", "builtin_page_create"},
		{"builtin_page_read", "builtin_page_read"},
		{"builtin_page_edit", "builtin_page_edit"},
		{"builtin_studio_list", "builtin_artifact_list"},
		{"builtin_studio_open", "builtin_artifact_open"},
		{"builtin_collaboration_list", "builtin_subagent_list"},
		{"builtin_collaboration_dispatch", "builtin_subagent_dispatch"},
		{"builtin_collaboration_send", "builtin_subagent_send"},
		{"builtin_collaboration_wait", "builtin_subagent_wait"},
		{"builtin_collaboration_stop", "builtin_subagent_stop"},
		{"builtin_subtask_list", "builtin_subagent_list"},
		{"builtin_subtask_dispatch", "builtin_subagent_dispatch"},
		{"builtin_subtask_send", "builtin_subagent_send"},
		{"builtin_subtask_wait", "builtin_subagent_wait"},
		{"builtin_subtask_stop", "builtin_subagent_stop"},
		{"builtin_subtask_archive", "builtin_subagent_archive"},
		{"builtin_subtask_restore", "builtin_subagent_restore"},
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

// Check the wire representation because providers prefer native continuations
// over the canonical parts where historical tool names are translated.
func TestRenamedToolNamesAcrossProviderContinuations(t *testing.T) {
	for _, protocol := range []struct {
		kind, text, call string
		wire             func(provider.Request) provider.WireInput
	}{
		{provider.ContinuationOpenAIChat, `{"role":"assistant","content":%q}`, `{"role":"assistant","content":"native-call","tool_calls":[{"id":"subagent-call","type":"function","function":{"name":%q,"arguments":"{\"prompt\":\"task-prompt\"}"}}]}`, openai.ChatRequestInput},
		{provider.ContinuationOpenAIResponses, `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}]`, `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"native-call"}]},{"type":"function_call","call_id":"subagent-call","name":%q,"arguments":"{\"prompt\":\"task-prompt\"}"}]`, openai.ResponsesRequestInput},
		{provider.ContinuationAnthropic, `[{"type":"text","text":%q}]`, `[{"type":"text","text":"native-call"},{"type":"tool_use","id":"subagent-call","name":%q,"input":{"prompt":"task-prompt"}}]`, anthropic.RequestInput},
		{provider.ContinuationGoogle, `[{"text":%q}]`, `[{"text":"native-call"},{"functionCall":{"id":"subagent-call","name":%q,"args":{"prompt":"task-prompt"}}}]`, google.RequestInput},
	} {
		for _, pair := range [][2]string{
			{"builtin_subtask_dispatch", "builtin_subagent_dispatch"},
			{"builtin_collaboration_dispatch", "builtin_subagent_dispatch"},
			{"builtin_subagent_dispatch", "builtin_subagent_dispatch"},
			{"builtin_doc_create", "builtin_page_create"},
			{"builtin_table_read", "builtin_sheet_read"},
			{"builtin_table_update", "builtin_sheet_edit"},
			{"builtin_doc_read", "builtin_page_read"},
			{"builtin_doc_edit", "builtin_page_edit"},
			{"builtin_note_create", "builtin_page_create"},
			{"builtin_note_read", "builtin_page_read"},
			{"builtin_note_edit", "builtin_page_edit"},
			{"builtin_page_create", "builtin_page_create"},
			{"builtin_page_read", "builtin_page_read"},
			{"builtin_page_edit", "builtin_page_edit"},
		} {
			storedName, currentName := pair[0], pair[1]
			t.Run(protocol.kind+"/"+storedName, func(t *testing.T) {
				ctx := context.Background()
				st := storetest.New(t)
				if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "test", Model: "model", ActiveMode: store.ModeWork}); err != nil {
					t.Fatal(err)
				}
				state := func(data string) *store.ProviderState {
					return &store.ProviderState{Provider: "test", Model: "model", Kind: protocol.kind, Data: json.RawMessage(data)}
				}
				history := []*store.Message{
					{Role: store.RoleUser, Parts: store.TextPart("Earlier task")},
					{Role: store.RoleAssistant, Parts: store.TextPart("canonical-before"), ProviderState: state(fmt.Sprintf(protocol.text, "native-before"))},
					{Role: store.RoleUser, Parts: store.TextPart("Dispatch a subagent")},
					{Role: store.RoleAssistant, Parts: []store.ContentPart{
						{Type: store.ContentPartText, Text: "canonical-call"},
						{Type: store.ContentPartToolUse, Name: storedName, CallID: "subagent-call", Args: json.RawMessage(`{"prompt":"task-prompt"}`)},
					}, ProviderState: state(fmt.Sprintf(protocol.call, storedName))},
					{Role: store.RoleTool, Parts: []store.ContentPart{{Type: store.ContentPartToolResult, Name: storedName, CallID: "subagent-call", Ok: true, Content: `{"id":"child-1"}`}}},
					{Role: store.RoleAssistant, Parts: store.TextPart("canonical-answer"), ProviderState: state(fmt.Sprintf(protocol.text, "native-answer"))},
					{Role: store.RoleUser, Parts: store.TextPart("Later task")},
					{Role: store.RoleAssistant, Parts: store.TextPart("canonical-after"), ProviderState: state(fmt.Sprintf(protocol.text, "native-after"))},
				}
				before, err := json.Marshal(history)
				if err != nil {
					t.Fatal(err)
				}
				req, err := New(st, nil).BuildForProviderWithHistory(ctx, "s", "test", "model", "work", []provider.ToolDef{{Name: currentName}}, history, provider.ModelConfig{})
				if err != nil {
					t.Fatal(err)
				}
				wire, err := json.Marshal(protocol.wire(req).Messages)
				if err != nil {
					t.Fatal(err)
				}
				for _, retired := range []string{"builtin_subtask_dispatch", "builtin_collaboration_dispatch", "builtin_doc_create", "builtin_doc_read", "builtin_doc_edit", "builtin_note_create", "builtin_note_read", "builtin_note_edit"} {
					if strings.Contains(string(wire), retired) {
						t.Fatalf("retired tool name survived native replay: %s", wire)
					}
				}
				for _, retained := range []string{currentName, "task-prompt", "child-1", "native-before", "native-after"} {
					if !strings.Contains(string(wire), retained) {
						t.Fatalf("history lost %q: %s", retained, wire)
					}
				}
				for _, suffix := range []string{"call", "answer"} {
					want, unwanted := "native-"+suffix, "canonical-"+suffix
					if storedName != currentName {
						want, unwanted = unwanted, want
					}
					if !strings.Contains(string(wire), want) || strings.Contains(string(wire), unwanted) {
						t.Fatalf("wrong continuation fallback: want %q without %q: %s", want, unwanted, wire)
					}
				}
				var parts []provider.Part
				for _, message := range req.Messages {
					for _, part := range message.Parts {
						if part.Type == provider.PartToolUse || part.Type == provider.PartToolResult {
							parts = append(parts, part)
						}
					}
				}
				wantParts := []provider.Part{
					{Type: provider.PartToolUse, Name: currentName, CallID: "subagent-call", Args: json.RawMessage(`{"prompt":"task-prompt"}`)},
					{Type: provider.PartToolResult, Name: currentName, CallID: "subagent-call", Ok: true, Content: `{"id":"child-1"}`},
				}
				if !reflect.DeepEqual(parts, wantParts) {
					t.Fatalf("call/result pairing or payload changed: got %+v, want %+v", parts, wantParts)
				}
				after, err := json.Marshal(history)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("canonical history or provider state was mutated")
				}
			})
		}
	}
}
