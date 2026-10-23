package contextbuilder

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestCoolingNativeContinuationKeepsPairingAndUsesColdResultOnWire(t *testing.T) {
	for _, tc := range []struct {
		kind, native string
		wire         func(provider.Request) provider.WireInput
	}{
		{provider.ContinuationOpenAIChat, `{"role":"assistant","reasoning_content":"native-reasoning","tool_calls":[{"id":"call","type":"function","function":{"name":"builtin_command_run","arguments":"{}"}}]}`, openai.ChatRequestInput},
		{provider.ContinuationOpenAIResponses, `[{"type":"reasoning","id":"r","encrypted_content":"native-reasoning"},{"type":"function_call","id":"f","call_id":"call","name":"builtin_command_run","arguments":"{}"}]`, openai.ResponsesRequestInput},
		{provider.ContinuationAnthropic, `[{"type":"thinking","thinking":"native-reasoning","signature":"sig"},{"type":"tool_use","id":"call","name":"builtin_command_run","input":{}}]`, anthropic.RequestInput},
		{provider.ContinuationGoogle, `[{"text":"native-reasoning","thought":true},{"functionCall":{"id":"call","name":"builtin_command_run","args":{}},"thoughtSignature":"sig"}]`, google.RequestInput},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ctx := context.Background()
			db := storetest.New(t)
			if err := db.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "m"}); err != nil {
				t.Fatal(err)
			}
			state := &store.ProviderState{Provider: "mock", Model: "m", Kind: tc.kind, Data: json.RawMessage(tc.native)}
			raw := strings.Repeat("ORIGINAL-EVIDENCE ", 300)
			messages := []*store.Message{
				{ID: "input", TurnID: "old", Role: store.RoleUser, Parts: store.TextPart("inspect")},
				{ID: "call", TurnID: "old", TurnIndex: 1, Role: store.RoleAssistant, ProviderState: state, Parts: []store.ContentPart{{Type: store.ContentPartToolUse, Name: tool.CommandRun, CallID: "call", Args: []byte(`{}`)}}},
				{ID: "result", TurnID: "old", TurnIndex: 2, Role: store.RoleTool, Parts: []store.ContentPart{{Type: store.ContentPartToolResult, Name: tool.CommandRun, CallID: "call", Ok: true, Content: raw}}},
				{ID: "current", TurnID: "current", Role: store.RoleUser, Parts: store.TextPart("continue")},
			}
			defs := []provider.ToolDef{{Name: tool.CommandRun}, {Name: tool.HistoryGetMessage}}
			build := func(defs []provider.ToolDef, cold store.ContextCooling) provider.Request {
				r, err := New(db, nil).BuildForProviderWithHistory(ctx, "s", "mock", "m", "code", defs, messages, provider.ModelConfig{}, cold)
				if err != nil {
					t.Fatal(err)
				}
				r.Tools = defs
				return r
			}
			cooled := build(defs, store.ContextCooling{ThroughTurnID: "old"})
			wire, err := json.Marshal(tc.wire(cooled).Messages)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(wire), "cold_only") || strings.Contains(string(wire), "ORIGINAL-EVIDENCE") || !strings.Contains(string(wire), "native-reasoning") {
				t.Fatalf("wrong wire projection: %s", wire)
			}
			if string(state.Data) != tc.native || messages[2].Parts[0].Content != raw {
				t.Fatal("mutated canonical history")
			}
			// Without the readback tool the original view must remain usable.
			withoutReader := build(defs[:1], store.ContextCooling{ThroughTurnID: "old"})
			wire, err = json.Marshal(tc.wire(withoutReader).Messages)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "cold_only") || !strings.Contains(string(wire), "ORIGINAL-EVIDENCE") {
				t.Fatal("cooled without readback")
			}
		})
	}
}
