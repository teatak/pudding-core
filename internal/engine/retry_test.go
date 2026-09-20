package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

type retryClient struct {
	mu       sync.Mutex
	requests []provider.Request
}

func TestRetryRemainsCancellable(t *testing.T) {
	eng, st, _, sid := newTestEngine(t, mock.WithScript([]string{"wait"}), mock.WithDelay(time.Minute))
	defer eng.Stop()
	ctx := context.Background()
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "failed", ClientMessageID: "original", UserMessageID: "user", UserText: "request"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "failed", Status: store.TurnFailed, Error: "503"}); err != nil {
		t.Fatal(err)
	}
	retry, err := eng.Retry(ctx, sid, "failed", "retry")
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Cancel(sid); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	turn, err := st.GetConversationTurn(ctx, sid, retry.TurnID)
	if err != nil || turn.Status != store.TurnCancelled || turn.RetryOfTurnID != "failed" {
		t.Fatalf("retry did not cancel: %+v %v", turn, err)
	}
	if _, err := eng.Retry(ctx, sid, retry.TurnID, "retry-cancelled"); !errors.Is(err, store.ErrInvalidRetry) {
		t.Fatalf("cancelled attempt was retryable: %v", err)
	}
}

func (*retryClient) Name() string { return "retry" }
func (c *retryClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 3)
	switch len(c.requests) {
	case 1:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "done_tool", Name: tool.TimeGetCurrent, ArgsDelta: `{}`}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Delta: "partial output"}
		out <- provider.Chunk{Err: errors.New("status 503")}
	default:
		out <- provider.Chunk{Delta: "finished"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func TestRetryUsesCanonicalHistoryWithoutReplayingTools(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	client := &retryClient{}
	runner := &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.TimeGetCurrent, InputSchema: json.RawMessage(`{"type":"object"}`), Capability: store.ModeChat}}, result: tool.Result{Ok: true, Content: "completed exactly once"}}
	eng := New(st, event.NewHub(), mapResolver{"retry": client}, st, WithTools(runner))
	defer eng.Stop()
	if err := st.CreateSession(ctx, &store.Session{ID: "s", Title: "test", Provider: "retry", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProviderProfile(ctx, &store.ProviderProfile{ID: "retry", Protocol: "openai-compatible", BaseURL: "http://example.invalid", Models: []store.ProviderModel{{ID: "model"}}}); err != nil {
		t.Fatal(err)
	}
	first, err := eng.Submit(ctx, SubmitInput{SessionID: "s", ClientMessageID: "original", Text: "get time"})
	if err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	retried, err := eng.Retry(ctx, "s", first.TurnID, "retry-1")
	if err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	if retried.TurnID == first.TurnID || retried.UserMessageID != "" {
		t.Fatal("retry reused lifecycle or created user message")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("tool replayed %d times", len(runner.calls))
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests=%d", len(client.requests))
	}
	data, _ := json.Marshal(client.requests[2].Messages)
	for _, text := range []string{"get time", "completed exactly once", "partial output"} {
		if !bytes.Contains(data, []byte(text)) {
			t.Fatalf("missing canonical context %q: %s", text, data)
		}
	}
	duplicate, err := eng.Retry(ctx, "s", first.TurnID, "retry-1")
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	eng.Wait()
	if len(client.requests) != 3 {
		t.Fatal("duplicate retry called model again")
	}
}
