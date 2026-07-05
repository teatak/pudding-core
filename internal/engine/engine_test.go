package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func newTestEngine(t *testing.T, opts ...mock.Option) (*Engine, *memstore.Memstore, *event.Hub, string) {
	t.Helper()
	ms := memstore.New()
	hub := event.NewHub()
	eng := New(ms, hub, registry.Static(mock.New(opts...)), ms)
	sess := &store.Session{ID: "sess_1", Provider: "mock", Model: "mock-model"}
	if err := ms.CreateSession(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(context.Background(), &store.ProviderProfile{
		ID:          "mock",
		DisplayName: "mock",
		Protocol:    "openai-compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Models:      []store.ProviderModel{{ID: "mock-model"}},
	}); err != nil {
		t.Fatal(err)
	}
	return eng, ms, hub, sess.ID
}

// mapResolver 按 profile 名路由到不同 client,服务多 provider 路由测试。
type mapResolver map[string]provider.Client

func (m mapResolver) Resolve(_ context.Context, name string) (provider.Client, error) {
	c, ok := m[name]
	if !ok {
		return nil, errors.New("no such profile: " + name)
	}
	return c, nil
}

func waitTurnDone(t *testing.T, s store.Store, sessionID string) *store.Turn {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("turn did not finish in time")
		case <-time.After(10 * time.Millisecond):
		}
		if _, err := s.RunningTurn(context.Background(), sessionID); errors.Is(err, store.ErrNotFound) {
			evs, err := s.EventsAfter(context.Background(), sessionID, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(evs) >= 2 {
				return nil
			}
		}
	}
}

func TestSubmitFallsBackWhenSessionProviderDeleted(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"ok"}))), ms)
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "sess_fallback", Provider: "deleted", Model: "old-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID:          "fallback",
		DisplayName: "fallback",
		Protocol:    "openai-compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Models: []store.ProviderModel{{
			ID:            "new-model",
			ContextWindow: 1234,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: "sess_fallback", ClientMessageID: "c1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, "sess_fallback")

	sess, err := ms.GetSession(ctx, "sess_fallback")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Provider != "fallback" || sess.Model != "new-model" {
		t.Fatalf("session should be updated to fallback model, got provider=%q model=%q", sess.Provider, sess.Model)
	}
	page, err := ms.ListTurnsPage(ctx, "sess_fallback", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Turns) != 1 || page.Turns[0].Provider != "fallback" || page.Turns[0].Model != "new-model" {
		t.Fatalf("turn should use fallback model, got %+v", page.Turns)
	}
}

func TestStopCancelsAuxGoroutines(t *testing.T) {
	// 慢 provider:turn 与自动标题的 LLM 都会卡在 delay。Cancel 让 turn
	// 收尾,Stop 让标题 goroutine 立即退出;没有 Stop 时 Wait 会被标题
	// LLM 拖住。sess_1 标题为空,Submit 触发 autoTitle。
	eng, _, _, sessionID := newTestEngine(t, mock.WithScript([]string{"x"}), mock.WithDelay(3*time.Second))
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sessionID, ClientMessageID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // 让 turn + title goroutine 起来

	_ = eng.Cancel(sessionID) // turn 收尾
	eng.Stop()                // 辅助 goroutine(标题)取消

	done := make(chan struct{})
	go func() { eng.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Stop did not cancel aux goroutines; Wait blocked on the slow title LLM")
	}
}

func TestSubmitHappyPath(t *testing.T) {
	eng, ms, hub, sid := newTestEngine(t, mock.WithScript([]string{"你好", ",", "世界"}), mock.WithDelay(time.Millisecond))
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	res, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate || res.TurnID == "" || res.UserMessageID == "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	waitTurnDone(t, ms, sid)

	msgs, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[1].Role != store.RoleAssistant || msgs[1].Text != "你好,世界" {
		t.Fatalf("unexpected assistant message: %+v", msgs[1])
	}

	evs, _ := ms.EventsAfter(context.Background(), sid, 0, 0)
	if len(evs) != 2 || evs[0].Kind != event.TurnStarted || evs[1].Kind != event.TurnCompleted {
		t.Fatalf("unexpected lifecycle events: %+v", evs)
	}
	if evs[0].Seq != 1 || evs[1].Seq != 2 {
		t.Fatalf("seq not monotonic: %+v", evs)
	}
	if evs[0].Text != "hi" {
		t.Fatalf("started event must carry user text for live transcript: %+v", evs[0])
	}
	if evs[1].AssistantMessageID != msgs[1].ID {
		t.Fatalf("completed event not linked to assistant message")
	}

	// hub 侧应能看到 delta(不带 seq)与两条 lifecycle;连续小 chunk 可被合并。
	deltaText := ""
	timeout := time.After(time.Second)
	for deltaText != "你好,世界" {
		select {
		case ev := <-sub:
			if ev.Kind == event.TurnDelta {
				if ev.Seq != 0 {
					t.Fatalf("delta must not carry seq: %+v", ev)
				}
				deltaText += ev.Delta
			}
		case <-timeout:
			t.Fatalf("saw delta text %q", deltaText)
		}
	}
}

func TestSystemSubmitDoesNotCreateUserMessage(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	capture := &captureClient{reqCh: make(chan provider.Request, 1)}
	eng := New(ms, hub, mapResolver{"capture": capture}, ms)
	ctx := context.Background()
	sid := "sess_system"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "named", Provider: "capture", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID:          "capture",
		DisplayName: "capture",
		Protocol:    "openai-compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Models:      []store.ProviderModel{{ID: "model"}},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := eng.Submit(ctx, SubmitInput{
		SessionID:       sid,
		ClientMessageID: "sys_1",
		Kind:            "system",
		Text:            "Summarize this chat.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UserMessageID != "" {
		t.Fatalf("system submit must not create user message: %+v", res)
	}
	waitTurnDone(t, ms, sid)

	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Role != store.RoleSystem || msgs[0].Text != "Summarize this chat." || msgs[1].Role != store.RoleAssistant || msgs[1].Text != "ok" {
		t.Fatalf("unexpected canonical messages: %+v", msgs)
	}
	select {
	case req := <-capture.reqCh:
		if len(req.Messages) != 1 || req.Messages[0].Role != provider.RoleUser {
			t.Fatalf("system reminder should be a transient user-role provider message: %+v", req.Messages)
		}
		if !strings.Contains(req.Messages[0].Text, "<system-reminder>") || !strings.Contains(req.Messages[0].Text, "Summarize this chat.") {
			t.Fatalf("system reminder missing from request: %q", req.Messages[0].Text)
		}
	default:
		t.Fatal("provider request was not captured")
	}
}

