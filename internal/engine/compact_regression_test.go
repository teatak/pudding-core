package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
)

type compactGateClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *compactGateClient) Name() string { return "mock" }
func (c *compactGateClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	answer := "new turn answer"
	if req.System == compactSystemPrompt {
		c.once.Do(func() { close(c.started) })
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		answer = "summary of the old snapshot"
	}
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Delta: answer}
	out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	close(out)
	return out, nil
}

func TestCompactRegressionSnapshotPreservesConcurrentCompletedTurn(t *testing.T) {
	eng, _, _, sid := newTestEngine(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateSession(ctx, &store.Session{ID: sid, Title: "audit", Provider: "mock", Model: "mock-model"}); err != nil {
		t.Fatal(err)
	}
	eng.store = db
	eng.rebuildBuilder()
	gate := &compactGateClient{started: make(chan struct{}), release: make(chan struct{})}
	eng.resolver = registry.Static(gate)
	defer eng.Stop()
	for i := 1; i <= 3; i++ {
		appendEngineTestTurn(t, db, sid, fmt.Sprint(i), strings.Repeat(fmt.Sprintf("old user %d ", i), 100), "old answer")
	}
	compactDone := make(chan error, 1)
	compactCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() { _, err := eng.Compact(compactCtx, CompactInput{SessionID: sid}); compactDone <- err }()
	<-gate.started
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "concurrent-new", Text: "NEW_FACT_WHILE_COMPACTING"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, db, sid)
	eng.Wait()
	close(gate.release)
	if err := <-compactDone; !errors.Is(err, store.ErrHistoryChanged) {
		t.Fatalf("want stale snapshot rejection, got %v", err)
	}
	msgs, err := db.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	canonicalHas := false
	for _, m := range msgs {
		canonicalHas = canonicalHas || strings.Contains(m.Text, "NEW_FACT_WHILE_COMPACTING")
	}
	req, err := eng.builder.Build(ctx, sid, "mock-model", "chat")
	if err != nil {
		t.Fatal(err)
	}
	visibleHas := false
	for _, m := range req.Messages {
		visibleHas = visibleHas || strings.Contains(m.Text, "NEW_FACT_WHILE_COMPACTING")
	}
	t.Logf("canonical_has_new_turn=%v provider_request_has_new_turn=%v", canonicalHas, visibleHas)
	if !canonicalHas || !visibleHas {
		t.Fatal("concurrent completed turn disappears from provider context after compact commit")
	}
}

func TestCompactRegressionInputPreservesPreviousSummaryTail(t *testing.T) {
	text := strings.Repeat("A", 4100) + "\n## TODO / Open Questions\nCRITICAL_UNFINISHED_TASK"
	msg := &store.Message{ID: "summary_old", Role: store.RoleSummary, Text: text, Parts: store.TextPart(text)}
	dump := compactMessageRecord(msg, compactMessageText(msg))
	if !strings.Contains(dump, "CRITICAL_UNFINISHED_TASK") {
		t.Fatal("previous summary TODO is removed before the summarizer receives it")
	}
}

