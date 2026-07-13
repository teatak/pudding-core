package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/attachment"
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

func TestAudioInputSupportedUsesResolvedModelCapability(t *testing.T) {
	eng, ms, _, sessionID := newTestEngine(t)
	ctx := context.Background()
	supported, err := eng.AudioInputSupported(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if supported {
		t.Fatal("model without audio capability should not support original audio input")
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID:          "mock",
		DisplayName: "mock",
		Protocol:    "openai-compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Models: []store.ProviderModel{{
			ID:           "mock-model",
			Capabilities: &store.ModelCaps{Audio: true},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	supported, err = eng.AudioInputSupported(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Fatal("audio-capable model should support original audio input")
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
		result:   tool.Result{Ok: true, Content: `{"iso":"2026-06-21T12:00:00+08:00"}`, SummaryKind: tool.SummaryReturnedFields, SummaryCount: 1},
		progress: []tool.Progress{{Stream: tool.ProgressStdout, Content: "live output\n"}},
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
	sub, cancelSub := hub.Subscribe(sid)
	defer cancelSub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "现在几点"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	var resultEvent event.Event
	var outputEvent event.Event
	for {
		select {
		case ev := <-sub:
			if ev.Kind == event.TurnTool && ev.Phase == "ok" {
				resultEvent = ev
			}
			if ev.Kind == event.TurnTool && ev.Phase == "output" {
				outputEvent = ev
			}
		default:
			goto doneDrain
		}
	}
doneDrain:
	if resultEvent.CallID != "call_time" || resultEvent.Ok == nil || !*resultEvent.Ok || resultEvent.Content == "" {
		t.Fatalf("tool result event missing content/ok: %+v", resultEvent)
	}
	if outputEvent.CallID != "call_time" || outputEvent.Stream != tool.ProgressStdout || outputEvent.Content != "live output\n" {
		t.Fatalf("tool output event missing stream/content: %+v", outputEvent)
	}

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

func TestEngineReleasesToolResources(t *testing.T) {
	runner := &recordingToolRunner{}
	eng := New(memstore.New(), event.NewHub(), mapResolver{}, memstore.New(), WithTools(runner))
	eng.ReleaseSessionResources(" sess_resources ")
	eng.Stop()
	eng.Stop()
	if len(runner.closedSessions) != 1 || runner.closedSessions[0] != "sess_resources" {
		t.Fatalf("session resources were not released: %+v", runner.closedSessions)
	}
	if runner.closeCount != 1 {
		t.Fatalf("tool resources must close once, got %d", runner.closeCount)
	}
}

func TestToolkitLoadRebuildsToolsAndResetsNextTurn(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &toolkitLoopClient{}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"ok":true,"clean":true,"fileCount":0}`},
	}
	eng := New(ms, hub, mapResolver{"toolkit": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_toolkit"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Title: "toolkit", Provider: "toolkit", Model: "toolkit-model",
		ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "toolkit", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID: "toolkit-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 4},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_toolkit_1", Text: "查看 Git 状态"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_toolkit_2", Text: "下一轮"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 4 {
		t.Fatalf("provider requests = %d, want 4", len(client.requests))
	}
	if hasToolDef(client.requests[0].Tools, tool.GitStatus) || !hasToolDef(client.requests[0].Tools, tool.ToolkitLoad) || !strings.Contains(client.requests[0].System, "code.git-read") {
		t.Fatalf("initial toolkit request wrong: tools=%v system=%s", client.requests[0].Tools, client.requests[0].System)
	}
	if !hasToolDef(client.requests[1].Tools, tool.GitStatus) || hasToolDef(client.requests[1].Tools, tool.GitCommit) {
		t.Fatalf("loaded toolkit request wrong: %+v", client.requests[1].Tools)
	}
	if !hasToolDef(client.requests[2].Tools, tool.GitStatus) {
		t.Fatal("loaded toolkit did not remain active within the turn")
	}
	if hasToolDef(client.requests[3].Tools, tool.GitStatus) {
		t.Fatal("toolkit leaked into the next turn")
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != tool.GitStatus {
		t.Fatalf("loaded tool was not called exactly once: %+v", runner.calls)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	seenLoadResult := false
	for _, message := range msgs {
		for _, part := range message.Parts {
			if part.Type == store.ContentPartToolResult && part.Name == tool.ToolkitLoad && part.Ok {
				seenLoadResult = strings.Contains(part.Content, `"loaded":["code.git-read"]`)
			}
		}
	}
	if !seenLoadResult {
		t.Fatal("toolkit load result was not canonicalized")
	}
}

func TestToolkitLoadEnforcesCapabilityAndPerTurnLimit(t *testing.T) {
	catalog := tool.BuildToolkitCatalog(tool.BuiltinDefinitions())
	state := newTurnToolkitState()
	state.catalog = catalog
	call := func(id, toolkitID string, mode store.AgentMode) (tool.Result, bool) {
		args, _ := json.Marshal(map[string]any{"toolkit_ids": []string{toolkitID}})
		return loadTurnToolkits(tool.Call{CallID: id, Name: tool.ToolkitLoad, Args: args}, mode, state)
	}
	if result, changed := call("work_code", "code.lsp", store.ModeWork); result.Ok || changed || !strings.Contains(result.Content, `"reason":"capability_required"`) {
		t.Fatalf("Work loaded Code toolkit: %+v", result)
	}
	if result, changed := call("load_1", "code.git-read", store.ModeCode); !result.Ok || !changed {
		t.Fatalf("first toolkit load failed: %+v", result)
	}
	if result, changed := call("load_2", "code.lsp", store.ModeCode); !result.Ok || !changed {
		t.Fatalf("second toolkit load failed: %+v", result)
	}
	if result, changed := call("load_3", "code.skill", store.ModeCode); result.Ok || changed || !strings.Contains(result.Content, `"reason":"toolkit_load_limit"`) {
		t.Fatalf("third toolkit load was not rejected: %+v", result)
	}
}

func TestExplicitAppLoadLoadsToolsForSession(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &appLoadClient{}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"ok":true}`},
	}
	apps := &mutableAppSource{defs: app.BuiltinDefinitions()}
	eng := New(ms, hub, mapResolver{"app-load": client}, ms, WithTools(runner), WithApps(apps))
	ctx := context.Background()
	sid := "sess_app_load"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Title: "app load", Provider: "app-load", Model: "app-model",
		ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "app-load", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID: "app-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 4},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_app_load_1", Text: "打开网页"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if len(client.requests) != 3 {
		t.Fatalf("provider requests = %d, want 3", len(client.requests))
	}
	if hasToolDef(client.requests[0].Tools, tool.BrowserOpen) || !hasToolDef(client.requests[0].Tools, tool.AppLoad) {
		t.Fatalf("browser must start unloaded: %+v", client.requests[0].Tools)
	}
	if !hasToolDef(client.requests[1].Tools, tool.BrowserOpen) {
		t.Fatalf("browser tools missing after app skill read: %+v", client.requests[1].Tools)
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != tool.BrowserOpen {
		t.Fatalf("unexpected app tool calls: %+v", runner.calls)
	}
	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(sess.LoadedAppIDs, []string{app.BuiltinBrowserID}) {
		t.Fatalf("loaded app ids = %+v", sess.LoadedAppIDs)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_app_load_2", Text: "继续"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if len(client.requests) != 4 || !hasToolDef(client.requests[3].Tools, tool.BrowserOpen) {
		t.Fatalf("loaded app did not persist across turns: %+v", client.requests)
	}
	chatDefs, _, err := eng.toolDefinitions(ctx, sid, store.ModeChat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasToolDef(chatDefs, tool.BrowserOpen) {
		t.Fatal("mode downgrade must hide loaded browser tools")
	}
	apps.defs[0].Enabled = false
	workDefs, _, err := eng.toolDefinitions(ctx, sid, store.ModeWork, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasToolDef(workDefs, tool.BrowserOpen) {
		t.Fatal("disabled app must hide loaded browser tools")
	}
	client.forceBrowser = true
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_app_load_3", Text: "再打开一次"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if len(runner.calls) != 1 {
		t.Fatalf("disabled app tool reached runner: %+v", runner.calls)
	}
	messages, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundDisabled := false
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == store.ContentPartToolResult && part.Name == tool.BrowserOpen && !part.Ok && strings.Contains(part.Content, `"reason":"app_disabled"`) {
				foundDisabled = true
			}
		}
	}
	if !foundDisabled {
		t.Fatal("disabled app call did not return app_disabled")
	}
	sess, err = ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(sess.LoadedAppIDs, []string{app.BuiltinBrowserID}) {
		t.Fatalf("mode or enablement change cleared loaded app ids: %+v", sess.LoadedAppIDs)
	}
}

func TestAppLoadIsExplicitAndAtomic(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	apps := app.NewService(t.TempDir(), nil)
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), ms, WithApps(apps))
	sid := "sess_app_load_atomic"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Provider: "mock", Model: "mock-model"}); err != nil {
		t.Fatal(err)
	}

	call := tool.Call{CallID: "load_browser", Name: tool.AppLoad, Args: json.RawMessage(`{"app_id":"browser"}`)}
	result, changed := eng.loadApp(ctx, sid, call, store.ModeChat)
	if result.Ok || changed || !strings.Contains(result.Content, `"reason":"capability_required"`) {
		t.Fatalf("Chat loaded Browser: %+v", result)
	}
	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.LoadedAppIDs) != 0 {
		t.Fatalf("failed load mutated session: %+v", sess.LoadedAppIDs)
	}

	call.Args = json.RawMessage(`{"app_id":"browser","skill_id":"missing"}`)
	result, changed = eng.loadApp(ctx, sid, call, store.ModeWork)
	if result.Ok || changed || !strings.Contains(result.Content, `"reason":"app_skill_not_found"`) {
		t.Fatalf("missing App skill loaded Browser: %+v", result)
	}
	sess, _ = ms.GetSession(ctx, sid)
	if len(sess.LoadedAppIDs) != 0 {
		t.Fatalf("skill failure mutated session: %+v", sess.LoadedAppIDs)
	}

	call.Args = json.RawMessage(`{"app_id":"browser"}`)
	result, changed = eng.loadApp(ctx, sid, call, store.ModeWork)
	if !result.Ok || !changed || !strings.Contains(result.Content, `"newlyLoaded":true`) {
		t.Fatalf("explicit App load failed: %+v", result)
	}
	result, changed = eng.loadApp(ctx, sid, call, store.ModeWork)
	if !result.Ok || changed || !strings.Contains(result.Content, `"alreadyLoaded":true`) {
		t.Fatalf("repeated App load should be idempotent: %+v", result)
	}
}

