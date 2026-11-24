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
	dump := compactMessageRecord(msg, compactMessageText(msg), "summarize")
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
		if requestInputTokens(req) > contextInputLimit(req.Config) {
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
	dump := compactMessageRecord(msg, compactMessageText(msg), "summarize")
	if !strings.Contains(dump, "UNIQUE_TARGET") {
		t.Fatalf("tool arguments absent from summary input: %s", dump)
	}
}

func TestCompactExcludesUIOnlyHistory(t *testing.T) {
	for _, name := range []string{tool.SetNextPrompt, "builtin_plan_update"} {
		t.Run(name, func(t *testing.T) {
			testCompactExcludesUIOnlyHistory(t, name)
		})
	}
}

func testCompactExcludesUIOnlyHistory(t *testing.T, name string) {
	msg := &store.Message{Role: store.RoleAssistant, Parts: []store.ContentPart{
		{Type: store.ContentPartText, Text: "Task completed"},
		{Type: store.ContentPartToolUse, Name: name, CallID: "ui", Args: json.RawMessage(`{"text":"SUGGESTION_ONLY"}`)},
		{Type: store.ContentPartToolResult, Name: name, CallID: "ui", Ok: true, Content: `{"text":"SUGGESTION_ONLY"}`},
		{Type: store.ContentPartToolUse, Name: tool.TimeGetCurrent, CallID: "time", Args: json.RawMessage(`{}`)},
		{Type: store.ContentPartToolResult, Name: tool.TimeGetCurrent, CallID: "time", Ok: true, Content: `{"time":"BUSINESS_RESULT"}`},
	}}
	text := compactMessageText(msg)
	if strings.Contains(text, name) || strings.Contains(text, "SUGGESTION_ONLY") {
		t.Fatalf("suggestion leaked into summary input: %s", text)
	}
	if !strings.Contains(text, "Task completed") || !strings.Contains(text, tool.TimeGetCurrent) || !strings.Contains(text, "BUSINESS_RESULT") {
		t.Fatalf("summary lost task context: %s", text)
	}
	msg.Parts = msg.Parts[1:3]
	if got := compactMessageText(msg); got != "" {
		t.Fatalf("suggestion-only message should be skipped: %s", got)
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
	b, a := requestInputTokens(before), requestInputTokens(after)
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
		if req.System != compactSystemPrompt || requestInputTokens(req) > contextInputLimit(req.Config) {
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

func TestCompactOversizedInputFailsBeforeProvider(t *testing.T) {
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
		return compactChunks(provider.Chunk{Delta: "must not call", Done: true, Finish: provider.FinishStop}), nil
	})
	messages := []*store.Message{{ID: "large", Role: store.RoleUser, Text: strings.Repeat("source ", 12000)}}
	summary, err := eng.summarizeCompactHistory(ctx, sid, resolved, client, messages, messages, "", 100, true)
	if !errors.Is(err, ErrContextBudget) || calls != 0 || summary != "" {
		t.Fatalf("oversized history must fail without splitting or calling provider: calls=%d error=%v", calls, err)
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

func TestCompactOversizedMultiTurnUsesRollingBatches(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
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
	var requests []provider.Request
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		input := requestInputTokens(req)
		output, _ := req.Config.MaxOutputTokens()
		if output <= 0 || input+output > req.Config.ContextWindow {
			t.Errorf("rolling batch request exceeded window: input=%d output=%d window=%d", input, output, req.Config.ContextWindow)
		}
		requests = append(requests, req)
		return compactChunks(provider.Chunk{Delta: fmt.Sprintf("rolling batch %d summary.", len(requests)), Done: true, Finish: provider.FinishStop}), nil
	}))
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
		t.Fatalf("multi-turn rolling compact failed: %v", err)
	}
	if len(requests) < 2 {
		t.Fatalf("expected rolling batches for oversized history, got %d request(s)", len(requests))
	}
	for i := 1; i < len(requests); i++ {
		if !strings.Contains(requests[i].Messages[0].Text, fmt.Sprintf("rolling batch %d summary.", i)) {
			t.Fatalf("batch %d missing rolling summary carry-over", i+1)
		}
	}
	usage, err := eng.SessionUsage(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if usage.ContextEstimatedTokens >= 64000 {
		t.Fatalf("post-compact usage still exceeds window: %+v", usage)
	}

	t.Run("single_oversized_message", func(t *testing.T) {
		singleEng, singleMS, _, singleSID := newTestEngine(t)
		defer singleEng.Stop()
		if err := singleMS.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
			Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 64000}}}); err != nil {
			t.Fatal(err)
		}
		appendEngineTestTurn(t, singleMS, singleSID, "1", strings.Repeat("y", 280000), "answer")
		appendEngineTestTurn(t, singleMS, singleSID, "2", "tail", "answer")
		capture := &captureClient{reqCh: make(chan provider.Request, 1)}
		singleEng.resolver = registry.Static(capture)
		before, _ := singleMS.ListMessages(ctx, singleSID, 0)
		if _, err := singleEng.Compact(ctx, CompactInput{SessionID: singleSID}); !errors.Is(err, ErrContextBudget) {
			t.Fatalf("single oversized message must fail: %v", err)
		}
		after, _ := singleMS.ListMessages(ctx, singleSID, 0)
		if len(after) != len(before) || len(capture.reqCh) != 0 {
			t.Fatal("single oversized message triggered provider calls or changed history")
		}
	})

	t.Run("small_window_retains_tail", func(t *testing.T) {
		smallEng, smallMS, _, smallSID := newTestEngine(t)
		defer smallEng.Stop()
		if err := smallMS.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
			Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 8192}}}); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 4; i++ {
			appendEngineTestTurn(t, smallMS, smallSID, fmt.Sprintf("old-%d", i), fmt.Sprintf("OLD_%d %s", i, strings.Repeat("x", 4000)), "ok")
		}
		for i := 1; i <= 2; i++ {
			appendEngineTestTurn(t, smallMS, smallSID, fmt.Sprintf("tail-%d", i), fmt.Sprintf("RETAIN_TAIL_FACT_%d %s", i, strings.Repeat("r", 12000)), "ok")
		}
		var smallReqs []provider.Request
		smallEng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
			input := requestInputTokens(req)
			output, _ := req.Config.MaxOutputTokens()
			if output <= 0 || input+output > req.Config.ContextWindow {
				t.Errorf("small-window rolling batch exceeded window: input=%d output=%d window=%d", input, output, req.Config.ContextWindow)
			}
			smallReqs = append(smallReqs, req)
			return compactChunks(provider.Chunk{Delta: fmt.Sprintf("small batch %d summary.", len(smallReqs)), Done: true, Finish: provider.FinishStop}), nil
		}))
		if _, err := smallEng.Compact(ctx, CompactInput{SessionID: smallSID}); err != nil {
			t.Fatalf("small-window rolling compact failed: %v", err)
		}
		if len(smallReqs) < 2 {
			t.Fatalf("expected rolling batches for small window, got %d request(s)", len(smallReqs))
		}
		finalPrompt := smallReqs[len(smallReqs)-1].Messages[0].Text
		if !strings.Contains(finalPrompt, fmt.Sprintf("small batch %d summary.", len(smallReqs)-1)) || !strings.Contains(finalPrompt, `action="retain"`) || !strings.Contains(finalPrompt, "RETAIN_TAIL_FACT_1") {
			t.Fatalf("small-window probe dropped retained tail or rolling summary: %s", finalPrompt)
		}
	})

	t.Run("running_compact_preserves_retain_order", func(t *testing.T) {
		runEng, runMS, _, runSID := newTestEngine(t)
		defer runEng.Stop()
		runEng.tools = &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.TimeGetCurrent, Capability: store.ModeCode}}}
		if err := runMS.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
			Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 8192, Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
			t.Fatal(err)
		}
		sess, _ := runMS.GetSession(ctx, runSID)
		resolved, err := runEng.resolveModel(ctx, sess)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runMS.BeginTurn(ctx, store.BeginTurnInput{
			SessionID: runSID, TurnID: "running", UserMessageID: "u_run", ClientMessageID: "u_run",
			UserText: "RUNNING_INPUT_BEFORE_COLD " + strings.Repeat("u", 10000), Mode: store.ModeCode,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := runMS.AppendTurnOutput(ctx, store.AppendTurnOutputInput{
			TurnID: "running",
			Parts: []store.ContentPart{
				{Type: store.ContentPartToolUse, CallID: "c1", Name: tool.TimeGetCurrent, Args: []byte(`{}`)},
				{Type: store.ContentPartToolResult, CallID: "c1", Name: tool.TimeGetCurrent, Ok: true, Content: "COLD_1 " + strings.Repeat("a", 10000)},
				{Type: store.ContentPartToolUse, CallID: "c2", Name: tool.TimeGetCurrent, Args: []byte(`{}`)},
				{Type: store.ContentPartToolResult, CallID: "c2", Name: tool.TimeGetCurrent, Ok: true, Content: "COLD_2 " + strings.Repeat("b", 10000)},
				{Type: store.ContentPartToolUse, CallID: "c3", Name: tool.TimeGetCurrent, Args: []byte(`{}`)},
				{Type: store.ContentPartToolResult, CallID: "c3", Name: tool.TimeGetCurrent, Ok: true, Content: "RETAIN_AFTER_COLD " + strings.Repeat("z", 4000)},
			},
		}); err != nil {
			t.Fatal(err)
		}
		var runReqs []provider.Request
		runEng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
			runReqs = append(runReqs, req)
			return compactChunks(provider.Chunk{Delta: fmt.Sprintf("running batch %d.", len(runReqs)), Done: true, Finish: provider.FinishStop}), nil
		}))
		if _, err := runEng.compact(ctx, CompactInput{SessionID: runSID}, resolved, store.ModeCode, "running", false); err != nil {
			t.Fatalf("running rolling compact failed: %v", err)
		}
		if len(runReqs) < 2 {
			t.Fatalf("expected rolling batches for running turn, got %d", len(runReqs))
		}
		finalPrompt := runReqs[len(runReqs)-1].Messages[0].Text
		idxBefore := strings.Index(finalPrompt, "RUNNING_INPUT_BEFORE_COLD")
		idxRolling := strings.Index(finalPrompt, fmt.Sprintf("running batch %d.", len(runReqs)-1))
		idxAfter := strings.Index(finalPrompt, "RETAIN_AFTER_COLD")
		if idxBefore < 0 || idxRolling < 0 || idxAfter < 0 || !(idxBefore < idxRolling && idxRolling < idxAfter) {
			t.Fatalf("retain order broken across rolling batch: before=%d rolling=%d after=%d", idxBefore, idxRolling, idxAfter)
		}
	})

	t.Run("single_source_with_large_retain_drops_retain", func(t *testing.T) {
		singleRetainEng, singleRetainMS, _, singleRetainSID := newTestEngine(t)
		defer singleRetainEng.Stop()
		if err := singleRetainMS.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
			Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 8192}}}); err != nil {
			t.Fatal(err)
		}
		sess, _ := singleRetainMS.GetSession(ctx, singleRetainSID)
		resolved, err := singleRetainEng.resolveModel(ctx, sess)
		if err != nil {
			t.Fatal(err)
		}
		msgs := []*store.Message{
			{ID: "src1", Role: store.RoleUser, Text: "SINGLE_SOURCE_FACT " + strings.Repeat("s", 14000)},
			{ID: "ret1", Role: store.RoleUser, Text: "LARGE_RETAIN_FACT " + strings.Repeat("r", 22000)},
		}
		var calls []provider.Request
		client := compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
			calls = append(calls, req)
			return compactChunks(provider.Chunk{Delta: "single source summarized.", Done: true, Finish: provider.FinishStop}), nil
		})
		summary, err := singleRetainEng.summarizeCompactHistory(ctx, singleRetainSID, resolved, client, msgs, msgs[:1], "", 512, true)
		if err != nil || summary == "" || len(calls) != 1 {
			t.Fatalf("single source with large retain failed: summary=%q calls=%d err=%v", summary, len(calls), err)
		}
		prompt := calls[0].Messages[0].Text
		if !strings.Contains(prompt, "SINGLE_SOURCE_FACT") || strings.Contains(prompt, "LARGE_RETAIN_FACT") {
			t.Fatalf("expected single source without overflowing retain: %s", prompt)
		}
	})
}