func TestCompactPreflightAppliesSteerAndDrainsQueuedInput(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	title := "named"
	_, _ = ms.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title})
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000, Limits: &store.ModelLimits{MaxOutputTokens: 1024}}}}); err != nil {
		t.Fatal(err)
	}
	if err := ms.SetSettings(ctx, map[string]string{config.SettingCompactAutoThresholdPercent: "50"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("old ", 3000), "old answer")
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var requests []provider.Request
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		if req.System == compactSystemPrompt {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return compactChunks(provider.Chunk{Delta: "Previous tasks completed.", Done: true, Finish: provider.FinishStop}), nil
		}
		requests = append(requests, req)
		return compactChunks(provider.Chunk{Delta: "Done."}, provider.Chunk{Done: true, Finish: provider.FinishStop}), nil
	}))
	submitted, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "current", Text: "CURRENT_TASK"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("preflight did not compact before the first ordinary request")
	}
	if _, err := eng.Steer(ctx, SteerInput{SessionID: sid, TurnID: submitted.TurnID, ClientMessageID: "steer", Text: "NEW_STEERING_FACT", Parts: store.TextPart("NEW_STEERING_FACT")}); err != nil {
		t.Fatal(err)
	}
	if queued, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "queued", Text: "NEXT_QUEUED_TASK"}); err != nil || !queued.Queued {
		t.Fatalf("queue during compact: %+v %v", queued, err)
	}
	close(release)
	eng.Wait()
	if len(requests) != 2 {
		t.Fatalf("want two ordinary requests, got %d", len(requests))
	}
	first, _ := json.Marshal(requests[0].Messages)
	second, _ := json.Marshal(requests[1].Messages)
	if !strings.Contains(string(first), "CURRENT_TASK") || !strings.Contains(string(first), "NEW_STEERING_FACT") || !strings.Contains(string(second), "NEXT_QUEUED_TASK") {
		t.Fatal("compaction lost a steer or queued input")
	}
	for _, req := range requests {
		if contextbuilder.EstimateRequest(req).Total() > contextInputLimit(req.Config, "openai-compatible") {
			t.Fatal("queued request bypassed the context budget")
		}
	}
}

func TestCompactRegressionInputPreservesToolArguments(t *testing.T) {
	parts := []store.ContentPart{
		{Type: store.ContentPartToolUse, Name: "builtin_command_run", CallID: "c", Args: json.RawMessage("{\"command\":\"deploy --target UNIQUE_TARGET\"}")},
		{Type: store.ContentPartToolResult, Name: "builtin_command_run", CallID: "c", Ok: true, Content: "{\"exitCode\":0}"},
	}
	msg := &store.Message{ID: "toolmsg", Role: store.RoleAssistant, Parts: parts}
	dump := compactMessageRecord(msg, compactMessageText(msg))
	if !strings.Contains(dump, "UNIQUE_TARGET") {
		t.Fatalf("tool arguments absent from summary input: %s", dump)
	}
}

func TestCompactRegressionDoesNotExpandContext(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{strings.Repeat("verbose summary ", 400)}), mock.WithDelay(0))
	defer eng.Stop()
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), "small question", "small answer")
	}
	before, err := eng.builder.Build(ctx, sid, "mock-model", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); !errors.Is(err, ErrCompactNotReduced) {
		t.Fatalf("want no-reduction rejection, got %v", err)
	}
	after, err := eng.builder.Build(ctx, sid, "mock-model", "chat")
	if err != nil {
		t.Fatal(err)
	}
	b, a := contextbuilder.EstimateRequest(before).Total(), contextbuilder.EstimateRequest(after).Total()
	t.Logf("estimated request tokens before=%d after=%d", b, a)
	if a != b {
		t.Fatal("rejected summary changed context")
	}
}

func TestManualCompactAcceptsReductionAboveSoftTargets(t *testing.T) {
	for _, threshold := range []string{"0", "5", "80"} {
		t.Run(threshold, func(t *testing.T) {
			eng, ms, _, sid := newTestEngine(t)
			defer eng.Stop()
			ctx := context.Background()
			if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
				Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000, Limits: &store.ModelLimits{MaxOutputTokens: 1024}}}}); err != nil {
				t.Fatal(err)
			}
			if err := ms.SetSettings(ctx, map[string]string{config.SettingCompactAutoThresholdPercent: threshold}); err != nil {
				t.Fatal(err)
			}
			appendEngineTestTurn(t, ms, sid, "old", strings.Repeat("facts ", 500), "answer")
			appendEngineTestTurn(t, ms, sid, "recent-1", "recent question", "answer")
			appendEngineTestTurn(t, ms, sid, "recent-2", "latest question", "answer")
			calls := 0
			eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
				calls++
				return compactChunks(provider.Chunk{Delta: strings.Repeat("facts ", 350), Done: true, Finish: provider.FinishStop}), nil
			}))
			if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
				t.Fatalf("manual compact must accept a useful summary even above its target: %v", err)
			}
			if calls != 1 {
				t.Fatalf("manual compact made %d summary calls, want one", calls)
			}
			messages, _ := ms.ListMessages(ctx, sid, 0)
			meta, ok := store.CompactMetadataFromMessage(messages[len(messages)-1])
			if !ok || meta.BeforeInputEstimate <= meta.AfterInputEstimate || meta.AfterInputEstimate <= 0 || meta.InputBudget <= 0 || len(meta.TailMessageIDs) != 4 {
				t.Fatalf("canonical summary must record its gain and preserve the recent tail: %+v", meta)
			}
		})
	}
}