func TestUsageChunkRecordsHourlyStats(t *testing.T) {
	usage := provider.UsageInfo{
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	}
	eng, ms, _, sid := newTestEngine(t,
		mock.WithChunks([]provider.Chunk{
			{Usage: &usage},
			{Delta: "ok"},
			{Done: true, Finish: provider.FinishStop},
		}),
		mock.WithDelay(time.Millisecond),
	)
	title := "has title"
	if _, err := ms.UpdateSession(context.Background(), sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "usage_1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	stats, err := ms.UsageHourlyStats(context.Background(), time.Now().Add(-time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats len=%d want 1: %+v", len(stats), stats)
	}
	stat := stats[0]
	if stat.Model != "mock-model" || stat.RequestCount != 1 || stat.TotalTokens() != 150 {
		t.Fatalf("stat wrong: %+v", stat)
	}
	sessionUsage, err := eng.SessionUsage(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if sessionUsage.RequestCount != 1 || sessionUsage.LastPromptTokens != 60 || sessionUsage.LastOutputTokens != 90 || sessionUsage.CumulativeTotalTokens != 150 {
		t.Fatalf("session usage wrong: %+v", sessionUsage)
	}
}

func TestMultipleUsageChunksCountOneRequest(t *testing.T) {
	inputUsage := provider.UsageInfo{InputUncachedTokens: 10, InputCachedTokens: 20}
	outputUsage := provider.UsageInfo{OutputContentTokens: 30, OutputReasoningTokens: 40}
	eng, ms, _, sid := newTestEngine(t,
		mock.WithChunks([]provider.Chunk{
			{Usage: &inputUsage},
			{Delta: "ok"},
			{Usage: &outputUsage},
			{Done: true, Finish: provider.FinishStop},
		}),
		mock.WithDelay(time.Millisecond),
	)
	title := "has title"
	if _, err := ms.UpdateSession(context.Background(), sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "usage_multi", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	stats, err := ms.UsageHourlyStats(context.Background(), time.Now().Add(-time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].RequestCount != 1 || stats[0].TotalTokens() != 100 {
		t.Fatalf("global usage wrong: %+v", stats)
	}
	sessionUsage, err := eng.SessionUsage(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if sessionUsage.RequestCount != 1 || sessionUsage.LastPromptTokens != 30 || sessionUsage.LastOutputTokens != 70 || sessionUsage.CumulativeTotalTokens != 100 {
		t.Fatalf("session usage wrong: %+v", sessionUsage)
	}
}

func TestProviderRequestCountRecordedWithoutUsageChunk(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"ok"}), mock.WithDelay(time.Millisecond))
	title := "has title"
	if _, err := ms.UpdateSession(context.Background(), sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "usage_empty", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	stats, err := ms.UsageHourlyStats(context.Background(), time.Now().Add(-time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats len=%d want 1: %+v", len(stats), stats)
	}
	if stats[0].RequestCount != 1 || stats[0].TotalTokens() != 0 {
		t.Fatalf("stat wrong: %+v", stats[0])
	}
	sessionUsage, err := eng.SessionUsage(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if sessionUsage.RequestCount != 1 || sessionUsage.CumulativeTotalTokens != 0 {
		t.Fatalf("session usage wrong: %+v", sessionUsage)
	}
}

func TestCompactWritesSummaryAndContextBoundary(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"## User Context\nremembered facts"}), mock.WithDelay(time.Millisecond))
	ctx := context.Background()
	appendEngineTestTurn(t, ms, sid, "1", "old user", "old assistant")
	appendEngineTestTurn(t, ms, sid, "2", "tail user 1", "tail assistant 1")
	appendEngineTestTurn(t, ms, sid, "3", "tail user 2", "tail assistant 2")

	res, err := eng.Compact(ctx, CompactInput{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceMessages != 2 || res.TailMessages != 4 || res.SummaryMessageID == "" || res.TurnID == "" {
		t.Fatalf("unexpected compact result: %+v", res)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	summary := msgs[len(msgs)-1]
	if summary.Role != store.RoleSummary || summary.Text != "## User Context\nremembered facts" {
		t.Fatalf("summary message not preserved: %+v", summary)
	}
	meta, ok := store.CompactMetadataFromMessage(summary)
	if !ok {
		t.Fatalf("unexpected compact metadata: %+v", meta)
	}
	if got, want := messageLabelsByID(msgs, meta.TailMessageIDs), []string{"user:tail user 1", "assistant:tail assistant 1", "user:tail user 2", "assistant:tail assistant 2"}; !sameStrings(got, want) {
		t.Fatalf("unexpected tail messages: got %v want %v", got, want)
	}
	if meta.SourceTurnCount != 1 || meta.TailTurnCount != 2 {
		t.Fatalf("unexpected compact turn counts: %+v", meta)
	}
	req, err := eng.builder.Build(ctx, sid, "mock-model", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		texts = append(texts, msg.Text)
	}
	joined := strings.Join(texts, "\n")
	if strings.Contains(joined, "old user") || strings.Contains(joined, "old assistant") {
		t.Fatalf("old raw messages should be compacted away: %v", texts)
	}
	if !strings.Contains(joined, "tail user 1") || !strings.Contains(joined, "tail assistant 2") {
		t.Fatalf("tail messages should remain raw: %v", texts)
	}
}

func TestCompactCountsSystemReminderAsInputTurn(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"## User Context\nremembered facts"}), mock.WithDelay(time.Millisecond))
	ctx := context.Background()
	appendEngineTestTurn(t, ms, sid, "1", "old user", "old assistant")
	appendEngineTestSystemTurn(t, ms, sid, "2", "system reminder", "system answer")
	appendEngineTestTurn(t, ms, sid, "3", "tail user", "tail assistant")

	res, err := eng.Compact(ctx, CompactInput{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceMessages != 2 || res.TailMessages != 4 {
		t.Fatalf("unexpected compact result: %+v", res)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := store.CompactMetadataFromMessage(msgs[len(msgs)-1])
	if !ok {
		t.Fatalf("compact metadata missing: %+v", msgs[len(msgs)-1])
	}
	if meta.SourceTurnCount != 1 || meta.TailTurnCount != 2 {
		t.Fatalf("unexpected compact turn counts: %+v", meta)
	}
	if got, want := messageLabelsByID(msgs, meta.TailMessageIDs), []string{"system:system reminder", "assistant:system answer", "user:tail user", "assistant:tail assistant"}; !sameStrings(got, want) {
		t.Fatalf("unexpected tail messages: got %v want %v", got, want)
	}
}

func TestAutoCompactRunsAfterCompletedTurn(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"auto compact summary"}), mock.WithDelay(time.Millisecond))
	ctx := context.Background()
	title := "has title"
	if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID:          "mock",
		DisplayName: "mock",
		Protocol:    "openai-compatible",
		Models: []store.ProviderModel{{
			ID:            "mock-model",
			ContextWindow: 100,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.SetSettings(ctx, map[string]string{
		config.SettingCompactAutoThresholdPercent: "1",
		config.SettingCompactTailInputTurns:       "2",
	}); err != nil {
		t.Fatal(err)
	}
	appendEngineTestTurn(t, ms, sid, "1", "old user 1", "old assistant 1")
	appendEngineTestTurn(t, ms, sid, "2", "old user 2", "old assistant 2")
	appendEngineTestTurn(t, ms, sid, "3", "tail user", "tail assistant")

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_auto", Text: "new user"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	eng.Wait()

	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var summary *store.Message
	for _, msg := range msgs {
		if _, ok := store.CompactMetadataFromMessage(msg); ok {
			summary = msg
		}
	}
	if summary == nil || summary.Role != store.RoleSummary || summary.Text != "auto compact summary" {
		t.Fatalf("auto compact summary missing: %+v", msgs)
	}
	meta, _ := store.CompactMetadataFromMessage(summary)
	if meta.SourceTurnCount != 2 || meta.TailTurnCount != 2 {
		t.Fatalf("unexpected compact metadata: %+v", meta)
	}
}

func TestAutoCompactDueSkipsQueuedInput(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"summary"}), mock.WithDelay(time.Millisecond))
	ctx := context.Background()
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID:          "mock",
		DisplayName: "mock",
		Protocol:    "openai-compatible",
		Models: []store.ProviderModel{{
			ID:            "mock-model",
			ContextWindow: 100,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.SetSettings(ctx, map[string]string{
		config.SettingCompactAutoThresholdPercent: "1",
	}); err != nil {
		t.Fatal(err)
	}
	appendEngineTestTurn(t, ms, sid, "1", "old user", "old assistant")
	if _, err := ms.QueueInput(ctx, store.QueueInputInput{
		SessionID:       sid,
		ClientMessageID: "queued_1",
		Text:            "queued",
		Provider:        "mock",
		Model:           "mock-model",
		Mode:            store.ModeChat,
	}); err != nil {
		t.Fatal(err)
	}

	due, err := eng.autoCompactDue(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("auto compact must not run while queued input exists")
	}
}

func TestSubmitPersistsThoughtParts(t *testing.T) {
	eng, ms, hub, sid := newTestEngine(t, mock.WithChunks([]provider.Chunk{
		{Part: provider.PartThought, Delta: "thinking"},
		{Part: provider.PartText, Delta: "answer"},
		{Done: true, Finish: provider.FinishStop},
	}), mock.WithDelay(time.Millisecond))
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	msgs, err := ms.ListMessages(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want user + thought + assistant text messages, got %+v", msgs)
	}
	if msgs[1].Kind != store.MessageKindThought || msgs[1].Text != "thinking" {
		t.Fatalf("thought should be its own message: %+v", msgs[1])
	}
	if msgs[2].Kind != store.MessageKindText || msgs[2].Text != "answer" {
		t.Fatalf("text should be its own assistant message: %+v", msgs[2])
	}

	seenThought, seenText := false, false
	timeout := time.After(time.Second)
	for !seenThought || !seenText {
		select {
		case ev := <-sub:
			if ev.Kind != event.TurnDelta {
				continue
			}
			switch ev.Part {
			case string(provider.PartThought):
				seenThought = ev.Delta == "thinking"
			case string(provider.PartText):
				seenText = ev.Delta == "answer"
			}
		case <-timeout:
			t.Fatalf("missing thought/text delta events: thought=%v text=%v", seenThought, seenText)
		}
	}
}

func TestSubmitRunsToolLoop(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &toolLoopClient{}
	runner := &recordingToolRunner{
		defs: []provider.ToolDef{{
			Name:        tool.TimeGetCurrent,
			Description: "Get time",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Capability:  store.ModeChat,
		}},
		result: tool.Result{Ok: true, Content: `{"iso":"2026-06-21T12:00:00+08:00"}`, SummaryKind: tool.SummaryReturnedFields, SummaryCount: 1},
	}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_tools"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "tools", Provider: "capture", Model: "tool-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "现在几点"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	if !hasToolDef(client.requests[0].Tools, tool.TimeGetCurrent) {
		t.Fatalf("tool definitions not injected: %+v", client.requests[0].Tools)
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if !hasProviderPart(last.Parts, provider.PartToolUse) || !hasProviderPart(last.Parts, provider.PartToolResult) {
		t.Fatalf("second provider call missing tool history: %+v", last.Parts)
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != tool.TimeGetCurrent || string(runner.calls[0].Args) != `{"timezone":"Asia/Singapore"}` {
		t.Fatalf("tool call not executed correctly: %+v", runner.calls)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[3].Text != "工具结果已收到" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	if msgs[1].Kind != store.MessageKindToolUse || msgs[1].Role != store.RoleAssistant {
		t.Fatalf("tool_use should be assistant message: %+v", msgs[1])
	}
	if msgs[2].Kind != store.MessageKindToolResult || msgs[2].Role != store.RoleTool {
		t.Fatalf("tool_result should be tool message: %+v", msgs[2])
	}
	parts := msgs[2].Parts
	if len(parts) != 1 || parts[0].Type != store.ContentPartToolResult || parts[0].Name != tool.TimeGetCurrent || !parts[0].Ok || parts[0].Content == "" || parts[0].SummaryKind != tool.SummaryReturnedFields || parts[0].SummaryCount != 1 {
		t.Fatalf("tool_result part wrong: %+v", parts)
	}
}

func TestSubmitRunsBuiltinBrowserToolLoop(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &browserToolLoopClient{}
	browserSvc := &engineTestBrowser{}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithTools(tool.NewBuiltinRunner(tool.WithBrowser(browserSvc))))
	ctx := context.Background()
	sid := "sess_browser_tool"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "browser", Provider: "capture", Model: "tool-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "看一下网页"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	if !hasToolDef(client.requests[0].Tools, tool.BrowserObserve) {
		t.Fatalf("browser tool definitions not injected: %+v", client.requests[0].Tools)
	}
	if browserSvc.observedSession != sid || browserSvc.observedTab != "tab_1" || browserSvc.observedOptions.MaxElements != 2 {
		t.Fatalf("browser observe not routed correctly: %+v", browserSvc)
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if !hasProviderPart(last.Parts, provider.PartToolUse) || !hasProviderPart(last.Parts, provider.PartToolResult) {
		t.Fatalf("second provider call missing browser tool history: %+v", last.Parts)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[3].Text != "浏览器结果已收到" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	parts := msgs[2].Parts
	if len(parts) != 1 || parts[0].Type != store.ContentPartToolResult || parts[0].Name != tool.BrowserObserve || !parts[0].Ok || !strings.Contains(parts[0].Content, "Example Domain") {
		t.Fatalf("browser tool result part wrong: %+v", parts)
	}
}

func TestSubmitRoutesFileReadImageToNextProviderRequest(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	expectedImageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
	if err := os.WriteFile(filepath.Join(tempDir, "image.png"), expectedImageBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	ms := memstore.New()
	hub := event.NewHub()
	client := &fileReadImageClient{}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithAttachmentHome(home), WithTools(tool.NewBuiltinRunner(tool.WithHomeDir(home))))
	ctx := context.Background()
	sid := "sess_image_tool"
	if err := ms.CreateSession(ctx, &store.Session{
		ID:         sid,
		Title:      "image tool",
		Provider:   "capture",
		Model:      "vision-model",
		ActiveMode: store.ModeWorkspace,
		ModeLease:  store.ModeLeaseSession,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "vision-model",
			Capabilities: &store.ModelCaps{Image: true, Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "看图"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	lastReq := client.requests[1]
	if len(lastReq.Messages) < 2 {
		t.Fatalf("second request should include tool history and image: %+v", lastReq.Messages)
	}
	imageMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if imageMsg.Role != provider.RoleUser || !hasProviderPart(imageMsg.Parts, provider.PartImage) {
		t.Fatalf("image should be routed as a user image message: %+v", imageMsg)
	}
	var routedImageBytes []byte
	for _, part := range imageMsg.Parts {
		if part.Type == provider.PartImage {
			routedImageBytes = part.Data
		}
	}
	if !bytes.Equal(routedImageBytes, expectedImageBytes) {
		t.Fatalf("unexpected image bytes: %x", routedImageBytes)
	}
}

func TestSubmitDoesNotRouteFileReadImageWhenCapabilityUnknown(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "image.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o600); err != nil {
		t.Fatal(err)
	}

	ms := memstore.New()
	hub := event.NewHub()
	client := &fileReadImageClient{}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithAttachmentHome(home), WithTools(tool.NewBuiltinRunner(tool.WithHomeDir(home))))
	ctx := context.Background()
	sid := "sess_image_tool_unknown"
	if err := ms.CreateSession(ctx, &store.Session{
		ID:         sid,
		Title:      "image tool",
		Provider:   "capture",
		Model:      "tool-model",
		ActiveMode: store.ModeWorkspace,
		ModeLease:  store.ModeLeaseSession,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "看图"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	lastReq := client.requests[1]
	if len(lastReq.Messages) == 0 {
		t.Fatalf("second request should include fallback attachment text")
	}
	for _, msg := range lastReq.Messages {
		if hasProviderPart(msg.Parts, provider.PartImage) {
			t.Fatalf("unknown image capability should not route image bytes: %+v", msg)
		}
	}
	imageMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if !strings.Contains(imageMsg.Text, "Source path:") {
		t.Fatalf("fallback should expose source path metadata: %+v", imageMsg)
	}
}

func TestSubmitBlocksToolOutsideCurrentMode(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &unauthorizedFileClient{}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"answer":"should not run"}`},
	}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_tool_gate"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "gate", Provider: "capture", Model: "tool-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "再查一次天气"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	if !hasToolDef(client.requests[0].Tools, tool.WebSearch) || !hasToolDef(client.requests[0].Tools, tool.RESTRequest) || hasToolDef(client.requests[0].Tools, tool.FileRead) {
		t.Fatalf("chat request should expose endpoint tools but not file tools: %+v", client.requests[0].Tools)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unauthorized tool should not execute: %+v", runner.calls)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[2].Kind != store.MessageKindToolResult {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	parts := msgs[2].Parts
	if len(parts) != 1 || parts[0].Type != store.ContentPartToolResult || parts[0].Name != tool.FileRead || parts[0].Ok {
		t.Fatalf("blocked tool result wrong: %+v", parts)
	}
	if !strings.Contains(parts[0].Content, "capability_required") {
		t.Fatalf("blocked tool result should explain capability gate: %+v", parts[0])
	}
	turn, err := ms.GetConversationTurn(ctx, sid, msgs[0].TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Mode != store.ModeChat {
		t.Fatalf("blocked tool must not upgrade mode: %+v", turn.Mode)
	}
}

func TestSubmitMarksUnknownTool(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &unknownToolClient{}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"answer":"should not run"}`},
	}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_unknown_tool"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "unknown", Provider: "capture", Model: "tool-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "查网页"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(runner.calls) != 0 {
		t.Fatalf("unknown tool should not execute: %+v", runner.calls)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[2].Kind != store.MessageKindToolResult {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	parts := msgs[2].Parts
	if len(parts) != 1 || parts[0].Type != store.ContentPartToolResult || parts[0].Name != "builtin_search_webpage_search" || parts[0].Ok {
		t.Fatalf("unknown tool result wrong: %+v", parts)
	}
	if !strings.Contains(parts[0].Content, "unknown_tool") || strings.Contains(parts[0].Content, "capability_required") {
		t.Fatalf("unknown tool result should not look like capability gate: %+v", parts[0])
	}
}

func TestCapabilityApprovalUpgradesTurnTools(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &capabilityClient{}
	runner := &recordingToolRunner{defs: tool.BuiltinDefinitions()}
	eng := New(ms, hub, mapResolver{"cap": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_capability"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "cap", Provider: "cap", Model: "cap-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "cap", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "cap-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	res, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "查一下最新资料"})
	if err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("approval request not emitted")
		}
	}
	if approval.ApprovalID == "" || approval.ApprovalKind != "capability" {
		t.Fatalf("bad approval event: %+v", approval)
	}
	pending := eng.PendingApprovals(sid)
	if len(pending) != 1 || pending[0].ID != approval.ApprovalID || pending[0].TargetMode != store.ModeWorkspace {
		t.Fatalf("pending approval not exposed: %+v", pending)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	if pending := eng.PendingApprovals(sid); len(pending) != 0 {
		t.Fatalf("pending approval should be cleared: %+v", pending)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	if !hasToolDef(client.requests[0].Tools, tool.RequestCapability) || !hasToolDef(client.requests[0].Tools, tool.TimeGetCurrent) || !hasToolDef(client.requests[0].Tools, tool.WebSearch) || !hasToolDef(client.requests[0].Tools, tool.WebFetch) || !hasToolDef(client.requests[0].Tools, tool.RESTRequest) || hasToolDef(client.requests[0].Tools, tool.FileRead) {
		t.Fatalf("chat tools wrong: %+v", client.requests[0].Tools)
	}
	if !hasToolDef(client.requests[1].Tools, tool.RequestCapability) || !hasToolDef(client.requests[1].Tools, tool.WebSearch) || !hasToolDef(client.requests[1].Tools, tool.WebFetch) || !hasToolDef(client.requests[1].Tools, tool.RESTRequest) || !hasToolDef(client.requests[1].Tools, tool.GraphQLRequest) || !hasToolDef(client.requests[1].Tools, tool.FileRead) {
		t.Fatalf("workspace tools wrong: %+v", client.requests[1].Tools)
	}
	turn, err := ms.GetConversationTurn(ctx, sid, res.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Mode != store.ModeWorkspace {
		t.Fatalf("turn mode not upgraded: %+v", turn)
	}
}

func TestSkillDraftSubmitApprovalAppliesDraft(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &skillDraftSubmitClient{}
	runner := &recordingToolRunner{
		defs: tool.BuiltinDefinitions(),
		result: tool.Result{
			Ok:      true,
			Content: `{"ok":true,"status":"pending_user_review","draft":{"id":"demo-skill","description":"Demo skill ready for review.","path":"skills-draft/demo-skill","validation":{"ok":true}},"fileCount":1}`,
		},
	}
	eng := New(ms, hub, mapResolver{"skill": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_skill_draft"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "skill", Provider: "skill", Model: "skill-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "skill", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "skill-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "创建 skill"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("approval request not emitted")
		}
	}
	if approval.ApprovalKind != ApprovalKindSkillDraft || !strings.Contains(string(approval.Payload), `"draft_id":"demo-skill"`) {
		t.Fatalf("bad skill draft approval: %+v", approval)
	}
	waitTurnDone(t, ms, sid)
	if pending := eng.PendingApprovals(sid); len(pending) != 1 || pending[0].ID != approval.ApprovalID {
		t.Fatalf("skill draft approval should remain pending after turn finishes: %+v", pending)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	if len(runner.appliedDrafts) != 1 || runner.appliedDrafts[0] != "demo-skill" {
		t.Fatalf("draft not applied: %+v", runner.appliedDrafts)
	}
}

func TestWorkspaceApprovalAllowsOptionalDirs(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &workspaceCapabilityClient{}
	runner := &recordingToolRunner{defs: tool.BuiltinDefinitions()}
	eng := New(ms, hub, mapResolver{"workspace": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_workspace"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "workspace", Provider: "workspace", Model: "workspace-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "workspace", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "workspace-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "写一个五子棋游戏"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("approval request not emitted")
		}
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeSession, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveMode != store.ModeWorkspace || sess.ModeLease != store.ModeLeaseSession {
		t.Fatalf("session mode not upgraded: %+v", sess)
	}
	if len(sess.WorkspaceDirs) != 0 {
		t.Fatalf("workspace dirs not stored: %+v", sess.WorkspaceDirs)
	}
	if len(client.requests) != 2 || !hasToolDef(client.requests[1].Tools, tool.FileList) {
		t.Fatalf("workspace tools not exposed after approval: %+v", client.requests)
	}
}

func TestWorkspaceApprovalTurnScopeGrantsDirsWithoutPersisting(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	dir := t.TempDir()
	client := &workspaceDirGrantClient{dir: dir}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"ok":true}`},
	}
	eng := New(ms, hub, mapResolver{"workspace": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_workspace_turn_dirs"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "workspace", Provider: "workspace", Model: "workspace-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "workspace", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "workspace-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 4},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "看看这个目录"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("approval request not emitted")
		}
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(runner.calls) != 1 {
		t.Fatalf("expected one file tool call after approval, got %+v", runner.calls)
	}
	if got := runner.calls[0].WorkspaceDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("turn-scoped workspace dir not passed to tool: %+v", got)
	}
	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.WorkspaceDirs) != 0 {
		t.Fatalf("turn-scoped approval must not persist dirs: %+v", sess.WorkspaceDirs)
	}
}

func TestCompletedOutputPersistsBeforeTurnFinish(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &capabilityAfterTextClient{}
	runner := &recordingToolRunner{defs: tool.BuiltinDefinitions()}
	eng := New(ms, hub, mapResolver{"cap": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_incremental_output"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "cap", Provider: "cap", Model: "cap-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "cap", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "cap-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "查最新赛况"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("approval request not emitted")
		}
	}
	if _, err := ms.RunningTurn(ctx, sid); err != nil {
		t.Fatalf("turn should still be running: %v", err)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 3 {
		t.Fatalf("completed output should be persisted while turn runs: %+v", msgs)
	}
	if msgs[1].Role != store.RoleAssistant || msgs[1].Kind != store.MessageKindText || msgs[1].Text != "需要查询最新信息。" {
		t.Fatalf("assistant text not persisted: %+v", msgs)
	}
	if msgs[2].Role != store.RoleAssistant || msgs[2].Kind != store.MessageKindToolUse || msgs[2].Parts[0].Name != tool.RequestCapability {
		t.Fatalf("tool use not persisted: %+v", msgs)
	}

	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
}

func TestSubmitIdempotent(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithScript([]string{"ok"}), mock.WithDelay(time.Millisecond))
	first, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	again, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Duplicate || again.TurnID != first.TurnID {
		t.Fatalf("duplicate submit must return original turn: %+v vs %+v", again, first)
	}
	msgs, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(msgs) != 2 {
		t.Fatalf("duplicate submit must not append messages, got %d", len(msgs))
	}
}

func TestSubmitQueuesWhileRunning(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithDelay(20*time.Millisecond))
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "slow"}); err != nil {
		t.Fatal(err)
	}
	queued, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c2", Text: "again"})
	if err != nil {
		t.Fatalf("running submit should queue, got %v", err)
	}
	if !queued.Queued || queued.ClientMessageID != "c2" || queued.Status != string(store.QueuedInputQueued) {
		t.Fatalf("unexpected queued result: %+v", queued)
	}
	inputs, err := ms.ListQueuedInputs(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].ClientMessageID != "c2" || inputs[0].Status != store.QueuedInputQueued {
		t.Fatalf("queued input not persisted: %+v", inputs)
	}

	// streaming 中列表项必须带 running 派生态(rail 运行态指示的数据源)
	list, err := ms.ListSessions(context.Background())
	if err != nil || len(list) != 1 || !list[0].Running {
		t.Fatalf("session must report running while streaming: %+v err=%v", list, err)
	}

	eng.Wait()

	list, _ = ms.ListSessions(context.Background())
	if list[0].Running {
		t.Fatal("running must clear after queued turns drain")
	}
	inputs, err = ms.ListQueuedInputs(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("queued input should be promoted after first turn: %+v", inputs)
	}
	msgs, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(msgs) != 4 || msgs[0].ClientMessageID != "c1" || msgs[2].ClientMessageID != "c2" {
		t.Fatalf("queued turn should run after first turn, got messages: %+v", msgs)
	}
}

func TestQueuedEditingBlocksDrain(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithDelay(20*time.Millisecond))
	ctx := context.Background()
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if res, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c2", Text: "draft"}); err != nil || !res.Queued {
		t.Fatalf("second submit should queue: %+v err=%v", res, err)
	}
	editing := store.QueuedInputEditing
	if _, err := ms.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{
		SessionID:       sid,
		ClientMessageID: "c2",
		Status:          &editing,
	}); err != nil {
		t.Fatal(err)
	}
	if res, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c3", Text: "third"}); err != nil || !res.Queued {
		t.Fatalf("later submit should queue behind editing input: %+v err=%v", res, err)
	}
	eng.Wait()

	if _, ok := turnByClientID(t, ms, sid, "c2"); ok {
		t.Fatal("editing queued input must not be promoted")
	}
	if _, ok := turnByClientID(t, ms, sid, "c3"); ok {
		t.Fatal("later queued input must not skip an editing head")
	}
	inputs, err := ms.ListQueuedInputs(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0].Status != store.QueuedInputEditing || inputs[1].Status != store.QueuedInputQueued {
		t.Fatalf("queue should remain blocked by editing input: %+v", inputs)
	}

	queued := store.QueuedInputQueued
	text := "edited draft"
	if _, err := ms.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{
		SessionID:       sid,
		ClientMessageID: "c2",
		Text:            &text,
		Status:          &queued,
	}); err != nil {
		t.Fatal(err)
	}
	eng.TryDrainQueued(sid)
	eng.Wait()

	turn, ok := turnByClientID(t, ms, sid, "c2")
	if !ok {
		t.Fatal("queued input should be promoted once editing ends")
	}
	if len(turn.Messages) == 0 || turn.Messages[0].Text != text {
		t.Fatalf("promoted turn should use edited text: %+v", turn.Messages)
	}
	if _, ok := turnByClientID(t, ms, sid, "c3"); !ok {
		t.Fatal("later queued input should drain after edited head")
	}
}

func TestCancelKeepsPartial(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t,
		mock.WithScript([]string{"part", "ial", " never", " ends"}),
		mock.WithDelay(30*time.Millisecond))
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond) // 让前几个 delta 流出
	if err := eng.Cancel(sid); err != nil {
		t.Fatal(err)
	}
	eng.Wait()

	evs, _ := ms.EventsAfter(context.Background(), sid, 0, 0)
	last := evs[len(evs)-1]
	if last.Kind != event.TurnCancelled {
		t.Fatalf("want turn.cancelled, got %+v", last)
	}
	msgs, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(msgs) != 2 || !msgs[1].Interrupted || msgs[1].Text == "" {
		t.Fatalf("partial output must be kept as interrupted message: %+v", msgs)
	}
	if last.AssistantMessageID != msgs[1].ID || !last.Interrupted {
		t.Fatalf("cancelled event must reference partial message: %+v", last)
	}

	if err := eng.Cancel(sid); !errors.Is(err, ErrNoRunningTurn) {
		t.Fatalf("cancel without running turn must fail, got %v", err)
	}
}

func TestProviderErrorFailsTurn(t *testing.T) {
	boom := errors.New("boom")
	eng, ms, _, sid := newTestEngine(t,
		mock.WithScript([]string{"a", "b"}),
		mock.WithDelay(time.Millisecond),
		mock.WithError(1, boom))
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()

	evs, _ := ms.EventsAfter(context.Background(), sid, 0, 0)
	last := evs[len(evs)-1]
	if last.Kind != event.TurnFailed || last.Error == "" {
		t.Fatalf("want turn.failed with error, got %+v", last)
	}
	msgs, _ := ms.ListMessages(context.Background(), sid, 0)
	if len(msgs) != 2 || !msgs[1].Interrupted {
		t.Fatalf("partial output before failure must be kept: %+v", msgs)
	}
}

func TestPerSessionProviderRouting(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	resolver := mapResolver{
		"alpha": mock.New(mock.WithScript([]string{"from-alpha"}), mock.WithDelay(time.Millisecond)),
		"beta":  mock.New(mock.WithScript([]string{"from-beta"}), mock.WithDelay(time.Millisecond)),
	}
	eng := New(ms, hub, resolver, ms)
	ctx := context.Background()

	sessA := &store.Session{ID: "sa", Provider: "alpha", Model: "mock-model"}
	sessB := &store.Session{ID: "sb", Provider: "beta", Model: "mock-model"}
	if err := ms.CreateSession(ctx, sessA); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(ctx, sessB); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alpha", "beta"} {
		if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
			ID:          id,
			DisplayName: id,
			Protocol:    "openai-compatible",
			BaseURL:     "http://127.0.0.1:11434/v1",
			Models:      []store.ProviderModel{{ID: "mock-model"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: "sa", ClientMessageID: "a1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: "sb", ClientMessageID: "b1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()

	msgsA, _ := ms.ListMessages(ctx, "sa", 0)
	msgsB, _ := ms.ListMessages(ctx, "sb", 0)
	if !strings.Contains(msgsA[1].Text, "from-alpha") {
		t.Fatalf("session A must use alpha provider: %q", msgsA[1].Text)
	}
	if !strings.Contains(msgsB[1].Text, "from-beta") {
		t.Fatalf("session B must use beta provider: %q", msgsB[1].Text)
	}

	// turns 快照:provider/model 落库
	turnsA, err := ms.RunningTurns(ctx)
	if err != nil || len(turnsA) != 0 {
		t.Fatalf("no running turns expected: %v %v", turnsA, err)
	}
	evsA, _ := ms.EventsAfter(ctx, "sa", 0, 0)
	if len(evsA) != 2 {
		t.Fatalf("want 2 lifecycle events for A, got %d", len(evsA))
	}
	if _, err := ms.GetSession(ctx, "sa"); err != nil {
		t.Fatal(err)
	}
}

func TestTurnSnapshotsProviderAndModel(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	capture := &captureClient{reqCh: make(chan provider.Request, 1)}
	eng := New(ms, hub, mapResolver{"capture": capture}, ms)
	ctx := context.Background()
	sid := "sess_1"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "named", Provider: "capture", Model: "snap-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		BaseURL: "http://unused",
		Models: []store.ProviderModel{{
			ID:            "snap-model",
			ContextWindow: 1000,
			Capabilities:  &store.ModelCaps{Image: true, Audio: false, Tools: true},
			Limits:        &store.ModelLimits{MaxOutputTokens: 8192, MaxToolLoops: 7},
			ProviderOptions: &store.ProviderOptions{
				OpenAI: map[string]any{"temperature": 0.6},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	again, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"})
	if err != nil || !again.Duplicate {
		t.Fatalf("duplicate expected: %+v %v", again, err)
	}
	if again.TurnID != res.TurnID {
		t.Fatal("duplicate must return original turn")
	}
	turn, err := findTurn(ms, sid, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Provider != "capture" || turn.Model != "snap-model" {
		t.Fatalf("turn snapshot wrong: provider=%q model=%q", turn.Provider, turn.Model)
	}
	var snap provider.ModelConfig
	if err := json.Unmarshal(turn.ModelConfig, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.ContextWindow != 1000 || snap.Capabilities == nil || !snap.Capabilities.Image || snap.Capabilities.Audio || !snap.Capabilities.Tools {
		t.Fatalf("turn config snapshot wrong: %+v", snap)
	}
	if snap.Limits == nil || snap.Limits.MaxOutputTokens != 8192 || snap.Limits.MaxToolLoops != 7 {
		t.Fatalf("limits snapshot missing: %+v", snap.Limits)
	}
	if v, ok := provider.FloatOption(snap.OpenAIOptions(), "temperature"); !ok || v != 0.6 {
		t.Fatalf("temperature snapshot missing: %+v", snap.OpenAIOptions())
	}
	if v, ok := provider.StringOption(snap.OpenAIOptions(), "reasoning_effort"); !ok || v != "high" {
		t.Fatalf("reasoning effort snapshot missing: %+v", snap.OpenAIOptions())
	}

	select {
	case req := <-capture.reqCh:
		if req.Model != "snap-model" {
			t.Fatalf("request model wrong: %+v", req)
		}
		if req.Config.ContextWindow != snap.ContextWindow {
			t.Fatalf("request config must use resolved snapshot: %+v", req.Config)
		}
		if v, ok := req.Config.MaxToolLoops(); !ok || v != 7 {
			t.Fatalf("request config missing maxToolLoops: %+v", req.Config.Limits)
		}
	default:
		t.Fatal("provider request was not captured")
	}
}

func TestSubmitUsesSessionReasoningEffort(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	capture := &captureClient{reqCh: make(chan provider.Request, 1)}
	eng := New(ms, hub, mapResolver{"capture": capture}, ms)
	ctx := context.Background()
	sid := "sess_reasoning"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "reasoning", Provider: "capture", Model: "snap-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture",
		Protocol:    "openai-compatible",
		BaseURL:     "http://unused",
		Models: []store.ProviderModel{{
			ID: "snap-model",
			ProviderOptions: &store.ProviderOptions{
				OpenAI: map[string]any{},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	effort := "medium"
	if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{ReasoningEffort: &effort}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	turn, err := findTurn(ms, sid, "c1")
	if err != nil {
		t.Fatal(err)
	}
	var snap provider.ModelConfig
	if err := json.Unmarshal(turn.ModelConfig, &snap); err != nil {
		t.Fatal(err)
	}
	if v, ok := provider.StringOption(snap.OpenAIOptions(), "reasoning_effort"); !ok || v != "medium" {
		t.Fatalf("session reasoning effort snapshot missing: %+v", snap.OpenAIOptions())
	}

	select {
	case req := <-capture.reqCh:
		if v, ok := provider.StringOption(req.Config.OpenAIOptions(), "reasoning_effort"); !ok || v != "medium" {
			t.Fatalf("session reasoning effort request missing: %+v", req.Config.OpenAIOptions())
		}
	default:
		t.Fatal("provider request was not captured")
	}
}

func TestDeepSeekReasoningEffortMapsToProviderOptions(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	capture := &captureClient{reqCh: make(chan provider.Request, 1)}
	eng := New(ms, hub, mapResolver{"capture": capture}, ms)
	ctx := context.Background()
	sid := "sess_deepseek_reasoning"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "reasoning", Provider: "capture", Model: "deepseek-v4-pro"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture",
		Brand:       "deepseek",
		Protocol:    "openai-compatible",
		BaseURL:     "http://unused",
		Models: []store.ProviderModel{{
			ID: "deepseek-v4-pro",
			ProviderOptions: &store.ProviderOptions{
				OpenAI: map[string]any{},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	effort := "xhigh"
	if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{ReasoningEffort: &effort}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	turn, err := findTurn(ms, sid, "c1")
	if err != nil {
		t.Fatal(err)
	}
	var snap provider.ModelConfig
	if err := json.Unmarshal(turn.ModelConfig, &snap); err != nil {
		t.Fatal(err)
	}
	opts := snap.OpenAIOptions()
	if v, ok := provider.StringOption(opts, "reasoning_effort"); !ok || v != "xhigh" {
		t.Fatalf("deepseek reasoning effort not passed as standard option: %+v", opts)
	}
	if _, ok := opts["thinking"]; ok {
		t.Fatalf("deepseek thinking should not be sent as openai-compatible option: %+v", opts)
	}

	select {
	case req := <-capture.reqCh:
		if v, ok := provider.StringOption(req.Config.OpenAIOptions(), "reasoning_effort"); !ok || v != "xhigh" {
			t.Fatalf("request deepseek reasoning effort not passed as standard option: %+v", req.Config.OpenAIOptions())
		}
	default:
		t.Fatal("provider request was not captured")
	}
}

type captureClient struct {
	reqCh chan provider.Request
}

func (c *captureClient) Name() string { return "capture" }

func (c *captureClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.reqCh <- req
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Delta: "ok"}
	out <- provider.Chunk{Done: true}
	close(out)
	return out, nil
}

type toolLoopClient struct {
	requests []provider.Request
}

func (c *toolLoopClient) Name() string { return "tool-loop" }

func (c *toolLoopClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_time",
			Name:      tool.TimeGetCurrent,
			ArgsDelta: `{"timezone":"Asia/Singapore"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "工具结果已收到"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type browserToolLoopClient struct {
	requests []provider.Request
}

func (c *browserToolLoopClient) Name() string { return "browser-tool-loop" }

func (c *browserToolLoopClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_browser",
			Name:      tool.BrowserObserve,
			ArgsDelta: `{"tabID":"tab_1","maxTextChars":120,"maxElements":2}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "浏览器结果已收到"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type engineTestBrowser struct {
	observedSession string
	observedTab     string
	observedOptions browser.ObserveOptions
}

func (b *engineTestBrowser) CreateTab(_ context.Context, sessionID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: "tab_1", SessionID: sessionID}, nil
}

func (b *engineTestBrowser) ListTabs(context.Context, string) ([]browser.TabSnapshot, error) {
	return nil, nil
}

func (b *engineTestBrowser) GetTab(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (b *engineTestBrowser) ReleaseTab(context.Context, string, string) error {
	return nil
}

func (b *engineTestBrowser) ReleaseSession(context.Context, string) error {
	return nil
}

func (b *engineTestBrowser) Open(_ context.Context, sessionID, tabID, rawURL string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID, URL: rawURL}, nil
}

func (b *engineTestBrowser) Reveal(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (b *engineTestBrowser) Back(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (b *engineTestBrowser) Forward(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (b *engineTestBrowser) Reload(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID}, nil
}

func (b *engineTestBrowser) Observe(_ context.Context, sessionID, tabID string, opts browser.ObserveOptions) (browser.ObserveResult, error) {
	b.observedSession = sessionID
	b.observedTab = tabID
	b.observedOptions = opts
	tab := browser.TabSnapshot{ID: tabID, SessionID: sessionID, URL: "https://example.com", Title: "Example Domain"}
	return browser.ObserveResult{
		Tab:        tab,
		Title:      "Example Domain",
		URL:        "https://example.com",
		ReadyState: "complete",
		Text:       "Example Domain",
		TextChars:  len("Example Domain"),
	}, nil
}

func (b *engineTestBrowser) Screenshot(context.Context, string, string, browser.ScreenshotOptions) (browser.ScreenshotResult, error) {
	return browser.ScreenshotResult{}, nil
}

func (b *engineTestBrowser) Click(context.Context, string, string, browser.ClickInput) (browser.ActionResult, error) {
	return browser.ActionResult{}, nil
}

func (b *engineTestBrowser) Type(context.Context, string, string, browser.TypeInput) (browser.ActionResult, error) {
	return browser.ActionResult{}, nil
}

func (b *engineTestBrowser) Scroll(context.Context, string, string, browser.ScrollInput) (browser.ActionResult, error) {
	return browser.ActionResult{}, nil
}

func (b *engineTestBrowser) Screencast(context.Context, string, string, *websocket.Conn) error {
	return nil
}

func (b *engineTestBrowser) Close() error { return nil }

type fileReadImageClient struct {
	requests []provider.Request
}

func (c *fileReadImageClient) Name() string { return "file-read-image" }

func (c *fileReadImageClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_image",
			Name:      tool.FileRead,
			ArgsDelta: `{"scope":"temp","path":"image.png"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已收到图片"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type unauthorizedFileClient struct {
	requests []provider.Request
}

func (c *unauthorizedFileClient) Name() string { return "unauthorized-file" }

func (c *unauthorizedFileClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_file",
			Name:      tool.FileRead,
			ArgsDelta: `{"scope":"workspace","path":"/tmp/demo.txt"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已被能力边界拦截"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type unknownToolClient struct {
	requests []provider.Request
}

func (c *unknownToolClient) Name() string { return "unknown-tool" }

func (c *unknownToolClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_unknown",
			Name:      "builtin_search_webpage_search",
			ArgsDelta: `{"query":"pudding"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "工具不存在"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type capabilityClient struct {
	requests []provider.Request
}

func (c *capabilityClient) Name() string { return "capability" }

func (c *capabilityClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_cap",
			Name:      tool.RequestCapability,
			ArgsDelta: `{"targetMode":"workspace","reason":"需要读取本地文件","risk":"local file access"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type capabilityAfterTextClient struct {
	requests []provider.Request
}

func (c *capabilityAfterTextClient) Name() string { return "capability-after-text" }

func (c *capabilityAfterTextClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 8)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Part: provider.PartText, Delta: "需要查询最新信息。"}
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_cap",
			Name:      tool.RequestCapability,
			ArgsDelta: `{"targetMode":"workspace","reason":"需要读取本地文件"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type workspaceCapabilityClient struct {
	requests []provider.Request
}

func (c *workspaceCapabilityClient) Name() string { return "workspace-capability" }

func (c *workspaceCapabilityClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_workspace_cap",
			Name:      tool.RequestCapability,
			ArgsDelta: `{"targetMode":"workspace","reason":"需要在工作区创建文件并运行本地命令","needsWorkspaceDir":true,"suggestedDirName":"gomoku"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已准备好工作区"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type workspaceDirGrantClient struct {
	dir      string
	requests []provider.Request
}

func (c *workspaceDirGrantClient) Name() string { return "workspace-dir-grant" }

func (c *workspaceDirGrantClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		args, _ := json.Marshal(map[string]any{
			"targetMode":    "workspace",
			"reason":        "需要读取用户附带的本地目录",
			"workspaceDirs": []string{c.dir},
		})
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_workspace_dir",
			Name:      tool.RequestCapability,
			ArgsDelta: string(args),
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_file_list",
			Name:      tool.FileList,
			ArgsDelta: `{"scope":"workspace","path":"."}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type skillDraftSubmitClient struct {
	requests []provider.Request
}

func (c *skillDraftSubmitClient) Name() string { return "skill-draft-submit" }

func (c *skillDraftSubmitClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_skill_submit",
			Name:      tool.SkillSubmit,
			ArgsDelta: `{"draft_id":"demo-skill"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "skill 已发布"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type recordingToolRunner struct {
	defs          []provider.ToolDef
	result        tool.Result
	calls         []tool.Call
	appliedDrafts []string
}

func (r *recordingToolRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return r.defs, nil
}

func (r *recordingToolRunner) Call(_ context.Context, call tool.Call) tool.Result {
	r.calls = append(r.calls, call)
	result := r.result
	result.CallID = call.CallID
	result.Name = call.Name
	return result
}

func (r *recordingToolRunner) ApplySkillDraft(_ context.Context, id string) error {
	r.appliedDrafts = append(r.appliedDrafts, id)
	return nil
}

func hasProviderPart(parts []provider.Part, typ provider.PartType) bool {
	for _, part := range parts {
		if part.Type == typ {
			return true
		}
	}
	return false
}

func hasToolDef(defs []provider.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func messageLabelsByID(messages []*store.Message, ids []string) []string {
	byID := make(map[string]*store.Message, len(messages))
	for _, msg := range messages {
		byID[msg.ID] = msg
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		msg := byID[id]
		if msg == nil {
			out = append(out, "<missing>:"+id)
			continue
		}
		out = append(out, string(msg.Role)+":"+msg.Text)
	}
	return out
}

func appendEngineTestTurn(t *testing.T, ms store.Store, sessionID, suffix, userText, assistantText string) {
	t.Helper()
	turnID := "turn_" + suffix
	if _, err := ms.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		UserMessageID:   "msg_" + suffix,
		ClientMessageID: "client_" + suffix,
		UserText:        userText,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         turnID,
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(assistantText),
	}); err != nil {
		t.Fatal(err)
	}
}

func appendEngineTestSystemTurn(t *testing.T, ms store.Store, sessionID, suffix, systemText, assistantText string) {
	t.Helper()
	turnID := "turn_" + suffix
	if _, err := ms.BeginSystemTurn(context.Background(), store.BeginSystemTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		SystemMessageID: "msg_" + suffix,
		ClientMessageID: "client_" + suffix,
		Text:            systemText,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         turnID,
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(assistantText),
	}); err != nil {
		t.Fatal(err)
	}
}

func findTurn(ms store.Store, sessionID, clientMessageID string) (*store.Turn, error) {
	res, err := ms.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID: sessionID, TurnID: "probe", UserMessageID: "probe",
		ClientMessageID: clientMessageID, UserText: "probe",
	})
	if err != nil {
		return nil, err
	}
	return res.Turn, nil
}

func turnByClientID(t *testing.T, ms store.Store, sessionID, clientMessageID string) (*store.ConversationTurn, bool) {
	t.Helper()
	page, err := ms.ListTurnsPage(context.Background(), sessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range page.Turns {
		if turn.ClientMessageID == clientMessageID {
			return turn, true
		}
	}
	return nil, false
}

func TestRecoverFinalizesResidualRunningTurns(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithDelay(time.Millisecond))
	// 直接在 store 造一个 running turn,模拟 daemon 在 turn 进行中被 kill
	if _, err := ms.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID: sid, TurnID: "turn_stale", UserMessageID: "msg_stale",
		ClientMessageID: "stale1", UserText: "killed mid-turn",
	}); err != nil {
		t.Fatal(err)
	}

	if err := eng.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := ms.RunningTurn(context.Background(), sid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("running turn must be finalized, got %v", err)
	}
	evs, _ := ms.EventsAfter(context.Background(), sid, 0, 0)
	last := evs[len(evs)-1]
	if last.Kind != event.TurnFailed || last.TurnID != "turn_stale" || last.Error == "" {
		t.Fatalf("want turn.failed for stale turn, got %+v", last)
	}
	// 恢复后 session 必须立即可用
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c-new", Text: "hi"}); err != nil {
		t.Fatalf("submit after recover must work: %v", err)
	}
	eng.Wait()
}

func TestSessionsAreIsolated(t *testing.T) {
	eng, ms, _, sidA := newTestEngine(t, mock.WithDelay(40*time.Millisecond))
	sessB := &store.Session{ID: "sess_2", Provider: "mock", Model: "mock-model"}
	if err := ms.CreateSession(context.Background(), sessB); err != nil {
		t.Fatal(err)
	}
	// A streaming 时 B 可以 submit(验收第 12 节)
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sidA, ClientMessageID: "a1", Text: "slow A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sessB.ID, ClientMessageID: "b1", Text: "fast B"}); err != nil {
		t.Fatalf("session B must be able to submit while A streams: %v", err)
	}
	eng.Wait()

	msgsA, _ := ms.ListMessages(context.Background(), sidA, 0)
	msgsB, _ := ms.ListMessages(context.Background(), sessB.ID, 0)
	for _, m := range msgsA {
		if m.SessionID != sidA {
			t.Fatalf("message leaked across sessions: %+v", m)
		}
	}
	for _, m := range msgsB {
		if m.SessionID != sessB.ID {
			t.Fatalf("message leaked across sessions: %+v", m)
		}
	}
	if len(msgsA) != 2 || len(msgsB) != 2 {
		t.Fatalf("want 2+2 messages, got %d+%d", len(msgsA), len(msgsB))
	}
}