// Both second messages fit alone in an 8K window. The first partial made the
// original final request either overflow or leave only four output tokens.
func TestCompactHandoffWithNearWindowMessage(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"carry_exceeds_window", 29551}, {"carry_leaves_four_tokens", 28939}} {
		t.Run(tc.name, func(t *testing.T) {
			eng, db, _, sid := newTestEngine(t)
			defer eng.Stop()
			ctx := context.Background()
			profile := &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 8192}}}
			if err := db.PutProviderProfile(ctx, profile); err != nil {
				t.Fatal(err)
			}
			title := "Handoff"
			if _, err := db.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title}); err != nil {
				t.Fatal(err)
			}
			for i, text := range []string{strings.Repeat("a", 5000), strings.Repeat("b", tc.size)} {
				id := fmt.Sprintf("source%d", i+1)
				if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: id, UserMessageID: id, ClientMessageID: "external-mcp:agent:" + id, UserText: text}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: store.TurnCompleted}); err != nil {
					t.Fatal(err)
				}
			}
			const first = "first outcome @message(source1)"
			const second = "second outcome @message(source2)"
			var summaries, replies int
			eng.resolver = registry.Static(compactClientFunc(func(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
				text := "continued"
				if req.System == compactSystemPrompt {
					summaries++
					budget, _ := req.Config.MaxOutputTokens()
					prompt := req.Messages[0].Text
					if budget < 64 || strings.Contains(prompt, "@message(rolling_summary)") {
						t.Errorf("invalid summary request: budget=%d", budget)
					}
					switch {
					case strings.Contains(prompt, strings.Repeat("a", 100)):
						text = first + strings.Repeat("k", 1000-len(first))
					case strings.Contains(prompt, strings.Repeat("b", 100)):
						text = second
					default:
						if !strings.Contains(prompt, first) || !strings.Contains(prompt, second) {
							t.Error("merge lost a source or its citation")
						}
						text = first + " " + second
					}
				} else {
					replies++
					messages, _ := json.Marshal(req.Messages)
					for _, want := range []string{first, second, "continue this task"} {
						if !strings.Contains(string(messages), want) {
							t.Errorf("handoff lost %q", want)
						}
					}
				}
				return compactChunks(provider.Chunk{Delta: text, Done: true, Finish: provider.FinishStop}), nil
			}))
			if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "native", Text: "continue this task"}); err != nil {
				t.Fatal(err)
			}
			eng.Wait()
			turn, err := findTurn(db, sid, "native")
			if err != nil || turn.Status != store.TurnCompleted || summaries != 3 || replies != 1 {
				t.Fatalf("handoff: turn=%v err=%v summaries=%d replies=%d", turn, err, summaries, replies)
			}
		})
	}
}