func TestTerminalAppToolsRequireLoadedCodeMode(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	runner := &recordingToolRunner{defs: tool.BuiltinDefinitions()}
	apps := &mutableAppSource{defs: app.BuiltinDefinitions()}
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(runner), WithApps(apps))
	ctx := context.Background()
	sid := "sess_terminal_app"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Provider: "mock", Model: "mock-model",
		ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession,
		LoadedAppIDs: []string{app.BuiltinTerminalID},
	}); err != nil {
		t.Fatal(err)
	}

	codeDefs, _, err := eng.toolDefinitions(ctx, sid, store.ModeCode, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{tool.CommandRun, tool.CommandStart, tool.CommandPoll, tool.CommandStop} {
		if !hasToolDef(codeDefs, name) {
			t.Fatalf("loaded Terminal missing %s", name)
		}
	}
	workDefs, _, err := eng.toolDefinitions(ctx, sid, store.ModeWork, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasToolDef(workDefs, tool.CommandStart) || hasToolDef(workDefs, tool.CommandPoll) || hasToolDef(workDefs, tool.CommandStop) {
		t.Fatal("mode downgrade exposed Terminal tools")
	}

	loaded := []string{}
	if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{LoadedAppIDs: &loaded}); err != nil {
		t.Fatal(err)
	}
	codeDefs, _, err = eng.toolDefinitions(ctx, sid, store.ModeCode, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasToolDef(codeDefs, tool.CommandRun) || hasToolDef(codeDefs, tool.CommandStart) {
		t.Fatalf("Code Core and Terminal boundary is wrong: %+v", codeDefs)
	}
}

func TestInstalledAppToolsBypassToolkitOnlyWhenLoaded(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	defs := append(tool.BuiltinDefinitions(), provider.ToolDef{
		Name: "app_mcp__search__hash", Description: "Search GitHub", Capability: store.ModeWork, AppID: "github",
	})
	runner := &recordingToolRunner{defs: defs}
	apps := &mutableAppSource{defs: []*app.Definition{{
		ID: "github", Name: "GitHub", Source: app.SourceInstalled, Enabled: true, RequiredMode: "work", DefaultSkillID: "github",
		Endpoints: map[string]app.Endpoint{
			"github_rest": {Kind: app.EndpointKindREST},
		},
	}}}
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(runner), WithApps(apps))
	ctx := context.Background()
	sid := "sess_installed_app"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Provider: "mock", Model: "mock-model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession,
		LoadedAppIDs: []string{"github"},
	}); err != nil {
		t.Fatal(err)
	}

	loadedDefs, catalog, err := eng.toolDefinitions(ctx, sid, store.ModeWork, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasToolDef(loadedDefs, tool.RESTRequest) || hasToolDef(loadedDefs, tool.GraphQLRequest) || !hasToolDef(loadedDefs, "app_mcp__search__hash") {
		t.Fatalf("installed app tools not routed directly: %+v", loadedDefs)
	}
	if len(runner.definitionAppIDs) == 0 || !sameStrings(runner.definitionAppIDs[len(runner.definitionAppIDs)-1], []string{"github"}) {
		t.Fatalf("scoped definitions received app ids: %+v", runner.definitionAppIDs)
	}
	for _, manifest := range catalog {
		if manifest.ID == "work.api" || strings.HasPrefix(manifest.ID, "app.") {
			t.Fatalf("legacy App toolkit remained: %+v", manifest)
		}
	}

	loaded := []string{}
	if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{LoadedAppIDs: &loaded}); err != nil {
		t.Fatal(err)
	}
	unloadedDefs, _, err := eng.toolDefinitions(ctx, sid, store.ModeWork, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasToolDef(unloadedDefs, tool.RESTRequest) || hasToolDef(unloadedDefs, "app_mcp__search__hash") {
		t.Fatalf("unloaded installed app exposed tools: %+v", unloadedDefs)
	}
}