type compactLargeSchemaTool struct{ compactLargeTool }

func (*compactLargeSchemaTool) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return []provider.ToolDef{{Name: tool.TimeGetCurrent, Capability: store.ModeChat, Description: strings.Repeat("schema ", 10000), InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
}

func TestManualCompactKeepsGainWhenFixedOverheadExceedsCapacity(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	title := "named"
	_, _ = ms.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title})
	_ = ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000, Limits: &store.ModelLimits{MaxOutputTokens: 1024}, Capabilities: &store.ModelCaps{Tools: true}}}})
	_ = ms.SetSettings(ctx, map[string]string{config.SettingCompactAutoThresholdPercent: "0"})
	eng.tools = &compactLargeSchemaTool{}
	for i := 0; i < 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("facts ", 500), "answer")
	}
	calls := 0
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		calls++
		if req.System != compactSystemPrompt || contextbuilder.EstimateRequest(req).Total() > contextInputLimit(req.Config, "openai-compatible") {
			t.Error("only the bounded summary request may reach the provider")
		}
		return compactChunks(provider.Chunk{Delta: "Earlier work completed.", Done: true, Finish: provider.FinishStop}), nil
	}))
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
		t.Fatalf("fixed overhead must not prevent a useful manual compact: %v", err)
	}
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "next", Text: "continue"}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	turn, err := findTurn(ms, sid, "next")
	if err != nil || turn.Status != store.TurnFailed || !strings.Contains(turn.Error, ErrContextBudget.Error()) || calls != 1 {
		t.Fatalf("oversized follow-up bypassed capacity guard: turn=%+v err=%v calls=%d", turn, err, calls)
	}
	messages, _ := ms.ListMessages(ctx, sid, 0)
	if len(messages) < 7 || messages[6].Role != store.RoleSummary {
		t.Fatal("useful summary was not retained")
	}
	meta, _ := store.CompactMetadataFromMessage(messages[6])
	if meta.AfterInputEstimate <= meta.InputBudget || meta.BeforeInputEstimate <= meta.AfterInputEstimate {
		t.Fatalf("expected useful but still oversized context: %+v", meta)
	}
}

func TestCompactBatchesFitActualSummaryAboveTarget(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	_ = ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000, Limits: &store.ModelLimits{MaxOutputTokens: 4096}}}})
	sess, _ := ms.GetSession(ctx, sid)
	resolved, err := eng.resolveModel(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		calls++
		if contextbuilder.EstimateRequest(req).Total() > contextInputLimit(req.Config, resolved.protocol) {
			t.Fatal("batch exceeded input budget")
		}
		if calls > 1 && !strings.Contains(req.Messages[0].Text, "ROLLING_FACT") {
			t.Fatal("rolling summary was lost")
		}
		return compactChunks(provider.Chunk{Delta: "ROLLING_FACT " + strings.Repeat("summary ", 1000), Done: true, Finish: provider.FinishStop}), nil
	})
	summary, err := eng.summarizeCompactHistory(ctx, sid, resolved, client,
		[]*store.Message{{ID: "large", Role: store.RoleUser, Text: strings.Repeat("source ", 12000)}}, "", 100)
	if err != nil || calls < 2 || !strings.Contains(summary, "ROLLING_FACT") {
		t.Fatalf("above-target rolling summary must fit subsequent fragments: calls=%d error=%v", calls, err)
	}
}