func TestExternalMCPHistoryCooledInUsageAndCompact(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	eng.tools = &recordingToolRunner{defs: []provider.ToolDef{
		{Name: tool.FileRead, Capability: store.ModeCode},
		{Name: tool.HistoryGetMessage, Capability: store.ModeChat},
	}}
	mode, lease := store.ModeCode, store.ModeLeaseSession
	if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{ActiveMode: &mode, ModeLease: &lease}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		turnID := fmt.Sprintf("ext-%d", i)
		if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
			SessionID: sid, TurnID: turnID, UserMessageID: "u-" + turnID,
			ClientMessageID: "external-mcp:agent_1:" + turnID,
			UserText:        fmt.Sprintf("Agent · Task %d", i),
			UserMetadata:    json.RawMessage(`{"externalAgent":{"name":"Antigravity"}}`),
			Mode:            store.ModeCode,
		}); err != nil {
			t.Fatal(err)
		}
		big := fmt.Sprintf("RAW_EXTERNAL_OUTPUT_%d\n%s", i, strings.Repeat("line ", 4000))
		parts := []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "c-" + turnID, Name: tool.FileRead, Args: json.RawMessage(`{}`)},
			{Type: store.ContentPartToolResult, CallID: "c-" + turnID, Name: tool.FileRead, Ok: true, Content: big},
		}
		status := store.TurnCancelled
		if i > 0 {
			status = store.TurnCompleted
			parts = append(parts, store.ContentPart{Type: store.ContentPartText, Text: fmt.Sprintf("FINAL_EXTERNAL_SUMMARY_%d", i)})
		}
		if _, err := ms.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: turnID, Parts: parts}); err != nil {
			t.Fatal(err)
		}
		if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{TurnID: turnID, Status: status, Mode: store.ModeCode}); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := eng.SessionUsage(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	// Four raw outputs exceed 80,000 chars before fixed overhead;
	// cooling folds them to compact result_ref stubs.
	if usage.MessageEstimatedTokens > 1500 {
		t.Fatalf("external MCP history was not cooled in SessionUsage: %+v", usage)
	}
	all, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := compactHistoryText(all, all, compactColdTurns(all, true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(history, "RAW_EXTERNAL_OUTPUT_") || !strings.Contains(history, `"cold_only":true`) || !strings.Contains(history, "FINAL_EXTERNAL_SUMMARY_1") {
		t.Fatalf("compactHistoryText did not cool external outputs cleanly: %s", history)
	}
	// A cancelled turn has no final text to hand off. Exercise the real
	// compaction entry point with readback absent from the next model's tools.
	eng.tools = &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.FileRead, Capability: store.ModeCode}}}
	var summaryInput string
	eng.resolver = registry.Static(compactClientFunc(func(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		summaryInput = req.Messages[0].Text
		return compactChunks(provider.Chunk{Delta: "Earlier checks complete.", Done: true, Finish: provider.FinishStop}), nil
	}))
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summaryInput, "RAW_EXTERNAL_OUTPUT_0") || strings.Contains(summaryInput, strings.Repeat("line ", 4000)) || strings.Contains(summaryInput, `"cold_only":true`) {
		t.Fatal("compaction without a reader must include bounded evidence from the interrupted turn")
	}

}

