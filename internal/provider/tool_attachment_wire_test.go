package provider_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
)

type attachmentRoundTrip func(*http.Request) (*http.Response, error)

func (f attachmentRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise actual HTTP request serialization (including Chat's tool-pair
// sanitizer), without a network listener or a live model/account.
func TestToolAttachmentWireTimeline(t *testing.T) {
	tests := []struct {
		name, field, kind, firstState, lastState, response string
		client                                             func(*http.Client) provider.Client
	}{
		{"chat", "messages", provider.ContinuationOpenAIChat,
			`{"role":"assistant","reasoning_content":"state-observe","tool_calls":[{"id":"a","type":"function","function":{"name":"a","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"b","arguments":"{}"}}]}`,
			`{"role":"assistant","reasoning_content":"state-click","tool_calls":[{"id":"c","type":"function","function":{"name":"c","arguments":"{}"}}]}`,
			"data: [DONE]\n\n",
			func(h *http.Client) provider.Client {
				return openai.New(openai.Config{BaseURL: "https://test.invalid", HTTPClient: h})
			}},
		{"responses", "input", provider.ContinuationOpenAIResponses,
			`[{"type":"reasoning","id":"rs_a","encrypted_content":"state-observe"},{"type":"function_call","call_id":"a","name":"a","arguments":"{}"},{"type":"function_call","call_id":"b","name":"b","arguments":"{}"}]`,
			`[{"type":"reasoning","id":"rs_c","encrypted_content":"state-click"},{"type":"function_call","call_id":"c","name":"c","arguments":"{}"}]`,
			"data: {\"type\":\"response.completed\"}\n\n",
			func(h *http.Client) provider.Client {
				return openai.NewResponses(openai.Config{BaseURL: "https://test.invalid", HTTPClient: h})
			}},
		{"anthropic", "messages", provider.ContinuationAnthropic,
			`[{"type":"thinking","thinking":"observe","signature":"state-observe"},{"type":"tool_use","id":"a","name":"a","input":{}},{"type":"tool_use","id":"b","name":"b","input":{}}]`,
			`[{"type":"thinking","thinking":"click","signature":"state-click"},{"type":"tool_use","id":"c","name":"c","input":{}}]`,
			"data: {\"type\":\"message_stop\"}\n\n",
			func(h *http.Client) provider.Client {
				return anthropic.New(anthropic.Config{BaseURL: "https://test.invalid", HTTPClient: h})
			}},
		{"google", "contents", provider.ContinuationGoogle,
			`[{"thoughtSignature":"state-observe","functionCall":{"id":"a","name":"a","args":{}}},{"functionCall":{"id":"b","name":"b","args":{}}}]`,
			`[{"thoughtSignature":"state-click","functionCall":{"id":"c","name":"c","args":{}}}]`,
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n",
			func(h *http.Client) provider.Client {
				return google.New(google.Config{BaseURL: "https://test.invalid", HTTPClient: h})
			}},
	}
	for _, tt := range tests {
		for _, native := range []bool{false, true} {
			for _, preSplit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/native=%v/split=%v", tt.name, native, preSplit), func(t *testing.T) {
					msg := provider.Message{Role: provider.RoleAssistant, Parts: []provider.Part{
						{Type: provider.PartToolUse, CallID: "a", Name: "a"},
						{Type: provider.PartToolUse, CallID: "b", Name: "b"},
						{Type: provider.PartToolResult, CallID: "a", Name: "a", Ok: true, Content: "one"},
						{Type: provider.PartText, CallID: "a", Text: "Source tool call: a"},
						{Type: provider.PartImage, CallID: "a", MIME: "image/png", Data: []byte("before")},
						{Type: provider.PartToolResult, CallID: "b", Name: "b", Ok: true, Content: "two"},
						{Type: provider.PartText, CallID: "b", Text: "Source tool call: b"},
						{Type: provider.PartImage, CallID: "b", MIME: "image/png", Data: []byte("other")},
						{Type: provider.PartToolUse, CallID: "c", Name: "c"},
						{Type: provider.PartToolResult, CallID: "c", Name: "c", Ok: true, Content: "delivered"},
					}}
					if native {
						msg.Continuations = []provider.Continuation{{Kind: tt.kind, Data: json.RawMessage(tt.firstState)}, {Kind: tt.kind, Data: json.RawMessage(tt.lastState)}}
					}
					messages := []provider.Message{msg}
					if preSplit {
						messages = provider.SplitMessage(msg)
					}
					bodies := make(chan []byte, 1)
					h := &http.Client{Transport: attachmentRoundTrip(func(r *http.Request) (*http.Response, error) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							return nil, err
						}
						bodies <- body
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tt.response))}, nil
					})}
					chunks, err := tt.client(h).Stream(context.Background(), provider.Request{Model: "test", Messages: messages})
					if err != nil {
						t.Fatal(err)
					}
					for chunk := range chunks {
						if chunk.Err != nil {
							t.Fatal(chunk.Err)
						}
					}
					body := <-bodies
					var request map[string]json.RawMessage
					if err := json.Unmarshal(body, &request); err != nil {
						t.Fatal(err)
					}
					var items []map[string]any
					if err := json.Unmarshal(request[tt.field], &items); err != nil {
						t.Fatal(err)
					}
					var order []string
					for _, item := range items {
						if calls, ok := item["tool_calls"].([]any); ok {
							for _, call := range calls {
								order = append(order, "use:"+call.(map[string]any)["id"].(string))
							}
						}
						if item["role"] == "tool" {
							order = append(order, "result:"+item["tool_call_id"].(string))
						}
						blocks := []any{item}
						if content, ok := item["content"].([]any); ok {
							blocks = content
						}
						if parts, ok := item["parts"].([]any); ok {
							blocks = parts
						}
						for _, raw := range blocks {
							block := raw.(map[string]any)
							switch block["type"] {
							case "function_call":
								order = append(order, "use:"+block["call_id"].(string))
							case "function_call_output":
								order = append(order, "result:"+block["call_id"].(string))
							case "tool_use":
								order = append(order, "use:"+block["id"].(string))
							case "tool_result":
								order = append(order, "result:"+block["tool_use_id"].(string))
							}
							for _, field := range []string{"functionCall", "functionResponse"} {
								if f, ok := block[field].(map[string]any); ok {
									prefix := "use:"
									if field == "functionResponse" {
										prefix = "result:"
									}
									order = append(order, prefix+f["name"].(string))
								}
							}
							var encoded string
							if image, ok := block["image_url"].(map[string]any); ok {
								encoded = image["url"].(string)
							}
							if image, ok := block["image_url"].(string); ok {
								encoded = image
							}
							if source, ok := block["source"].(map[string]any); ok {
								encoded, _ = source["data"].(string)
							}
							if source, ok := block["inlineData"].(map[string]any); ok {
								encoded, _ = source["data"].(string)
							}
							if encoded != "" {
								if item["role"] != "user" {
									t.Fatalf("image not user input: %s", body)
								}
								encoded = strings.TrimPrefix(encoded, "data:image/png;base64,")
								data, err := base64.StdEncoding.DecodeString(encoded)
								if err != nil {
									t.Fatal(err)
								}
								order = append(order, "image:"+string(data))
							}
						}
					}
					want := []string{"use:a", "use:b", "result:a", "result:b", "image:before", "image:other", "use:c", "result:c"}
					if !reflect.DeepEqual(order, want) {
						t.Fatalf("wire order=%v; want %v\n%s", order, want, body)
					}
					if native {
						for _, token := range []string{"state-observe", "state-click"} {
							if strings.Count(string(body), token) != 1 {
								t.Fatalf("native state lost/duplicated: %s", body)
							}
						}
					}
				})
			}
		}
	}
}