func TestCompactEmptySummaryDoesNotChangeHistory(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithChunks([]provider.Chunk{{Done: true, Finish: provider.FinishStop}}))
	defer eng.Stop()
	for i := 0; i < 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("facts ", 100), "answer")
	}
	if _, err := eng.Compact(context.Background(), CompactInput{SessionID: sid}); !errors.Is(err, ErrCompactSummaryEmpty) {
		t.Fatalf("empty model output must differ from no history: %v", err)
	}
	messages, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(messages) != 6 {
		t.Fatal("empty summary changed canonical history")
	}
}

func TestCompactRegressionLongTailFitsContextWindow(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"short summary"}), mock.WithDelay(0))
	defer eng.Stop()
	ctx := context.Background()
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 64000}}}); err != nil {
		t.Fatal(err)
	}
	appendEngineTestTurn(t, ms, sid, "1", "old question", "old answer")
	for i := 2; i <= 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("x", 160000), "answer")
	}
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	usage, err := eng.SessionUsage(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("post-compact estimated tokens=%d context window=%d threshold=%d", usage.ContextEstimatedTokens, usage.ContextWindow, usage.AutoCompactThresholdTokens)
	if usage.ContextEstimatedTokens >= usage.ContextWindow {
		t.Fatal("compact succeeds but protected two-turn tail exceeds the entire context window")
	}
}

func TestCompactRegressionSummaryRequestFitsContextWindow(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 64000}}}); err != nil {
		t.Fatal(err)
	}
	capture := &captureClient{reqCh: make(chan provider.Request, 128)}
	eng.resolver = registry.Static(capture)
	for i := 0; i < 102; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("x", 4000), strings.Repeat("y", 4000))
	}
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	count := len(capture.reqCh)
	if count < 2 {
		t.Fatalf("large history was not batched: %d requests", count)
	}
	for i := 0; i < count; i++ {
		req := <-capture.reqCh
		n := contextbuilder.EstimateRequest(req).Total()
		if n > contextInputLimit(req.Config, "") {
			t.Fatalf("oversized summary request: %d", n)
		}
		if i > 0 && !strings.Contains(req.Messages[0].Text, "Previous summary") {
			t.Fatal("lost earlier batch summary")
		}
	}
}

type compactClientFunc func(context.Context, provider.Request) (<-chan provider.Chunk, error)

func (f compactClientFunc) Name() string { return "mock" }
func (f compactClientFunc) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return f(ctx, req)
}
func compactChunks(chunks ...provider.Chunk) <-chan provider.Chunk {
	out := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		out <- chunk
	}
	close(out)
	return out
}

