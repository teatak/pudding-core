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
	"github.com/teatak/pudding-core/internal/tool"
)

// Exercise the wire serializers: a canonical-only assertion would miss native
// continuations, which adapters prefer over visible parts when replaying history.
func TestUIOnlyHistoryExcludedFromEveryProvider(t *testing.T) {
	for _, name := range []string{tool.SetNextPrompt, "builtin_plan_update"} {
		t.Run(name, func(t *testing.T) {
			testUIOnlyHistoryExcludedFromEveryProvider(t, name)
		})
	}
}

func testUIOnlyHistoryExcludedFromEveryProvider(t *testing.T, name string) {
	for _, protocol := range []struct {
		kind, text, call string
		wire             func(provider.Request) provider.WireInput
	}{
		{provider.ContinuationOpenAIChat, `{"role":"assistant","content":%q}`, `{"role":"assistant","content":"native-suggestion","tool_calls":[{"id":"ui-call","type":"function","function":{"name":"builtin_set_next_prompt","arguments":"{\"text\":\"suggestion-secret\"}"}}]}`, openai.ChatRequestInput},
		{provider.ContinuationOpenAIResponses, `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}]`, `[{"type":"function_call","call_id":"ui-call","name":"builtin_set_next_prompt","arguments":"{\"text\":\"suggestion-secret\"}"},{"type":"reasoning","encrypted_content":"native-suggestion"}]`, openai.ResponsesRequestInput},
		{provider.ContinuationAnthropic, `[{"type":"text","text":%q}]`, `[{"type":"thinking","thinking":"native-suggestion","signature":"signature"},{"type":"tool_use","id":"ui-call","name":"builtin_set_next_prompt","input":{"text":"suggestion-secret"}}]`, anthropic.RequestInput},
		{provider.ContinuationGoogle, `[{"text":%q}]`, `[{"functionCall":{"id":"ui-call","name":"builtin_set_next_prompt","args":{"text":"suggestion-secret"}},"thoughtSignature":"native-suggestion"}]`, google.RequestInput},
	} {
		for _, scenario := range []struct {
			name                     string
			answer, business, result bool
		}{
			{"final answer", true, false, true},
			{"mixed tools", true, true, true},
			{"suggestion only", false, false, true},
			{"interrupted call", true, false, false},
		} {
			t.Run(protocol.kind+"/"+scenario.name, func(t *testing.T) {
				ctx := context.Background()
				st := storetest.New(t)
				if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "test", Model: "model"}); err != nil {
					t.Fatal(err)
				}
				state := func(data string) *store.ProviderState {
					return &store.ProviderState{Provider: "test", Model: "model", Kind: protocol.kind, Data: json.RawMessage(data)}
				}
				history := []*store.Message{
					{Role: store.RoleUser, Parts: store.TextPart("Earlier task")},
					{Role: store.RoleAssistant, Parts: store.TextPart("canonical-before"), ProviderState: state(fmt.Sprintf(protocol.text, "native-before"))},
					{Role: store.RoleUser, Parts: store.TextPart("Current task")},
				}
				if scenario.business {
					// Earlier native segments must not shift onto later content after
					// removing the suggestion. Business calls/results remain paired.
					history = append(history,
						&store.Message{Role: store.RoleAssistant, Parts: []store.ContentPart{{Type: store.ContentPartToolUse, Name: tool.TimeGetCurrent, CallID: "time-call", Args: json.RawMessage(`{}`)}}, ProviderState: state(fmt.Sprintf(protocol.text, "native-business"))},
						&store.Message{Role: store.RoleTool, Parts: []store.ContentPart{{Type: store.ContentPartToolResult, Name: tool.TimeGetCurrent, CallID: "time-call", Ok: true, Content: `{"time":"business-result"}`}}},
					)
				}
				var parts []store.ContentPart
				if scenario.answer {
					parts = store.TextPart("final-answer")
				}
				parts = append(parts, store.ContentPart{Type: store.ContentPartToolUse, Name: name, CallID: "ui-call", Args: json.RawMessage(`{"text":"suggestion-secret"}`)})
				history = append(history, &store.Message{Role: store.RoleAssistant, Parts: parts, ProviderState: state(strings.ReplaceAll(protocol.call, tool.SetNextPrompt, name))})
				if scenario.result {
					history = append(history, &store.Message{Role: store.RoleTool, Parts: []store.ContentPart{{Type: store.ContentPartToolResult, Name: name, CallID: "ui-call", Ok: true, Content: `{"text":"suggestion-secret"}`}}})
				}
				history = append(history,
					&store.Message{Role: store.RoleUser, Parts: store.TextPart("Later task")},
					&store.Message{Role: store.RoleAssistant, Parts: store.TextPart("canonical-after"), ProviderState: state(fmt.Sprintf(protocol.text, "native-after"))},
					&store.Message{Role: store.RoleUser, Parts: store.TextPart("Follow-up")},
				)
				before := make([]*store.Message, len(history))
				for i, message := range history {
					copy := *message
					copy.Parts = store.CloneContentParts(message.Parts)
					copy.ProviderState = store.CloneProviderState(message.ProviderState)
					before[i] = &copy
				}
				// Even a caller supplying an obsolete definition cannot reenable
				// UI-only history or its opaque provider continuation.
				defs := []provider.ToolDef{{Name: name}, {Name: tool.TimeGetCurrent}}
				req, err := New(st, nil).BuildForProviderWithHistory(ctx, "s", "test", "model", "chat", defs, history, provider.ModelConfig{})
				if err != nil {
					t.Fatal(err)
				}
				wire, err := json.Marshal(protocol.wire(req).Messages)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{name, "ui-call", "suggestion-secret", "native-suggestion", "native-business"} {
					if strings.Contains(string(wire), forbidden) {
						t.Fatalf("filtered history leaked %q: %s", forbidden, wire)
					}
				}
				for _, retained := range []string{"native-before", "native-after", "Follow-up"} {
					if !strings.Contains(string(wire), retained) {
						t.Fatalf("unrelated history lost %q: %s", retained, wire)
					}
				}
				if scenario.answer && !strings.Contains(string(wire), "final-answer") {
					t.Fatalf("final answer was lost: %s", wire)
				}
				if scenario.business && (!strings.Contains(string(wire), tool.TimeGetCurrent) || !strings.Contains(string(wire), "business-result")) {
					t.Fatalf("business call/result was lost: %s", wire)
				}
				timeUses, timeResults := 0, 0
				for _, message := range req.Messages {
					if len(message.Parts) == 0 && message.Text == "" && len(message.Continuations) == 0 {
						t.Fatal("filter emitted an empty message")
					}
					for _, part := range message.Parts {
						if part.CallID == "time-call" && part.Type == provider.PartToolUse {
							timeUses++
						}
						if part.CallID == "time-call" && part.Type == provider.PartToolResult {
							timeResults++
						}
					}
				}
				if scenario.business && (timeUses != 1 || timeResults != 1) {
					t.Fatalf("unpaired business history: calls=%d results=%d", timeUses, timeResults)
				}
				if !reflect.DeepEqual(before, history) {
					t.Fatal("request filtering mutated canonical history")
				}
			})
		}
	}
}

func TestRetiredPlanHistoryExcludedWithoutToolDefinitions(t *testing.T) {
	for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
		if toolReplayedInHistory(mode, "builtin_plan_update", nil) {
			t.Fatalf("retired plan replayed without a tool allowlist in %s", mode)
		}
	}
}