func TestLoadedAppCannotCallAnotherAppsEndpoint(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &crossAppAPIClient{}
	runner := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: `{"ok":true}`}}
	apps := &mutableAppSource{
		defs: []*app.Definition{
			{ID: "github", Name: "GitHub", Source: app.SourceInstalled, Enabled: true, RequiredMode: "work", Endpoints: map[string]app.Endpoint{"github_rest": {Kind: app.EndpointKindREST}}},
			{ID: "jira", Name: "Jira", Source: app.SourceInstalled, Enabled: true, RequiredMode: "work", Endpoints: map[string]app.Endpoint{"jira_rest": {Kind: app.EndpointKindREST}}},
		},
		endpointApps: map[string]string{"github_rest": "github", "jira_rest": "jira"},
	}
	eng := New(ms, hub, mapResolver{"cross-app-api": client}, ms, WithTools(runner), WithApps(apps))
	ctx := context.Background()
	sid := "sess_cross_app_api"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Title: "cross app", Provider: "cross-app-api", Model: "app-model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession,
		LoadedAppIDs: []string{"github"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "cross-app-api", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{ID: "app-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 2}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_cross_app", Text: "读取 Jira"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if len(runner.calls) != 0 {
		t.Fatalf("unloaded App endpoint reached runner: %+v", runner.calls)
	}
	messages, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundBlocked := false
	var toolResults []string
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == store.ContentPartToolResult {
				toolResults = append(toolResults, part.Name+":"+part.Content)
			}
			if part.Type == store.ContentPartToolResult && part.Name == tool.RESTRequest && !part.Ok && strings.Contains(part.Content, `"appID":"jira"`) && strings.Contains(part.Content, `"reason":"app_not_loaded"`) {
				foundBlocked = true
			}
		}
	}
	if !foundBlocked {
		events, _ := ms.EventsAfter(ctx, sid, 0, 0)
		t.Fatalf("cross-App API call was not blocked as app_not_loaded: results=%+v requests=%d events=%+v", toolResults, len(client.requests), events)
	}
}

func TestMaxToolLoopsResetsAfterAssistantOutput(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &toolOutputResetClient{}
	runner := &recordingToolRunner{
		defs: []provider.ToolDef{{
			Name:        tool.TimeGetCurrent,
			Description: "Get time",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Capability:  store.ModeChat,
		}},
		result: tool.Result{Ok: true, Content: "ok"},
	}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_tool_reset"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "tools", Provider: "capture", Model: "tool-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 1},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "连续操作"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	evs, err := ms.EventsAfter(ctx, sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if last := evs[len(evs)-1]; last.Kind != event.TurnCompleted {
		t.Fatalf("assistant output should reset tool loop limit, got %+v", last)
	}
	if len(runner.calls) != 3 || len(client.requests) != 4 {
		t.Fatalf("all tool calls should run after resets: calls=%d requests=%d", len(runner.calls), len(client.requests))
	}
}

func TestMaxToolLoopsFailsConsecutiveToolOnlyCalls(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &toolOnlyLimitClient{}
	runner := &recordingToolRunner{
		defs: []provider.ToolDef{{
			Name:        tool.TimeGetCurrent,
			Description: "Get time",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Capability:  store.ModeChat,
		}},
		result: tool.Result{Ok: true, Content: "ok"},
	}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_tool_limit"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "tools", Provider: "capture", Model: "tool-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "capture", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "tool-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 1},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "连续操作"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	evs, err := ms.EventsAfter(ctx, sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := evs[len(evs)-1]
	if last.Kind != event.TurnFailed || !strings.Contains(last.Error, "max tool loops") {
		t.Fatalf("consecutive tool-only calls should still hit limit, got %+v", last)
	}
	if len(runner.calls) != 1 || len(client.requests) != 2 {
		t.Fatalf("second tool-only call should not run: calls=%d requests=%d", len(runner.calls), len(client.requests))
	}
}