func TestCompactSessionsDoNotBlockEachOther(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "other", Provider: "mock", Model: "mock-model"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{sid, "other"} {
		for i := 0; i < 3; i++ {
			appendEngineTestTurn(t, ms, id, fmt.Sprintf("%s-%d", id, i), strings.Repeat("facts ", 100), "answer")
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	var calls int
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		calls++
		if calls == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return compactChunks(provider.Chunk{Delta: "summary", Done: true, Finish: provider.FinishStop}), nil
	}))
	first := make(chan error, 1)
	go func() { _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); first <- err }()
	<-started
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); !errors.Is(err, ErrCompactRunning) {
		t.Errorf("same session must be serialized: %v", err)
	}
	_, err := eng.Compact(ctx, CompactInput{SessionID: "other"})
	close(release)
	if err != nil {
		t.Errorf("other session was blocked: %v", err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestCompactCancellationDoesNotChangeBoundary(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	for i := 0; i < 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("facts ", 100), "answer")
	}
	gate := &compactGateClient{started: make(chan struct{}), release: make(chan struct{})}
	eng.resolver = registry.Static(gate)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); done <- err }()
	<-gate.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	messages, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(messages) != 6 {
		t.Fatal("cancelled summary was committed")
	}
	// The per-session lock must also be released.
	eng.resolver = registry.Static(mock.New(mock.WithScript([]string{"summary"}), mock.WithDelay(0)))
	if _, err := eng.Compact(context.Background(), CompactInput{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
}

func TestCompactRejectsTruncatedSummary(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithChunks([]provider.Chunk{{Delta: "partial", Done: true, Finish: provider.FinishLength}}), mock.WithDelay(0))
	defer eng.Stop()
	for i := 0; i < 3; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("facts ", 100), "answer")
	}
	if _, err := eng.Compact(context.Background(), CompactInput{SessionID: sid}); err == nil {
		t.Fatal("truncated summary accepted")
	}
	messages, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(messages) != 6 {
		t.Fatal("truncated summary changed boundary")
	}
}

type compactLargeTool struct{ calls int }

func (r *compactLargeTool) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return []provider.ToolDef{{Name: tool.TimeGetCurrent, Capability: store.ModeChat, InputSchema: json.RawMessage("{\"type\":\"object\",\"properties\":{}}")}}, nil
}
func (r *compactLargeTool) Call(_ context.Context, call tool.Call) tool.Result {
	r.calls++
	return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: fmt.Sprintf("result %d\n%s", r.calls, strings.Repeat("x", 11000))}
}

func TestCompactDuringToolLoopKeepsLatestExchangeAndContinues(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	title := "named"
	_, _ = ms.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title})
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000, Limits: &store.ModelLimits{MaxOutputTokens: 1024}, Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	runner := &compactLargeTool{}
	eng.tools = runner
	normalCalls, runningCompacts := 0, 0
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		if contextbuilder.EstimateRequest(req).Total() > contextInputLimit(req.Config, "") {
			t.Error("sent oversized request")
		}
		if req.System == compactSystemPrompt {
			if _, err := ms.RunningTurn(ctx, sid); err == nil {
				runningCompacts++
			}
			return compactChunks(provider.Chunk{Delta: "Earlier checks completed; continue the current task.", Done: true, Finish: provider.FinishStop}), nil
		}
		normalCalls++
		calls, results := map[string]int{}, map[string]int{}
		hasInput, hasLatest := false, normalCalls == 1
		for _, msg := range req.Messages {
			hasInput = hasInput || strings.Contains(msg.Text, "KEEP_CURRENT_TASK")
			for _, part := range msg.Parts {
				if part.Type == provider.PartToolUse {
					calls[part.CallID]++
				}
				if part.Type == provider.PartToolResult {
					if !part.Ok {
						t.Errorf("tool failed: %s", part.Content)
					}
					results[part.CallID]++
					hasLatest = hasLatest || part.CallID == fmt.Sprintf("call_%d", normalCalls-1)
				}
			}
		}
		if !hasInput || !hasLatest {
			t.Error("lost current input or latest tool result")
		}
		for id, n := range calls {
			if n != 1 || results[id] != 1 {
				t.Errorf("broken/duplicated call pair %s: %d/%d", id, n, results[id])
			}
		}
		for id := range results {
			if calls[id] != 1 {
				t.Errorf("orphan result %s", id)
			}
		}
		if normalCalls == 13 {
			return compactChunks(provider.Chunk{Delta: "TASK_DONE"}, provider.Chunk{Done: true, Finish: provider.FinishStop}), nil
		}
		id := fmt.Sprintf("call_%d", normalCalls)
		state := &provider.Continuation{Kind: provider.ContinuationOpenAIChat, Data: json.RawMessage(fmt.Sprintf("{\"content\":\"state %d\"}", normalCalls))}
		return compactChunks(provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: id, Name: tool.TimeGetCurrent, ArgsDelta: "{}"}},
			provider.Chunk{Done: true, Finish: provider.FinishToolCalls, Continuation: state}), nil
	}))
	res, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "input", Text: "KEEP_CURRENT_TASK"})
	if err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	turn, err := findTurn(ms, sid, "input")
	if err != nil || turn.Status != store.TurnCompleted {
		t.Fatalf("turn failed: %+v %v", turn, err)
	}
	if normalCalls != 13 || runner.calls != 12 || runningCompacts == 0 {
		t.Fatalf("requests=%d tools=%d live compactions=%d", normalCalls, runner.calls, runningCompacts)
	}
	messages, _ := ms.ListMessages(ctx, sid, 0)
	found := false
	for _, m := range messages {
		found = found || m.TurnID == res.TurnID && m.Text == "TASK_DONE"
	}
	if !found {
		t.Fatal("final canonical answer missing")
	}
}

