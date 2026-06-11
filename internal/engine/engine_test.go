package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func newTestEngine(t *testing.T, opts ...mock.Option) (*Engine, store.Store, *event.Hub, string) {
	t.Helper()
	ms := memstore.New()
	hub := event.NewHub()
	eng := New(ms, hub, mock.New(opts...), "mock-model")
	sess := &store.Session{ID: "sess_1"}
	if err := ms.CreateSession(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return eng, ms, hub, sess.ID
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
	if err := eng.Cancel(sid); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	_ = ms
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

func TestSessionsAreIsolated(t *testing.T) {
	eng, ms, _, sidA := newTestEngine(t, mock.WithDelay(40*time.Millisecond))
	sessB := &store.Session{ID: "sess_2"}
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