func TestCompactRegressionSummaryRequestFitsContextWindow(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 1048576}}}); err != nil {
		t.Fatal(err)
	}
	requests := make(chan provider.Request, 2)
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		requests <- req
		return compactChunks(provider.Chunk{Delta: "short summary", Done: true, Finish: provider.FinishStop}), nil
	}))
	for i := 0; i < 102; i++ {
		appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("x", 4000), strings.Repeat("y", 4000))
	}
	if _, err := eng.Compact(ctx, CompactInput{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	count := len(requests)
	if count != 1 {
		t.Fatalf("large history must be summarized in one request: %d requests", count)
	}
	for i := 0; i < count; i++ {
		req := <-requests
		n := requestInputTokens(req)
		output, _ := req.Config.MaxOutputTokens()
		if n <= 64000 || n+output+compactInputMargin(n) > req.Config.ContextWindow {
			t.Fatalf("oversized summary request: %d", n)
		}
		if strings.Count(req.Messages[0].Text, "action=\"summarize\">") != 200 || strings.Count(req.Messages[0].Text, "action=\"retain\">") != 4 {
			t.Fatal("summary request lost source or retained messages")
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
	for _, finish := range []provider.FinishReason{provider.FinishLength, "", provider.FinishToolCalls} {
		t.Run(string(finish), func(t *testing.T) {
			eng, ms, _, sid := newTestEngine(t, mock.WithChunks([]provider.Chunk{{Delta: "partial", Done: true, Finish: finish}}), mock.WithDelay(0))
			defer eng.Stop()
			for i := 0; i < 3; i++ {
				appendEngineTestTurn(t, ms, sid, fmt.Sprint(i), strings.Repeat("facts ", 100), "answer")
			}
			if _, err := eng.Compact(context.Background(), CompactInput{SessionID: sid}); !errors.Is(err, ErrCompactIncomplete) {
				t.Fatal("truncated summary accepted")
			}
			messages, _ := ms.ListMessages(context.Background(), sid, 0)
			if len(messages) != 6 {
				t.Fatal("truncated summary changed boundary")
			}
		})
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
		Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 16000, Limits: &store.ModelLimits{MaxOutputTokens: 16000}, Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	runner := &compactLargeTool{}
	eng.tools = runner
	normalCalls, runningCompacts := 0, 0
	eng.resolver = registry.Static(compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		input := requestInputTokens(req)
		output, ok := req.Config.MaxOutputTokens()
		if !ok || output <= 0 || input+output > req.Config.ContextWindow {
			t.Errorf("request exceeds window: input=%d output=%d window=%d", input, output, req.Config.ContextWindow)
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
