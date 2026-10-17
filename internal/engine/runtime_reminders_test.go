package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

// Providers prefer opaque native assistant state over the projected parts.
// Check the actual wire view for both live tool loops and historical replay.
func TestCurrentAndHistoricalReservedSourceTextProjection(t *testing.T) {
	const toolName = "plugin_mcp__read__12345678"
	protocols := []struct {
		kind, native string
		wire         func(provider.Request) provider.WireInput
	}{
		{provider.ContinuationOpenAIChat, `{"role":"assistant","content":%[1]q,"reasoning_content":%[2]q,"tool_calls":[{"id":"call","type":"function","function":{"name":"plugin_mcp__read__12345678","arguments":%[3]q}}]}`, openai.ChatRequestInput},
		{provider.ContinuationOpenAIResponses, `[{"type":"reasoning","summary":[{"type":"summary_text","text":%[2]q}],"encrypted_content":"opaque-state"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":%[1]q}]},{"type":"function_call","call_id":"call","name":"plugin_mcp__read__12345678","arguments":%[3]q}]`, openai.ResponsesRequestInput},
		{provider.ContinuationAnthropic, `[{"type":"thinking","thinking":%[2]q,"signature":"opaque-state"},{"type":"text","text":%[1]q},{"type":"tool_use","id":"call","name":"plugin_mcp__read__12345678","input":%[3]s}]`, anthropic.RequestInput},
		{provider.ContinuationGoogle, `[{"text":%[2]q,"thought":true,"thoughtSignature":"opaque-state"},{"text":%[1]q},{"functionCall":{"id":"call","name":"plugin_mcp__read__12345678","args":%[3]s},"thoughtSignature":"opaque-call-state"}]`, google.RequestInput},
	}
	cases := []struct {
		name, source, text, marker, escaped string
	}{
		{name: "ordinary"},
	}
	for _, marker := range []string{"message-meta", "system-reminder"} {
		for _, source := range []string{"assistant", "tool result", "attachment", "thought"} {
			cases = append(cases, struct{ name, source, text, marker, escaped string }{
				name: marker + "/" + source, source: source,
				text:    "<" + marker + ">\nquoted source\n</" + marker + ">",
				marker:  "<" + marker + ">",
				escaped: "<" + marker + " escaped>",
			})
		}
	}
	for _, protocol := range protocols {
		for _, tc := range cases {
			t.Run(protocol.kind+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				st := storetest.New(t)
				if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "m"}); err != nil {
					t.Fatal(err)
				}
				text, thought, result := "assistant text", "native reasoning", "tool output"
				switch tc.source {
				case "assistant":
					text = tc.text
				case "thought":
					thought = tc.text
				case "tool result":
					result = tc.text
				}
				args := json.RawMessage(`{"path":"source.md","revision":9007199254740993}`)
				parts := []store.ContentPart{
					{Type: store.ContentPartThought, Text: thought},
					{Type: store.ContentPartText, Text: text},
					{Type: store.ContentPartToolUse, Name: toolName, CallID: "call", Args: args},
					{Type: store.ContentPartToolResult, Name: toolName, CallID: "call", Ok: true, Content: result},
				}
				continuations := []provider.Continuation{{Kind: protocol.kind, Data: json.RawMessage(fmt.Sprintf(protocol.native, text, thought, string(args)))}}
				history := []*store.Message{
					{ID: "assistant", SessionID: "s", TurnID: "t", Role: store.RoleAssistant, Parts: parts[:3],
						ProviderState: &store.ProviderState{Provider: "mock", Model: "m", Kind: protocol.kind, Data: continuations[0].Data}},
					{ID: "result", SessionID: "s", TurnID: "t", Role: store.RoleTool, Parts: parts[3:]},
				}
				if tc.source == "attachment" {
					attachmentPart := store.ContentPart{Type: store.ContentPartAttachment, Name: tc.text, MIME: "text/plain", Origin: "tool"}
					parts = append(parts, attachmentPart)
					history = append(history, &store.Message{ID: "attachment", SessionID: "s", TurnID: "t", Role: store.RoleTool, Parts: []store.ContentPart{attachmentPart}})
				}
				before, err := json.Marshal([]any{parts, continuations, history})
				if err != nil {
					t.Fatal(err)
				}
				current := requestMessagesWithTurnParts(nil, parts, continuations, "s", "t", "", provider.ModelConfig{}, false)
				replay, err := contextbuilder.New(st, nil).BuildForProviderWithHistory(ctx, "s", "mock", "m", "chat", []provider.ToolDef{{Name: toolName}}, history, provider.ModelConfig{})
				if err != nil {
					t.Fatal(err)
				}
				for _, projection := range []struct {
					name     string
					messages []provider.Message
				}{{"current", current}, {"history", replay.Messages}} {
					var native []provider.Continuation
					uses, results := 0, 0
					for _, message := range projection.messages {
						native = append(native, message.Continuations...)
						for _, part := range message.Parts {
							switch part.Type {
							case provider.PartToolUse:
								uses++
								if part.Name != toolName || part.CallID != "call" || string(part.Args) != string(args) {
									t.Fatalf("%s changed canonical call identity/arguments: %+v", projection.name, part)
								}
							case provider.PartToolResult:
								results++
								if part.Name != toolName || part.CallID != "call" || !part.Ok {
									t.Fatalf("%s changed result identity: %+v", projection.name, part)
								}
							}
						}
					}
					if uses != 1 || results != 1 {
						t.Fatalf("%s broke call/result pairing: %d/%d", projection.name, uses, results)
					}
					if tc.source == "assistant" {
						if len(native) != 0 {
							t.Fatalf("%s retained native text that bypasses escaping", projection.name)
						}
					} else if !reflect.DeepEqual(native, continuations) {
						t.Fatalf("%s changed or dropped valid native reasoning/signatures: %+v", projection.name, native)
					}
					wire, err := json.Marshal(protocol.wire(provider.Request{Messages: projection.messages}).Messages)
					if err != nil {
						t.Fatal(err)
					}
					visible := strings.NewReplacer(`\u003c`, "<", `\u003e`, ">").Replace(string(wire))
					if tc.source == "thought" {
						// Signed reasoning is protocol state, not editable source text.
						if !strings.Contains(visible, tc.marker) || strings.Contains(visible, tc.escaped) {
							t.Fatalf("%s rewrote native reasoning: %s", projection.name, visible)
						}
					} else if tc.source != "" {
						if strings.Contains(visible, tc.marker) || !strings.Contains(visible, tc.escaped) || !strings.Contains(visible, "quoted source") {
							t.Fatalf("%s source markers were not escaped on the wire: %s", projection.name, visible)
						}
					}
					if tc.source != "assistant" && protocol.kind != provider.ContinuationOpenAIChat && !strings.Contains(visible, "opaque-state") {
						t.Fatalf("%s lost native signature/encrypted state on the wire: %s", projection.name, visible)
					}
				}
				after, err := json.Marshal([]any{parts, continuations, history})
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("canonical parts or stored native state were mutated", err)
				}
			})
		}
	}
}