func TestSubmitRunsBuiltinBrowserToolLoop(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	client := &browserToolLoopClient{}
	browserSvc := &engineTestBrowser{}
	apps := app.NewService(t.TempDir(), nil)
	eng := New(ms, hub, mapResolver{"capture": client}, ms,
		WithTools(tool.NewBuiltinRunner(tool.WithBrowser(browserSvc))),
		WithApps(apps),
	)
	ctx := context.Background()
	sid := "sess_browser_tool"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "browser", Provider: "capture", Model: "tool-model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession}); err != nil {
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

	if len(client.requests) != 3 {
		t.Fatalf("want 3 provider calls, got %d", len(client.requests))
	}
	if hasToolDef(client.requests[0].Tools, tool.BrowserObserve) || !hasToolDef(client.requests[0].Tools, tool.AppLoad) {
		t.Fatalf("browser app must start unloaded: %+v", client.requests[0].Tools)
	}
	if !hasToolDef(client.requests[1].Tools, tool.BrowserObserve) {
		t.Fatalf("browser tool definitions not injected after load: %+v", client.requests[1].Tools)
	}
	if browserSvc.observedSession != sid || browserSvc.observedTab != "tab_1" || browserSvc.observedOptions.MaxElements != 2 {
		t.Fatalf("browser observe not routed correctly: %+v", browserSvc)
	}
	last := client.requests[2].Messages[len(client.requests[2].Messages)-1]
	if !hasProviderPart(last.Parts, provider.PartToolUse) || !hasProviderPart(last.Parts, provider.PartToolResult) {
		t.Fatalf("second provider call missing browser tool history: %+v", last.Parts)
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 6 || msgs[5].Text != "浏览器结果已收到" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	parts := msgs[4].Parts
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
		ActiveMode: store.ModeCode,
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
	sub, cancelSub := hub.Subscribe(sid)
	defer cancelSub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "看图"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	var resultEvent event.Event
	for {
		select {
		case ev := <-sub:
			if ev.Kind == event.TurnTool && ev.Phase == "ok" {
				resultEvent = ev
			}
		default:
			goto doneDrain
		}
	}
doneDrain:
	if len(resultEvent.Attachments) != 1 || resultEvent.Attachments[0].AttachmentKey == "" || resultEvent.Attachments[0].MIME != "image/png" {
		t.Fatalf("tool result event should include routed attachment: %+v", resultEvent)
	}

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

func TestSubmitDoesNotRouteDisplayOnlyToolAttachmentToNextProviderRequest(t *testing.T) {
	home := t.TempDir()
	ms := memstore.New()
	hub := event.NewHub()
	client := &displayAttachmentToolClient{}
	sid := "sess_display_photo"
	runner := &recordingToolRunner{
		defs: tool.BuiltinDefinitions(),
		result: tool.Result{
			Ok:      true,
			Content: `{"ok":true,"attachmentKey":"sessions/sess_display_photo/blobs/photo.jpg","url":"/sessions/sess_display_photo/attachments/blobs/photo.jpg"}`,
			Attachments: []store.Attachment{{
				ID:            "att_photo",
				Name:          "photo.jpg",
				AttachmentKey: "sessions/sess_display_photo/blobs/photo.jpg",
				URL:           "/sessions/sess_display_photo/attachments/blobs/photo.jpg",
				MIME:          "image/jpeg",
				Size:          4,
				Origin:        attachment.OriginTool,
			}},
		},
	}
	eng := New(ms, hub, mapResolver{"capture": client}, ms, WithAttachmentHome(home), WithTools(runner))
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{
		ID:         sid,
		Title:      "photo display",
		Provider:   "capture",
		Model:      "vision-model",
		ActiveMode: store.ModeWork,
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

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "拍个照片给我展示一下"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	for _, msg := range client.requests[1].Messages {
		if hasProviderPart(msg.Parts, provider.PartImage) {
			t.Fatalf("display-only attachment should not be routed as image bytes: %+v", msg)
		}
	}
	msgs, err := ms.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[2].Kind != store.MessageKindToolResult {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	for _, part := range msgs[2].Parts {
		if part.Type == store.ContentPartAttachment {
			t.Fatalf("display-only tool attachment should not be stored as context part: %+v", part)
		}
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
		ActiveMode: store.ModeCode,
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
	if !strings.Contains(imageMsg.Text, "Image content: not provided") {
		t.Fatalf("fallback should warn that visual contents are unavailable: %+v", imageMsg)
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
	if !hasToolDef(client.requests[0].Tools, tool.WebSearch) || hasToolDef(client.requests[0].Tools, tool.RESTRequest) || hasToolDef(client.requests[0].Tools, tool.FileRead) {
		t.Fatalf("chat request should expose chat tools only: %+v", client.requests[0].Tools)
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

func TestNormalizeCapabilityTargetModeRejectsLegacyWorkspace(t *testing.T) {
	if mode, publicMode := normalizeCapabilityTargetMode(store.AgentMode("workspace")); mode != "" || publicMode != "" {
		t.Fatalf("legacy workspace target must be rejected: mode=%q public=%q", mode, publicMode)
	}
	if mode, publicMode := normalizeCapabilityTargetMode(store.AgentMode("project")); mode != "" || publicMode != "" {
		t.Fatalf("legacy project target must be rejected: mode=%q public=%q", mode, publicMode)
	}
	if mode, publicMode := normalizeCapabilityTargetMode(store.ModeWork); mode != store.ModeWork || publicMode != "work" {
		t.Fatalf("work target must remain stable: mode=%q public=%q", mode, publicMode)
	}
	if mode, publicMode := normalizeCapabilityTargetMode(store.ModeCode); mode != store.ModeCode || publicMode != "code" {
		t.Fatalf("code target must remain stable: mode=%q public=%q", mode, publicMode)
	}
}

func TestWorkCapabilityApprovalUpgradesOnlyCurrentTurn(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := New(ms, hub, mapResolver{}, ms)
	ctx := context.Background()
	sid := "sess_work_capability"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Provider: "test", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	type response struct {
		result   tool.Result
		mode     store.AgentMode
		upgraded bool
	}
	done := make(chan response, 1)
	go func() {
		result, mode, upgraded := eng.requestCapabilityApproval(ctx, sid, "turn_work", tool.Call{
			SessionID: sid,
			TurnID:    "turn_work",
			CallID:    "call_work",
			Name:      tool.RequestCapability,
			Args:      json.RawMessage(`{"targetMode":"work","reason":"需要操作浏览器"}`),
		}, store.ModeChat)
		done <- response{result: result, mode: mode, upgraded: upgraded}
	}()

	var approval event.Event
	select {
	case approval = <-sub:
	case <-time.After(time.Second):
		t.Fatal("work approval request not emitted")
	}
	if approval.Kind != event.ApprovalRequested {
		t.Fatalf("unexpected event: %+v", approval)
	}
	pending := eng.PendingApprovals(sid)
	if len(pending) != 1 || pending[0].TargetMode != store.ModeWork || len(pending[0].ProjectDirs) != 0 {
		t.Fatalf("unexpected work approval: %+v", pending)
	}
	offeredDir := t.TempDir()
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, []string{offeredDir}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if !got.result.Ok || !got.upgraded || got.mode != store.ModeWork {
			t.Fatalf("unexpected work approval result: %+v", got)
		}
		if strings.Contains(got.result.Content, offeredDir) {
			t.Fatalf("work approval must not grant project directories: %s", got.result.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("work capability request did not resume")
	}
	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveMode != store.ModeChat || sess.ProjectID != "" {
		t.Fatalf("turn-scoped Work approval must not persist: %+v", sess)
	}
}

func TestWorkCapabilityApprovalPersistsForSession(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := New(ms, hub, mapResolver{}, ms)
	ctx := context.Background()
	sid := "sess_work_session"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Provider: "test", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	done := make(chan store.AgentMode, 1)
	go func() {
		result, mode, upgraded := eng.requestCapabilityApproval(ctx, sid, "turn_work", tool.Call{
			SessionID: sid,
			TurnID:    "turn_work",
			CallID:    "call_work",
			Name:      tool.RequestCapability,
			Args:      json.RawMessage(`{"targetMode":"work","reason":"需要操作外部系统"}`),
		}, store.ModeChat)
		if !result.Ok || !upgraded {
			done <- ""
			return
		}
		done <- mode
	}()

	var approval event.Event
	select {
	case approval = <-sub:
	case <-time.After(time.Second):
		t.Fatal("work approval request not emitted")
	}
	if approval.Kind != event.ApprovalRequested {
		t.Fatalf("unexpected event: %+v", approval)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeSession, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case mode := <-done:
		if mode != store.ModeWork {
			t.Fatalf("approved mode = %q", mode)
		}
	case <-time.After(time.Second):
		t.Fatal("work capability request did not resume")
	}
	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveMode != store.ModeWork || sess.ModeLease != store.ModeLeaseSession || sess.ProjectID != "" {
		t.Fatalf("session-scoped Work approval persisted incorrectly: %+v", sess)
	}
}

func TestWorkCapabilityRejectsProjectDirectoryFields(t *testing.T) {
	ms := memstore.New()
	eng := New(ms, event.NewHub(), mapResolver{}, ms)
	ctx := context.Background()
	sid := "sess_work_dirs"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Provider: "test", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	result, mode, upgraded := eng.requestCapabilityApproval(ctx, sid, "turn_work", tool.Call{
		SessionID: sid,
		TurnID:    "turn_work",
		CallID:    "call_work",
		Name:      tool.RequestCapability,
		Args:      json.RawMessage(`{"targetMode":"work","reason":"需要操作浏览器","projectDirs":["/tmp"]}`),
	}, store.ModeChat)
	if result.Ok || upgraded || mode != store.ModeChat || !strings.Contains(result.Content, "project_dirs_not_allowed") {
		t.Fatalf("unexpected work directory rejection: result=%+v mode=%q upgraded=%t", result, mode, upgraded)
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
	if len(pending) != 1 || pending[0].ID != approval.ApprovalID || pending[0].TargetMode != store.ModeCode {
		t.Fatalf("pending approval not exposed: %+v", pending)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, []string{t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if pending := eng.PendingApprovals(sid); len(pending) != 0 {
		t.Fatalf("pending approval should be cleared: %+v", pending)
	}
	waitTurnDone(t, ms, sid)

	if len(client.requests) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(client.requests))
	}
	if !hasToolDef(client.requests[0].Tools, tool.RequestCapability) || !hasToolDef(client.requests[0].Tools, tool.TimeGetCurrent) || !hasToolDef(client.requests[0].Tools, tool.WebSearch) || !hasToolDef(client.requests[0].Tools, tool.WebFetch) || hasToolDef(client.requests[0].Tools, tool.RESTRequest) || hasToolDef(client.requests[0].Tools, tool.FileRead) {
		t.Fatalf("chat tools wrong: %+v", client.requests[0].Tools)
	}
	if !hasToolDef(client.requests[1].Tools, tool.RequestCapability) || !hasToolDef(client.requests[1].Tools, tool.ToolkitLoad) || !hasToolDef(client.requests[1].Tools, tool.WebSearch) || !hasToolDef(client.requests[1].Tools, tool.WebFetch) || !hasToolDef(client.requests[1].Tools, tool.FileRead) || hasToolDef(client.requests[1].Tools, tool.RESTRequest) || hasToolDef(client.requests[1].Tools, tool.GraphQLRequest) {
		t.Fatalf("code tools wrong: %+v", client.requests[1].Tools)
	}
	turn, err := ms.GetConversationTurn(ctx, sid, res.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Mode != store.ModeCode {
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
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "skill", Provider: "skill", Model: "skill-model", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession}); err != nil {
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

func TestProjectApprovalSessionScopeCreatesProject(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	dir := t.TempDir()
	client := &projectCapabilityClient{}
	runner := &recordingToolRunner{defs: tool.BuiltinDefinitions()}
	eng := New(ms, hub, mapResolver{"project": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_project"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "project", Provider: "project", Model: "project-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "project", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "project-model",
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
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeSession, nil); !errors.Is(err, ErrProjectDirsRequired) {
		t.Fatalf("expected project dirs required, got %v", err)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeSession, []string{dir}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)

	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveMode != store.ModeCode || sess.ModeLease != store.ModeLeaseSession {
		t.Fatalf("session mode not upgraded: %+v", sess)
	}
	if sess.ProjectID == "" {
		t.Fatalf("session project not bound: %+v", sess)
	}
	project, err := ms.GetProject(ctx, sess.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if got := project.RootDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("project dirs not stored: %+v", got)
	}
	if len(client.requests) != 2 || !hasToolDef(client.requests[1].Tools, tool.FileRead) || !hasToolDef(client.requests[1].Tools, tool.CommandRun) || hasToolDef(client.requests[1].Tools, tool.FileList) {
		t.Fatalf("default project tools wrong after approval: %+v", client.requests)
	}
}

func TestProjectApprovalTurnScopeGrantsDirsWithoutPersisting(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	dir := t.TempDir()
	client := &projectDirGrantClient{dir: dir}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"ok":true}`},
	}
	eng := New(ms, hub, mapResolver{"project": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_project_turn_dirs"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Title: "project", Provider: "project", Model: "project-model"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "project", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "project-model",
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
	if got := runner.calls[0].ProjectDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("turn-scoped project dir not passed to tool: %+v", got)
	}
	sess, err := ms.GetSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ProjectID != "" {
		t.Fatalf("turn-scoped approval must not persist project: %+v", sess.ProjectID)
	}
	projects, err := ms.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("turn-scoped approval must not create a project: %+v", projects)
	}
}

func TestProjectAskApprovalRequiresFileWriteApproval(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	dir := t.TempDir()
	project := &store.Project{
		ID:           "proj_write_approval",
		Name:         "write approval",
		RootDirs:     []string{dir},
		ApprovalMode: store.ApprovalAsk,
	}
	if err := ms.CreateProject(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	client := &fileWriteApprovalClient{}
	runner := &recordingToolRunner{
		defs:   tool.BuiltinDefinitions(),
		result: tool.Result{Ok: true, Content: `{"ok":true}`},
	}
	eng := New(ms, hub, mapResolver{"project": client}, ms, WithTools(runner))
	ctx := context.Background()
	sid := "sess_project_file_write_approval"
	if err := ms.CreateSession(ctx, &store.Session{
		ID:         sid,
		Title:      "project",
		Provider:   "project",
		Model:      "project-model",
		ActiveMode: store.ModeCode,
		ModeLease:  store.ModeLeaseSession,
		ProjectID:  project.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "project", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "project-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "写文件"}); err != nil {
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
	if approval.ApprovalKind != ApprovalKindToolCall || approval.CallID != "call_file_write" {
		t.Fatalf("bad tool approval: %+v", approval)
	}
	if strings.Contains(string(approval.Payload), "created by tool") {
		t.Fatalf("tool approval payload must not expose full write content: %s", approval.Payload)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("file tool executed before approval: %+v", runner.calls)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if len(runner.calls) != 1 || runner.calls[0].Name != tool.FileWrite {
		t.Fatalf("file tool did not execute after approval: %+v", runner.calls)
	}
	if got := runner.calls[0].ProjectDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("project dirs not passed to file tool: %+v", got)
	}
}

func TestPatchProposalApprovalCarriesDiffAndAppliesAfterApproval(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	hub := event.NewHub()
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("old text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := &store.Project{
		ID:           "proj_patch_approval",
		Name:         "patch approval",
		RootDirs:     []string{dir},
		ApprovalMode: store.ApprovalAsk,
	}
	if err := ms.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	client := &patchApprovalClient{}
	eng := New(ms, hub, mapResolver{"patch": client}, ms, WithTools(tool.NewBuiltinRunner()))
	sid := "sess_patch_approval"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Title: "patch", Provider: "patch", Model: "patch-model",
		ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession, ProjectID: project.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "patch", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID: "patch-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 4},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_patch", Text: "更新 notes"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(2 * time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("patch approval request not emitted")
		}
	}
	if approval.ApprovalKind != ApprovalKindToolCall || approval.CallID != "call_patch_apply" {
		t.Fatalf("unexpected patch approval: %+v", approval)
	}
	payload := string(approval.Payload)
	if !strings.Contains(payload, `"toolName":"builtin_patch_apply"`) || !strings.Contains(payload, `"proposalID":"patch_`) || !strings.Contains(payload, `"diff":"`) || !strings.Contains(payload, `-old text`) || !strings.Contains(payload, `+new text`) {
		t.Fatalf("patch approval payload is missing review data: %s", payload)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "old text\n" {
		t.Fatalf("patch changed file before approval: data=%q err=%v", data, err)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if data, err := os.ReadFile(target); err != nil || string(data) != "new text\n" {
		t.Fatalf("approved patch was not applied: data=%q err=%v", data, err)
	}
}

func TestGitCommitApprovalCarriesStagedDiffAndCommitsAfterApproval(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	hub := event.NewHub()
	dir := t.TempDir()
	runEngineGitTest(t, dir, "init")
	runEngineGitTest(t, dir, "config", "user.name", "Pudding Test")
	runEngineGitTest(t, dir, "config", "user.email", "pudding@example.test")
	runEngineGitTest(t, dir, "config", "commit.gpgsign", "false")
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("old text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runEngineGitTest(t, dir, "add", "notes.txt")
	runEngineGitTest(t, dir, "commit", "-m", "initial commit")
	if err := os.WriteFile(target, []byte("reviewed text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runEngineGitTest(t, dir, "add", "notes.txt")
	project := &store.Project{
		ID: "proj_git_commit", Name: "git commit", RootDirs: []string{dir}, ApprovalMode: store.ApprovalAuto,
	}
	if err := ms.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	client := &gitCommitApprovalClient{}
	eng := New(ms, hub, mapResolver{"git": client}, ms, WithTools(tool.NewBuiltinRunner()))
	sid := "sess_git_commit"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sid, Title: "git", Provider: "git", Model: "git-model",
		ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession, ProjectID: project.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "git", Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID: "git-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sid)
	defer unsub()

	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c_git", Text: "提交已暂存修改"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(2 * time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("Git commit approval request not emitted")
		}
	}
	payload := string(approval.Payload)
	if approval.CallID != "call_git_commit" || !strings.Contains(payload, `"operation":"git_commit"`) || !strings.Contains(payload, `"commitMessage":"reviewed commit"`) || !strings.Contains(payload, `"diff":"`) || !strings.Contains(payload, `+reviewed text`) {
		t.Fatalf("Git commit approval payload is missing staged review data: %s", payload)
	}
	if subject := runEngineGitOutput(t, dir, "log", "-1", "--format=%s"); subject != "initial commit" {
		t.Fatalf("commit executed before approval: %q", subject)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	if subject := runEngineGitOutput(t, dir, "log", "-1", "--format=%s"); subject != "reviewed commit" {
		t.Fatalf("approved commit was not created: %q", subject)
	}
}

func TestProjectApprovalModesClassifyCommands(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	project := &store.Project{
		ID:           "proj_command_policy",
		Name:         "command policy",
		RootDirs:     []string{t.TempDir()},
		ApprovalMode: store.ApprovalAuto,
	}
	if err := ms.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	sid := "sess_command_policy"
	if err := ms.CreateSession(ctx, &store.Session{ID: sid, Provider: "mock", Model: "mock", ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	eng := New(ms, event.NewHub(), registry.Static(mock.New()), ms)
	readRisk := tool.ToolRisk{Class: tool.RiskClassRead, LowRisk: true}
	lowRisk := tool.ToolRisk{Class: tool.RiskClassCommand, LowRisk: true}
	highRisk := tool.ToolRisk{Class: tool.RiskClassCommand}
	projectWrite := tool.ToolRisk{Class: tool.RiskClassWrite, LowRisk: true}
	protectedWrite := tool.ToolRisk{Class: tool.RiskClassWrite}

	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, readRisk); err != nil || required {
		t.Fatalf("auto should allow read tools: required=%v err=%v", required, err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, lowRisk); err != nil || required {
		t.Fatalf("auto should allow low-risk command: required=%v err=%v", required, err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, projectWrite); err != nil || required {
		t.Fatalf("auto should allow low-risk project write: required=%v err=%v", required, err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, protectedWrite); err != nil || !required {
		t.Fatalf("auto should ask for protected project write: required=%v err=%v", required, err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, highRisk); err != nil || !required {
		t.Fatalf("auto should ask for other commands: required=%v err=%v", required, err)
	}
	ask := store.ApprovalAsk
	if _, err := ms.UpdateProject(ctx, project.ID, store.ProjectUpdate{ApprovalMode: &ask}); err != nil {
		t.Fatal(err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, lowRisk); err != nil || !required {
		t.Fatalf("ask should require command approval: required=%v err=%v", required, err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, readRisk); err != nil || !required {
		t.Fatalf("ask should require read approval: required=%v err=%v", required, err)
	}
	full := store.ApprovalFull
	if _, err := ms.UpdateProject(ctx, project.ID, store.ProjectUpdate{ApprovalMode: &full}); err != nil {
		t.Fatal(err)
	}
	if _, required, err := eng.toolCallApprovalRequired(ctx, sid, highRisk); err != nil || required {
		t.Fatalf("full should allow command: required=%v err=%v", required, err)
	}
}

func TestRefineToolRiskKeepsPatchDeletionProtected(t *testing.T) {
	risk := tool.ToolRisk{Class: tool.RiskClassWrite, LowRisk: true}
	regular := refineToolRisk(tool.PatchApply, risk, map[string]any{"destructive": false})
	if regular.Class != tool.RiskClassWrite || !regular.LowRisk {
		t.Fatalf("regular patch should remain low-risk: %+v", regular)
	}
	destructive := refineToolRisk(tool.PatchApply, risk, map[string]any{"destructive": true})
	if destructive.Class != tool.RiskClassDestructive || destructive.LowRisk {
		t.Fatalf("patch deletion should require approval: %+v", destructive)
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

	if err := eng.ApproveApproval(ctx, sid, approval.ApprovalID, ApprovalScopeTurn, []string{t.TempDir()}); err != nil {
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

type toolOutputResetClient struct {
	requests []provider.Request
}

func (c *toolOutputResetClient) Name() string { return "tool-output-reset" }

func (c *toolOutputResetClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		out <- timeToolCallChunk("call_1")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Part: provider.PartThought, Delta: "我看一下。"}
		out <- timeToolCallChunk("call_2")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 3:
		out <- provider.Chunk{Part: provider.PartText, Delta: "继续检查。"}
		out <- timeToolCallChunk("call_3")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type toolOnlyLimitClient struct {
	requests []provider.Request
}

func (c *toolOnlyLimitClient) Name() string { return "tool-only-limit" }

func (c *toolOnlyLimitClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 2)
	if len(c.requests) <= 2 {
		out <- timeToolCallChunk(fmt.Sprintf("call_%d", len(c.requests)))
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func timeToolCallChunk(callID string) provider.Chunk {
	return provider.Chunk{Tool: &provider.ToolCallChunk{
		Index:     0,
		CallID:    callID,
		Name:      tool.TimeGetCurrent,
		ArgsDelta: `{}`,
	}}
}

type browserToolLoopClient struct {
	requests []provider.Request
}

type toolkitLoopClient struct {
	requests []provider.Request
}

type appLoadClient struct {
	requests     []provider.Request
	forceBrowser bool
}

func (c *appLoadClient) Name() string { return "app-load" }

func (c *appLoadClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 3)
	if c.forceBrowser {
		c.forceBrowser = false
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_browser_disabled", Name: tool.BrowserOpen, ArgsDelta: `{"url":"https://example.com"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
		close(out)
		return out, nil
	}
	switch len(c.requests) {
	case 1:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_browser_load", Name: tool.AppLoad, ArgsDelta: `{"app_id":"browser"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_browser_open", Name: tool.BrowserOpen, ArgsDelta: `{"url":"https://example.com"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type mutableAppSource struct {
	defs         []*app.Definition
	endpointApps map[string]string
}

func (s *mutableAppSource) ListDefinitions(context.Context) ([]*app.Definition, error) {
	out := make([]*app.Definition, 0, len(s.defs))
	for _, definition := range s.defs {
		out = append(out, app.CloneDefinition(definition))
	}
	return out, nil
}

func (s *mutableAppSource) ReadSkill(_ context.Context, appID, skillID string) (*app.SkillDetail, error) {
	if detail, ok := app.ReadBuiltinSkill(appID, skillID); ok {
		return detail, nil
	}
	for _, definition := range s.defs {
		if definition == nil || definition.ID != appID {
			continue
		}
		if !definition.Enabled {
			return nil, app.ErrDisabled
		}
		for _, skill := range definition.Skills {
			if skillID == skill.ID || skillID == skill.Name || skillID == skill.Path {
				return &app.SkillDetail{ID: skill.ID, Name: skill.Name, Description: skill.Description, Path: skill.Path, Content: "# " + definition.Name}, nil
			}
		}
	}
	return nil, app.ErrNotFound
}

func (s *mutableAppSource) ResolveEndpoint(_ context.Context, _ string, endpointName, _ string) (*app.EndpointBinding, error) {
	appID := s.endpointApps[endpointName]
	if appID == "" {
		return nil, &app.EndpointResolveError{Reason: "endpoint_not_found", Endpoint: endpointName}
	}
	return &app.EndpointBinding{AppID: appID, EndpointName: endpointName}, nil
}

type crossAppAPIClient struct {
	requests []provider.Request
}

func (c *crossAppAPIClient) Name() string { return "cross-app-api" }

func (c *crossAppAPIClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 3)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_jira_rest", Name: tool.RESTRequest, ArgsDelta: `{"endpoint":"jira_rest","path":"/issues"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "未执行"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func (c *toolkitLoopClient) Name() string { return "toolkit-loop" }

func (c *toolkitLoopClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 3)
	switch len(c.requests) {
	case 1:
		out <- toolkitLoadChunk("call_toolkit_git_read", "code.git-read")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_git_status", Name: tool.GitStatus, ArgsDelta: `{"scope":"project"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 3:
		out <- provider.Chunk{Part: provider.PartText, Delta: "Git 已检查"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "新一轮"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func (c *browserToolLoopClient) Name() string { return "browser-tool-loop" }

func (c *browserToolLoopClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_browser_load", Name: tool.AppLoad, ArgsDelta: `{"app_id":"browser"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_browser",
			Name:      tool.BrowserObserve,
			ArgsDelta: `{"tabID":"tab_1","maxTextChars":120,"maxElements":2}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "浏览器结果已收到"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func toolkitLoadChunk(callID string, toolkitIDs ...string) provider.Chunk {
	args, _ := json.Marshal(map[string]any{"toolkit_ids": toolkitIDs})
	return provider.Chunk{Tool: &provider.ToolCallChunk{
		Index: 0, CallID: callID, Name: tool.ToolkitLoad, ArgsDelta: string(args),
	}}
}

type engineTestBrowser struct {
	observedSession string
	observedTab     string
	observedOptions browser.ObserveOptions
}

func (b *engineTestBrowser) ProcessMode(context.Context, string) string {
	return "headless"
}

func (b *engineTestBrowser) CreateTab(_ context.Context, sessionID string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: "tab_1", SessionID: sessionID}, nil
}

func (b *engineTestBrowser) OpenNewTab(_ context.Context, sessionID, rawURL string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: "tab_1", SessionID: sessionID, URL: rawURL}, nil
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

func (b *engineTestBrowser) CloseSessionBrowser(context.Context, string) error {
	return nil
}

func (b *engineTestBrowser) ReleaseSession(context.Context, string) error {
	return nil
}

func (b *engineTestBrowser) Open(_ context.Context, sessionID, tabID, rawURL string) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: tabID, SessionID: sessionID, URL: rawURL}, nil
}

func (b *engineTestBrowser) Recover(_ context.Context, sessionID string, hint browser.RecoverHint) (browser.TabSnapshot, error) {
	return browser.TabSnapshot{ID: hint.TabID, SessionID: sessionID, URL: hint.URL, Title: hint.Title, Mode: hint.Mode}, nil
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

type displayAttachmentToolClient struct {
	requests []provider.Request
}

func (c *displayAttachmentToolClient) Name() string { return "display-attachment-tool" }

func (c *displayAttachmentToolClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_photo",
			Name:      tool.CameraCapture,
			ArgsDelta: `{}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "照片已拍好"}
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
			ArgsDelta: `{"scope":"project","path":"/tmp/demo.txt"}`,
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
			ArgsDelta: `{"targetMode":"code","reason":"需要读取本地文件","risk":"local file access"}`,
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
			ArgsDelta: `{"targetMode":"code","reason":"需要读取本地文件"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type projectCapabilityClient struct {
	requests []provider.Request
}

func (c *projectCapabilityClient) Name() string { return "project-capability" }

func (c *projectCapabilityClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	if len(c.requests) == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_project_cap",
			Name:      tool.RequestCapability,
			ArgsDelta: `{"targetMode":"code","reason":"需要在项目中创建文件并运行本地命令","needsProjectDir":true,"suggestedDirName":"gomoku"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "已准备好项目"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type projectDirGrantClient struct {
	dir      string
	requests []provider.Request
}

func (c *projectDirGrantClient) Name() string { return "project-dir-grant" }

func (c *projectDirGrantClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		args, _ := json.Marshal(map[string]any{
			"targetMode":  "code",
			"reason":      "需要读取用户附带的本地目录",
			"projectDirs": []string{c.dir},
		})
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_project_dir",
			Name:      tool.RequestCapability,
			ArgsDelta: string(args),
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- toolkitLoadChunk("call_files_read_toolkit", "code.files-read")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 3:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_file_list",
			Name:      tool.FileList,
			ArgsDelta: `{"scope":"project","path":"."}`,
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
	switch len(c.requests) {
	case 1:
		out <- toolkitLoadChunk("call_skill_toolkit", "code.skill")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_skill_submit",
			Name:      tool.SkillSubmit,
			ArgsDelta: `{"draft_id":"demo-skill"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "skill 已发布"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type fileWriteApprovalClient struct {
	requests []provider.Request
}

type patchApprovalClient struct {
	requests []provider.Request
}

type gitCommitApprovalClient struct {
	requests []provider.Request
}

func (c *gitCommitApprovalClient) Name() string { return "git-commit-approval" }

func (c *gitCommitApprovalClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		out <- toolkitLoadChunk("call_git_write_toolkit", "code.git-write")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index: 0, CallID: "call_git_commit", Name: tool.GitCommit,
			ArgsDelta: `{"scope":"project","message":"reviewed commit"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "提交完成"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func runEngineGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runEngineGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func (c *patchApprovalClient) Name() string { return "patch-approval" }

func (c *patchApprovalClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		args, _ := json.Marshal(map[string]any{
			"scope": "project",
			"files": []map[string]any{{"path": "notes.txt", "new_text": "new text\n"}},
		})
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "call_patch_propose", Name: tool.PatchPropose, ArgsDelta: string(args)}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		proposalID := patchProposalIDFromRequest(req)
		if proposalID == "" {
			close(out)
			return out, nil
		}
		args, _ := json.Marshal(map[string]any{"proposal_id": proposalID})
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "call_patch_apply", Name: tool.PatchApply, ArgsDelta: string(args)}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "补丁已应用"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func patchProposalIDFromRequest(req provider.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		for j := len(req.Messages[i].Parts) - 1; j >= 0; j-- {
			part := req.Messages[i].Parts[j]
			if part.Type != provider.PartToolResult || part.Name != tool.PatchPropose {
				continue
			}
			var payload struct {
				ProposalID string `json:"proposalID"`
			}
			if json.Unmarshal([]byte(part.Content), &payload) == nil {
				return payload.ProposalID
			}
		}
	}
	return ""
}

func (c *fileWriteApprovalClient) Name() string { return "file-write-approval" }

func (c *fileWriteApprovalClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	out := make(chan provider.Chunk, 4)
	switch len(c.requests) {
	case 1:
		out <- toolkitLoadChunk("call_files_write_toolkit", "code.files-write")
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	case 2:
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_file_write",
			Name:      tool.FileWrite,
			ArgsDelta: `{"scope":"project","path":"notes.txt","content":"created by tool"}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	default:
		out <- provider.Chunk{Part: provider.PartText, Delta: "文件已写入"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

type recordingToolRunner struct {
	defs             []provider.ToolDef
	result           tool.Result
	progress         []tool.Progress
	calls            []tool.Call
	appliedDrafts    []string
	closedSessions   []string
	closeCount       int
	definitionAppIDs [][]string
}

func (r *recordingToolRunner) CloseSession(sessionID string) {
	r.closedSessions = append(r.closedSessions, sessionID)
}

func (r *recordingToolRunner) Close() error {
	r.closeCount++
	return nil
}

func (r *recordingToolRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return r.defs, nil
}

func (r *recordingToolRunner) DefinitionsForApps(_ context.Context, _ string, appIDs []string) ([]provider.ToolDef, error) {
	r.definitionAppIDs = append(r.definitionAppIDs, append([]string(nil), appIDs...))
	return r.defs, nil
}

func (r *recordingToolRunner) Call(ctx context.Context, call tool.Call) tool.Result {
	r.calls = append(r.calls, call)
	for _, progress := range r.progress {
		tool.EmitProgress(ctx, progress)
	}
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
