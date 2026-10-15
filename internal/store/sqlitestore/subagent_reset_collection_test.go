package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestSubagentResetAndSendCollectsOnlyTheNewTask(t *testing.T) {
	for _, status := range []store.TurnStatus{store.TurnCompleted, store.TurnFailed, store.TurnCancelled} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			st, in := resetLifecycleFixture(t)
			before, err := st.ListMessages(ctx, "child", 0)
			if err != nil {
				t.Fatal(err)
			}
			in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "fresh", Text: "fresh objective"}
			reset, err := st.ResetSubagent(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			assertNoResult := func() {
				t.Helper()
				if pending, err := st.HasUncollectedChildResults(ctx, "parent"); err != nil || pending {
					t.Fatalf("unexpected pending result: pending=%v err=%v", pending, err)
				}
				if events, err := st.CollectChildResults(ctx, "parent"); err != nil || len(events) != 0 {
					t.Fatalf("unexpected collection: events=%v err=%v", events, err)
				}
			}
			assertNoResult() // Neither the checkpoint nor the queued task is a result.
			if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "fresh-turn", UserMessageID: "fresh-input"}); err != nil {
				t.Fatal(err)
			}
			assertNoResult() // A running replacement must still suppress collection.
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "fresh-turn", Status: status, AssistantParts: store.TextPart("fresh result")}); err != nil {
				t.Fatal(err)
			}
			if pending, err := st.HasUncollectedChildResults(ctx, "parent"); err != nil || !pending {
				t.Fatalf("real %s result suppressed: pending=%v err=%v", status, pending, err)
			}
			if events, err := st.CollectChildResults(ctx, "parent"); err != nil || len(events) != 1 {
				t.Fatalf("real result must be delivered once: events=%v err=%v", events, err)
			}
			if _, err := st.GetMessage(ctx, "parent", "collaboration_result_fresh-turn"); err != nil {
				t.Fatalf("missing real task receipt: %v", err)
			}
			if _, err := st.GetMessage(ctx, "parent", "collaboration_result_"+reset.ResetTurnID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("maintenance checkpoint received a task receipt: %v", err)
			}
			assertNoResult()
			after, err := st.ListMessages(ctx, "child", 0)
			if err != nil || len(after) < len(before) || !reflect.DeepEqual(before, after[:len(before)]) {
				t.Fatalf("collecting replacement work changed earlier history: %v", err)
			}
		})
	}
}

func TestSubagentFreshContextBudgetSerializesDispatchAndReset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, _ := openTestStore(t)
	createTestSession(t, st, "parent")
	const candidates = 8
	before := make(map[string][]*store.Message)
	for i := 0; i < candidates; i++ {
		id := fmt.Sprintf("existing-%d", i)
		seedSettledSubagent(t, st, "parent", id, store.TurnCompleted)
		messages, err := st.ListMessages(ctx, id, 0)
		if err != nil {
			t.Fatal(err)
		}
		before[id] = messages
	}
	if _, err := st.CollectChildResults(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "one original request"}); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		id    string
		reset bool
		err   error
	}
	outcomes := make(chan outcome, 2*candidates)
	start := make(chan struct{})
	var wg sync.WaitGroup
	resetInput := func(id string) store.SubagentResetInput {
		return store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: "reset-" + id, SessionID: id, ExpectedTurnID: id + "-turn"}
	}
	for i := 0; i < candidates; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("existing-%d", i)
			_, err := st.ResetSubagent(ctx, resetInput(id))
			outcomes <- outcome{id: id, reset: true, err: err}
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("new-%d", i)
			_, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", id))
			outcomes <- outcome{id: id, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(outcomes)
	accepted, created := 0, 0
	for result := range outcomes {
		if result.err == nil {
			accepted++
			if result.reset {
				replay, err := st.ResetSubagent(ctx, resetInput(result.id))
				if err != nil || replay == nil || !replay.Duplicate {
					t.Fatalf("reset replay at capacity: %+v %v", replay, err)
				}
			} else {
				created++
				replay, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", result.id))
				if err != nil || replay == nil || !replay.Duplicate {
					t.Fatalf("dispatch replay at capacity: %+v %v", replay, err)
				}
			}
			continue
		}
		if !errors.Is(result.err, store.ErrSubtaskLimit) {
			t.Fatalf("unexpected admission failure: %+v", result)
		}
		if result.reset {
			messages, err := st.ListMessages(ctx, result.id, 0)
			if err != nil || !reflect.DeepEqual(messages, before[result.id]) {
				t.Fatalf("rejected reset changed history: %s %v", result.id, err)
			}
		} else if _, err := st.GetSession(ctx, result.id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rejected dispatch created an identity: %s %v", result.id, err)
		}
	}
	if accepted != store.SubtaskDispatchLimit {
		t.Fatalf("concurrent admission accepted %d, want %d", accepted, store.SubtaskDispatchLimit)
	}
	if used, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || used != accepted {
		t.Fatalf("durable budget=%d accepted=%d err=%v", used, accepted, err)
	}
	if children, err := st.ListChildSessions(ctx, "parent"); err != nil || len(children) != candidates+created {
		t.Fatalf("identity count=%d created=%d err=%v", len(children), created, err)
	}
}
