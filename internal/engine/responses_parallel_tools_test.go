package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

// Replay the failure shape: two same-name tools, distinct output_index/item_id/
// call_id values, and interleaved argument deltas. A zero index on every chunk
// makes the accumulator overwrite the first call and produce an orphaned input.
func TestResponsesParallelToolsSurviveExecutionAndHistoryReplay(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicit wire tags keep validation independent of adapter structs.
		var body struct {
			Input []struct {
				Type      string `json:"type"`
				CallID    string `json:"call_id"`
				Arguments string `json:"arguments"`
				Output    string `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frames := []string{
				`{"type":"response.reasoning_text.delta","output_index":0,"delta":"Check both timezones."}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_0","content":[{"type":"reasoning_text","text":"Check both timezones."}]}}`,
				`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"builtin_time_get_current","arguments":""}}`,
				`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"builtin_time_get_current","arguments":""}}`,
				`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_a","delta":"{\"timezone\":"}`,
				`{"type":"response.function_call_arguments.delta","output_index":2,"item_id":"fc_b","delta":"{\"timezone\":\"Asia/Singapore\"}"}`,
				`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_a","delta":"\"UTC\"}"}`,
				`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"builtin_time_get_current","arguments":"{\"timezone\":\"UTC\"}"}}`,
				`{"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"builtin_time_get_current","arguments":"{\"timezone\":\"Asia/Singapore\"}"}}`,
				`{"type":"response.completed"}`,
			}
			for _, frame := range frames {
				fmt.Fprintf(w, "data: %s\n\n", frame)
			}
			return
		}
		calls, outputs := map[string]int{}, map[string]int{}
		for _, item := range body.Input {
			switch item.Type {
			case "function_call":
				calls[item.CallID]++
			case "function_call_output":
				if item.Output != "" {
					outputs[item.CallID]++
				}
			}
		}
		for _, id := range []string{"call_a", "call_b"} {
			if outputs[id] != 1 || calls[id] != 1 {
				http.Error(w, "No tool output found for tool call "+id, http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Both checked.\"}\n\n"+
			"data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer srv.Close()

	ctx := context.Background()
	ms := memstore.New()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Title: "Parallel tools", Provider: "deepseek", Model: "deepseek-flash"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID: "deepseek", Protocol: "openai-responses",
		Models: []store.ProviderModel{{ID: "deepseek-flash", Capabilities: &store.ModelCaps{Tools: true}}},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &recordingToolRunner{
		defs:   []provider.ToolDef{{Name: tool.TimeGetCurrent, Capability: store.ModeChat, InputSchema: json.RawMessage(`{"type":"object"}`)}},
		result: tool.Result{Ok: true, Content: `{"time":"now"}`},
	}
	client := openai.NewResponses(openai.Config{BaseURL: srv.URL, HTTPClient: &http.Client{Timeout: 2 * time.Second}})
	eng := New(ms, event.NewHub(), mapResolver{"deepseek": client}, ms, WithTools(runner))
	for _, id := range []string{"first-turn", "replay-turn"} {
		if _, err := eng.Submit(ctx, SubmitInput{SessionID: "s1", ClientMessageID: id, Text: "Check times"}); err != nil {
			t.Fatal(err)
		}
		eng.Wait()
		events, err := ms.EventsAfter(ctx, "s1", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if last := events[len(events)-1]; last.Kind != event.TurnCompleted {
			t.Fatalf("%s did not complete: %s", id, last.Error)
		}
	}
	if requests.Load() != 3 || len(runner.calls) != 2 {
		t.Fatalf("provider calls=%d tool calls=%+v", requests.Load(), runner.calls)
	}
	got := map[string]string{}
	for _, call := range runner.calls {
		got[call.CallID] = string(call.Args)
	}
	if want := map[string]string{"call_a": `{"timezone":"UTC"}`, "call_b": `{"timezone":"Asia/Singapore"}`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool identities/arguments mixed: got %v want %v", got, want)
	}
}
