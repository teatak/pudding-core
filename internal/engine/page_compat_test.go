package engine

import (
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
)

// Legacy execution is allowed for six months; every following model request
// uses the canonical names, including the current turn's native continuation.
func TestLegacyPageCurrentTurnContinuations(t *testing.T) {
	for _, protocol := range []struct {
		kind, native string
		wire         func(provider.Request) provider.WireInput
	}{
		{provider.ContinuationOpenAIChat, `{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":%q,"arguments":"{}"}}]}`, openai.ChatRequestInput},
		{provider.ContinuationOpenAIResponses, `[{"type":"function_call","call_id":"call","name":%q,"arguments":"{}"}]`, openai.ResponsesRequestInput},
		{provider.ContinuationAnthropic, `[{"type":"tool_use","id":"call","name":%q,"input":{}}]`, anthropic.RequestInput},
		{provider.ContinuationGoogle, `[{"functionCall":{"id":"call","name":%q,"args":{}}}]`, google.RequestInput},
	} {
		for _, action := range []string{"create", "read", "edit", "rename"} {
			for _, prefix := range []string{"doc", "note", "page", "document"} {
				name := "builtin_" + prefix + "_" + action
				t.Run(protocol.kind+"/"+name, func(t *testing.T) {
					current := name
					if prefix == "doc" || prefix == "note" {
						current = "builtin_page_" + action
					}
					parts := []store.ContentPart{
						{Type: store.ContentPartToolUse, Name: name, CallID: "call", Args: json.RawMessage(`{}`)},
						{Type: store.ContentPartToolResult, Name: name, CallID: "call", Ok: true, Content: `{"ok":true}`},
					}
					continuations := []provider.Continuation{{Kind: protocol.kind, Data: json.RawMessage(fmt.Sprintf(protocol.native, name))}}
					before, _ := json.Marshal([]any{parts, continuations})
					messages := requestMessagesWithTurnParts(nil, parts, continuations, "s", "t", "", provider.ModelConfig{}, false)
					count := 0
					for _, message := range messages {
						if name != current && len(message.Continuations) != 0 {
							t.Fatal("legacy native state survived projection")
						}
						for _, part := range message.Parts {
							if part.Type == provider.PartToolUse || part.Type == provider.PartToolResult {
								count++
								if part.Name != current || part.CallID != "call" {
									t.Fatalf("wrong tool identity: %+v", part)
								}
							}
						}
					}
					wire, err := json.Marshal(protocol.wire(provider.Request{Messages: messages}).Messages)
					if err != nil || count != 2 || !strings.Contains(string(wire), current) || strings.Contains(string(wire), "builtin_doc_") || strings.Contains(string(wire), "builtin_note_") {
						t.Fatalf("incorrect continuation: %s, %v", wire, err)
					}
					after, _ := json.Marshal([]any{parts, continuations})
					if !reflect.DeepEqual(before, after) {
						t.Fatal("stored parts or native state were mutated")
					}
				})
			}
		}
	}
}
