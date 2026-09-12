package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

type deepSeekResponsesTransport func(*http.Request) (*http.Response, error)

func (f deepSeekResponsesTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// DeepSeek Responses is stateless and returns plaintext reasoning content.
// Exercise the shared adapter at the HTTP boundary, including the next tool step.
// Contract: https://api-docs.deepseek.com/zh-cn/guides/responses_api/
func TestDeepSeekResponsesToolRoundTrip(t *testing.T) {
	const reasoning = `{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"Inspect the image first."}]}`
	const call = `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"image\"}"}`
	type capturedRequest struct {
		url, authorization string
		body               map[string]json.RawMessage
	}
	requests := make(chan capturedRequest, 3)
	step := 0
	client := NewResponses(Config{
		BaseURL: "https://api.deepseek.com/", APIKey: "fixture-key",
		HTTPClient: &http.Client{Transport: deepSeekResponsesTransport(func(r *http.Request) (*http.Response, error) {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return nil, err
			}
			requests <- capturedRequest{r.URL.String(), r.Header.Get("Authorization"), body}
			stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Found it.\"}\n\n" +
				"data: {\"type\":\"response.completed\"}\n\n"
			if step == 0 {
				stream = strings.Join([]string{
					`data: {"type":"response.reasoning_text.delta","delta":"Inspect the image first."}`,
					`data: {"type":"response.output_item.done","item":` + reasoning + `}`,
					`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}`,
					`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"q\":\"image\"}"}`,
					`data: {"type":"response.output_item.done","item":` + call + `}`,
					`data: {"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":25},"output_tokens_details":{"reasoning_tokens":5}}}}`,
				}, "\n\n") + "\n\n" // No [DONE] sentinel in DeepSeek Responses.
			}
			step++
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, nil
		})},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := provider.Request{
		Model: "deepseek-flash", System: "Help identify the image.",
		Config: provider.ModelConfig{
			Limits:          &provider.ModelLimits{MaxOutputTokens: 384_000},
			ProviderOptions: &provider.ModelProviderOptions{OpenAI: map[string]any{"reasoning_effort": "high"}},
		},
		Tools: []provider.ToolDef{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)}},
		Messages: []provider.Message{{Role: provider.RoleUser, Parts: []provider.Part{
			{Type: provider.PartText, Text: "Identify this."},
			{Type: provider.PartImage, MIME: "image/png", Data: []byte("png")},
		}}},
	}
	run := func(request provider.Request) []provider.Chunk {
		t.Helper()
		ch, err := client.Stream(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		chunks := collect(t, ch)
		for _, chunk := range chunks {
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
		}
		return chunks
	}
	chunks := run(req)
	first := <-requests
	if first.url != "https://api.deepseek.com/responses" || first.authorization != "Bearer fixture-key" {
		t.Fatalf("wrong DeepSeek endpoint/auth: %+v", first)
	}
	if string(first.body["model"]) != `"deepseek-flash"` || string(first.body["max_output_tokens"]) != "384000" || string(first.body["reasoning"]) != `{"effort":"high"}` || first.body["temperature"] != nil {
		t.Fatalf("wrong request options: %s", first.body)
	}
	if !strings.Contains(string(first.body["input"]), `"image_url":"data:image/png;base64,cG5n"`) || !strings.Contains(string(first.body["tools"]), `"name":"lookup"`) {
		t.Fatalf("missing vision/tools: %s", first.body)
	}
	done := chunks[len(chunks)-1]
	if !done.Done || done.Finish != provider.FinishToolCalls || done.Continuation == nil {
		t.Fatalf("missing tool completion: %+v", chunks)
	}
	if chunks[0].Part != provider.PartThought || chunks[0].Delta != "Inspect the image first." || chunks[1].Tool == nil || chunks[1].Tool.CallID != "call_1" || chunks[2].Tool.ArgsDelta != `{"q":"image"}` {
		t.Fatalf("wrong thought/tool deltas: %+v", chunks)
	}
	usage := chunks[len(chunks)-2].Usage
	if usage == nil || usage.InputCachedTokens != 25 || usage.OutputReasoningTokens != 5 {
		t.Fatalf("wrong DeepSeek usage: %+v", usage)
	}
	req.Messages = append(req.Messages, provider.Message{
		Role: provider.RoleAssistant,
		Parts: []provider.Part{
			{Type: provider.PartThought, Text: "Display-only thought"},
			{Type: provider.PartToolUse, CallID: "call_1", Name: "lookup", Args: json.RawMessage(`{"q":"image"}`)},
			{Type: provider.PartToolResult, CallID: "call_1", Name: "lookup", Ok: true, Content: `{"label":"cat"}`},
		},
		Continuations: []provider.Continuation{*done.Continuation},
	})
	chunks = run(req)
	second := <-requests
	var inputs []json.RawMessage
	if err := json.Unmarshal(second.body["input"], &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 4 || string(inputs[1]) != reasoning || string(inputs[2]) != call {
		t.Fatalf("plaintext reasoning/tool call not replayed verbatim: %s", inputs)
	}
	var output map[string]string
	if err := json.Unmarshal(inputs[3], &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, map[string]string{"type": "function_call_output", "call_id": "call_1", "output": `{"label":"cat"}`}) {
		t.Fatalf("wrong tool result: %+v", output)
	}
	if second.body["previous_response_id"] != nil || chunksText(chunks) != "Found it." || chunks[len(chunks)-1].Finish != provider.FinishStop {
		t.Fatalf("stateless response failed: %s %+v", second.body, chunks)
	}
	run(provider.Request{Model: "deepseek-v4-pro", Messages: []provider.Message{{Role: provider.RoleUser, Text: "New session"}}})
	third := <-requests
	if string(third.body["input"]) != `[{"role":"user","content":"New session"}]` {
		t.Fatalf("client leaked prior session history: %s", third.body["input"])
	}
}

func TestResponsesIncompleteDoesNotCompleteTurn(t *testing.T) {
	out := make(chan provider.Chunk, 2)
	err := readResponsesSSE(context.Background(), strings.NewReader(
		"data: {\"type\":\"response.incomplete\",\"response\":{\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n",
	), out)
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") || len(out) != 0 {
		t.Fatalf("incomplete response must fail, got err=%v chunks=%d", err, len(out))
	}
}

func TestResponsesCancelInterruptsHTTPStream(t *testing.T) {
	serverCanceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"Thinking\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(serverCanceled)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := NewResponses(Config{BaseURL: srv.URL}).Stream(ctx, provider.Request{Model: "deepseek-flash"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case first := <-ch:
		if first.Delta != "Thinking" {
			t.Fatalf("unexpected first chunk: %+v", first)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case <-serverCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not abort HTTP request")
	}
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if chunk.Done {
				t.Fatal("canceled response completed successfully")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("canceled stream did not close")
		}
	}
}