func TestContextBudgetRejectsOversizedCurrentInputWithoutProviderCall(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	title := "named"
	_, _ = ms.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title})
	_ = ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000}}})
	calls := 0
	eng.resolver = registry.Static(compactClientFunc(func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		calls++
		return compactChunks(provider.Chunk{Delta: "unexpected", Done: true}), nil
	}))
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "large", Text: strings.Repeat("x", 100000)}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	turn, err := findTurn(ms, sid, "large")
	if err != nil || turn.Status != store.TurnFailed || !strings.Contains(turn.Error, ErrContextBudget.Error()) || calls != 0 {
		t.Fatalf("turn=%+v err=%v provider calls=%d", turn, err, calls)
	}
}

func TestCompactPartitionPreservesParallelCallsAndNativeState(t *testing.T) {
	m := func(id string, role store.Role, parts ...store.ContentPart) *store.Message {
		return &store.Message{ID: id, TurnID: "running", Role: role, Parts: parts}
	}
	messages := []*store.Message{
		m("input", store.RoleUser, store.ContentPart{Type: store.ContentPartText, Text: "task"}),
		m("oldcall", store.RoleAssistant, store.ContentPart{Type: store.ContentPartToolUse, CallID: "old"}),
		m("oldresult", store.RoleTool, store.ContentPart{Type: store.ContentPartToolResult, CallID: "old"}),
		m("oldimage", store.RoleAssistant, store.ContentPart{Type: store.ContentPartAttachment, AttachmentKey: "old-image"}),
		m("thought", store.RoleAssistant, store.ContentPart{Type: store.ContentPartThought, Text: "reasoning"}),
		m("call1", store.RoleAssistant, store.ContentPart{Type: store.ContentPartToolUse, CallID: "a"}),
		m("call2", store.RoleAssistant, store.ContentPart{Type: store.ContentPartToolUse, CallID: "b"}),
		m("result1", store.RoleTool, store.ContentPart{Type: store.ContentPartToolResult, CallID: "a"}),
		m("result2", store.RoleTool, store.ContentPart{Type: store.ContentPartToolResult, CallID: "b"}),
		m("image", store.RoleAssistant, store.ContentPart{Type: store.ContentPartAttachment, AttachmentKey: "new-image"}),
	}
	messages[6].ProviderState = &store.ProviderState{Provider: "p", Model: "m", Kind: provider.ContinuationOpenAIChat, Data: json.RawMessage("{\"signature\":\"unchanged\"}")}
	cold, tail := compactPartition(messages, 0, "running")
	if strings.Join(messageIDs(cold), ",") != "oldcall,oldresult,oldimage" || strings.Join(messageIDs(tail), ",") != "input,thought,call1,call2,result1,result2,image" {
		t.Fatalf("cold=%v tail=%v", messageIDs(cold), messageIDs(tail))
	}
	if tail[3].ProviderState != messages[6].ProviderState {
		t.Fatal("native state was rewritten")
	}
}
