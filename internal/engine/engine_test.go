package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
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
	if evs[1].AssistantMessageID != msgs[1].ID {
		t.Fatalf("completed event not linked to assistant message")
	}

	// hub 侧应能看到 delta(不带 seq)与两条 lifecycle
	deltas := 0
	timeout := time.After(time.Second)
	for deltas < 3 {
		select {
		case ev := <-sub:
			if ev.Kind == event.TurnDelta {
				if ev.Seq != 0 {
					t.Fatalf("delta must not carry seq: %+v", ev)
				}
				deltas++
			}
		case <-timeout:
			t.Fatalf("saw only %d deltas", deltas)
		}
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
	if len(msgs) != 2 {
		t.Fatalf("want user + assistant messages, got %+v", msgs)
	}
	if msgs[1].Text != "answer" {
		t.Fatalf("text column should keep final answer only: %+v", msgs[1])
	}
	if got := msgs[1].Parts; len(got) != 2 || got[0].Type != store.ContentPartThought || got[0].Text != "thinking" || got[1].Type != store.ContentPartText || got[1].Text != "answer" {
		t.Fatalf("unexpected assistant parts: %+v", got)
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

func TestSubmitConflict409(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t, mock.WithDelay(50*time.Millisecond))
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "slow"}); err != nil {
		t.Fatal(err)
	}
	_, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "c2", Text: "again"})
	if !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("want ErrTurnRunning, got %v", err)
	}

	// streaming 中列表项必须带 running 派生态(rail 运行态指示的数据源)
	list, err := ms.ListSessions(context.Background())
	if err != nil || len(list) != 1 || !list[0].Running {
		t.Fatalf("session must report running while streaming: %+v err=%v", list, err)
	}

	if err := eng.Cancel(sid); err != nil {
		t.Fatal(err)
	}
	eng.Wait()

	list, _ = ms.ListSessions(context.Background())
	if list[0].Running {
		t.Fatal("running must clear after cancel")
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
	res, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "c1", Text: "hi"})
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
