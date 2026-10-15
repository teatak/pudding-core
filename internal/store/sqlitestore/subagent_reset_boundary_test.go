package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestSubagentResetRejectsOldRetryAndLateWrites(t *testing.T) {
	ctx := context.Background()
	st, in := resetLifecycleFixture(t)
	if _, err := st.ResetSubagent(ctx, in); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{
		SessionID: "child", TurnID: "old-retry", ClientMessageID: "old-retry",
		SystemMessageID: "old-retry-input", RetryOfTurnID: "child-turn", Text: "retry old task",
	}); !errors.Is(err, store.ErrInvalidRetry) {
		t.Fatalf("pre-reset retry accepted: %v", err)
	}
	if _, err := st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "child-turn", Parts: store.TextPart("late old output")}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late output accepted: %v", err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "child-turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("late completion")}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late completion accepted: %v", err)
	}
	if _, err := st.AppendTurnSteer(ctx, store.AppendTurnSteerInput{SessionID: "child", TurnID: "child-turn", ClientMessageID: "late-steer", UserMessageID: "late-steer-input", UserText: "old task reply"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late steer accepted: %v", err)
	}
	after, err := st.ListMessages(ctx, "child", 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected old operations changed history: %v", err)
	}
	// The boundary must not disable retries of genuine new work.
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "fresh", ClientMessageID: "fresh", UserMessageID: "fresh-input", UserText: "new independent task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "fresh", Status: store.TurnFailed, AssistantParts: store.TextPart("fresh partial result")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "child", TurnID: "fresh-retry", ClientMessageID: "fresh-retry", SystemMessageID: "fresh-retry-input", RetryOfTurnID: "fresh", Text: "retry current task"}); err != nil {
		t.Fatalf("new task retry rejected: %v", err)
	}
	after, err = st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	current := store.SubagentContextMessages(after)
	if len(current) != 3 || current[0].ID != "fresh-input" || current[2].ID != "fresh-retry-input" {
		t.Fatalf("retry crossed the reset boundary: %+v", current)
	}
}

func TestSubagentResetAndSendReceiptSurvivesDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	st, path := openTestStore(t)
	createTestSession(t, st, "parent")
	seedSettledSubagent(t, st, "parent", "child", store.TurnCancelled)
	if _, err := st.CollectChildResults(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "new task"}); err != nil {
		t.Fatal(err)
	}
	in := store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: "reset", SessionID: "child", ExpectedTurnID: "child-turn",
		Input: &store.QueueInputInput{SessionID: "child", ClientMessageID: "fresh", Text: "self-contained new task"}}
	accepted, err := st.ResetSubagent(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	// Startup marks interrupted parent execution failed, but a committed
	// operation's receipt must still replay without altering accepted work.
	if _, err := reopened.FinishTurn(ctx, store.FinishTurnInput{TurnID: "request", Status: store.TurnFailed}); err != nil {
		t.Fatal(err)
	}
	replayed, err := reopened.ResetSubagent(ctx, in)
	if err != nil || replayed == nil || !replayed.Duplicate || replayed.ResetTurnID != accepted.ResetTurnID || !replayed.Queued {
		t.Fatalf("durable replay: %+v %v", replayed, err)
	}
	if queued, err := reopened.ListQueuedInputs(ctx, "child"); err != nil || len(queued) != 1 || queued[0].Text != in.Input.Text {
		t.Fatalf("lost or repeated queued work: %+v %v", queued, err)
	}
	if used, err := reopened.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || used != 1 {
		t.Fatalf("reopen changed quota: %d %v", used, err)
	}
	after, err := reopened.ListMessages(ctx, "child", 0)
	if err != nil || !reflect.DeepEqual(before, after) || len(store.SubagentContextMessages(after)) != 0 {
		t.Fatalf("reopen changed the persisted boundary or history: %v", err)
	}
}

func TestSubagentResetAndSendWorksWhenAllIdentitiesAreArchived(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "parent")
	for i := 0; i < store.SubagentTotalLimit; i++ {
		id := fmt.Sprintf("child-%02d", i)
		seedSettledSubagent(t, st, "parent", id, store.TurnFailed)
		if err := st.SetSubtaskArchived(ctx, "parent", id, id+"-turn", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CollectChildResults(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "independent task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", "over")); !errors.Is(err, store.ErrSubagentTotalLimit) {
		t.Fatalf("archive released an identity slot: %v", err)
	}
	in := store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: "reset", SessionID: "child-00", ExpectedTurnID: "child-00-turn",
		Input: &store.QueueInputInput{SessionID: "child-00", ClientMessageID: "fresh", Text: "self-contained fresh task"}}
	reset, err := st.ResetSubagent(ctx, in)
	if err != nil || reset == nil || !reset.Queued {
		t.Fatalf("full archived group cannot reuse a clean context: %+v %v", reset, err)
	}
	children, err := st.ListChildSessions(ctx, "parent")
	if err != nil || len(children) != store.SubagentTotalLimit {
		t.Fatalf("reset changed identity count: %d %v", len(children), err)
	}
	archives, err := st.ListSubtaskArchives(ctx, "parent")
	if err != nil || len(archives) != store.SubagentTotalLimit-1 || !archives["child-00"].IsZero() {
		t.Fatalf("reset-and-send did not restore only its target: %+v %v", archives, err)
	}
	if used, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || used != 1 {
		t.Fatalf("reset-and-send must charge exactly one fresh context: %d %v", used, err)
	}
}
