package contextbuilder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
)

type estimateTransport func(*http.Request) (*http.Response, error)

func (f estimateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEstimateMatchesTransmittedInputAndRetainsNativeReasoningAcrossTurns(t *testing.T) {
	reasoning := strings.Repeat("reasoning\n中文 ", 1000)
	quoted, _ := json.Marshal(reasoning)
	cases := []struct {
		protocol, kind, native string
		client                 func(*http.Client) provider.Client
	}{
		{"openai-compatible", provider.ContinuationOpenAIChat, `{"role":"assistant","reasoning_content":` + string(quoted) + `,"content":"native answer"}`, func(h *http.Client) provider.Client {
			return openai.New(openai.Config{BaseURL: "http://local", HTTPClient: h})
		}},
		{"openai-responses", provider.ContinuationOpenAIResponses, `[{"type":"reasoning","content":[{"type":"reasoning_text","text":` + string(quoted) + `}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"native answer"}]}]`, func(h *http.Client) provider.Client {
			return openai.NewResponses(openai.Config{BaseURL: "http://local", HTTPClient: h})
		}},
		{"anthropic", provider.ContinuationAnthropic, `[{"type":"thinking","thinking":` + string(quoted) + `,"signature":"signature"},{"type":"text","text":"native answer"}]`, func(h *http.Client) provider.Client { return anthropic.New(anthropic.Config{HTTPClient: h}) }},
		{"google", provider.ContinuationGoogle, `[{"text":` + string(quoted) + `,"thought":true,"thoughtSignature":"signature"},{"text":"native answer"}]`, func(h *http.Client) provider.Client { return google.New(google.Config{HTTPClient: h}) }},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			req := provider.Request{Model: "model", System: "system instruction", Tools: []provider.ToolDef{{Name: "inspect", InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"additionalProperties":false}`)}}, Messages: []provider.Message{{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartThought, Text: reasoning}, {Type: provider.PartText, Text: "display-only text"}}, Continuations: []provider.Continuation{{Kind: tc.kind, Data: json.RawMessage(tc.native)}}}}}
			during, err := EstimateRequest(req, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			// The history builder removes display thoughts, retaining canonical native state.
			req.Messages[0].Parts = req.Messages[0].Parts[1:]
			after, err := EstimateRequest(req, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			if during != after {
				t.Fatalf("turn end changed identical wire input: during=%+v after=%+v", during, after)
			}
			if after.MessageTokens < provider.EstimateTextTokens(reasoning) {
				t.Fatalf("reasoning omitted: %+v", after)
			}
			var transmitted []byte
			h := &http.Client{Transport: estimateTransport(func(r *http.Request) (*http.Response, error) {
				var readErr error
				transmitted, readErr = io.ReadAll(r.Body)
				if readErr != nil {
					return nil, readErr
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			ch, err := tc.client(h).Stream(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			for range ch {
			}
			if strings.Contains(string(transmitted), "display-only") {
				t.Fatal("display-only text sent alongside native replacement")
			}
			var body map[string]json.RawMessage
			if err = json.Unmarshal(transmitted, &body); err != nil {
				t.Fatal(err)
			}
			input := provider.WireInput{Tools: body["tools"]}
			switch tc.protocol {
			case "openai-compatible":
				var messages []json.RawMessage
				if err = json.Unmarshal(body["messages"], &messages); err != nil {
					t.Fatal(err)
				}
				var system struct {
					Content string `json:"content"`
				}
				json.Unmarshal(messages[0], &system)
				input.System = system.Content
				input.Messages = messages[1:]
			case "openai-responses":
				input.System = body["instructions"]
				input.Messages = body["input"]
			case "anthropic":
				input.System = body["system"]
				input.Messages = body["messages"]
			case "google":
				input.System = body["system_instruction"]
				input.Messages = body["contents"]
			}
			want, err := input.Estimate()
			if err != nil {
				t.Fatal(err)
			}
			if after != want {
				t.Fatalf("estimate=%+v HTTP input=%+v", after, want)
			}
			req.Config.Limits = &provider.ModelLimits{MaxOutputTokens: 1_000_000}
			limited, err := EstimateRequest(req, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			if limited != after {
				t.Fatal("generation limit counted as input")
			}
		})
	}
}

func TestEstimateUsesResponsesSanitizedToolsAndStateOnlyContinuation(t *testing.T) {
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartToolUse, CallID: "orphan", Name: "inspect", Args: json.RawMessage(`{"huge":"` + strings.Repeat("x", 10000) + `"}`)}}}}}
	got, err := EstimateRequest(req, "openai-responses")
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageTokens != 0 {
		t.Fatalf("unsent orphan counted: %+v", got)
	}
	req.Messages = []provider.Message{{Role: provider.RoleAssistant, Continuations: []provider.Continuation{{Kind: provider.ContinuationOpenAIResponses, Data: json.RawMessage(`[{"type":"reasoning","content":[{"type":"reasoning_text","text":"` + strings.Repeat("x", 10000) + `"}]}]`)}}}}
	got, err = EstimateRequest(req, "openai-responses")
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageTokens < 2500 {
		t.Fatalf("state-only continuation omitted: %+v", got)
	}
}
