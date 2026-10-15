package sqlitestore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func resetLifecycleFixture(t *testing.T) (*Store, store.SubagentResetInput) {
	t.Helper()
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "parent")
	seedSettledSubagent(t, st, "parent", "child", store.TurnFailed)
	if _, err := st.CollectChildResults(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "independent work"}); err != nil {
		t.Fatal(err)
	}
	return st, store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: "reset", SessionID: "child", ExpectedTurnID: "child-turn"}
}

func TestSubagentResetAndSendReplaysWithoutRepeatingTheReset(t *testing.T) {
	ctx := context.Background()
	st, in := resetLifecycleFixture(t)
	before, err := st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "new-work", Text: "new independent objective"}
	accepted, err := st.ResetSubagent(ctx, in)
	if err != nil || accepted == nil || !accepted.Queued || !accepted.ContextReset || accepted.Duplicate {
		t.Fatalf("admission: %+v %v", accepted, err)
	}
	if archives, err := st.ListSubtaskArchives(ctx, "parent"); err != nil || len(archives) != 0 {
		t.Fatalf("archived child not restored: %v %v", archives, err)
	}
	assertReplay := func() {
		t.Helper()
		again, err := st.ResetSubagent(ctx, in)
		if err != nil || again == nil || !again.Duplicate || again.ResetTurnID != accepted.ResetTurnID || again.ClientMessageID != "new-work" {
			t.Fatalf("replay: %+v %v", again, err)
		}
		if used, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || used != 1 {
			t.Fatalf("duplicate admission: %d %v", used, err)
		}
	}
	assertReplay()
	if queue, err := st.ListQueuedInputs(ctx, "child"); err != nil || len(queue) != 1 {
		t.Fatalf("duplicate queued work: %v %v", queue, err)
	}
	if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "new-turn", UserMessageID: "new-message"}); err != nil {
		t.Fatal(err)
	}
	assertReplay() // A lost response must not reset the already-running task.
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "new-turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("new result")}); err != nil {
		t.Fatal(err)
	}
	assertReplay()
	after, err := st.ListMessages(ctx, "child", 0)
	if err != nil || len(after) != len(before)+3 || !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatalf("reset lost or rewrote canonical history: %v", err)
	}
	current := store.SubagentContextMessages(after)
	if len(current) != 2 || current[0].ID != "new-message" || current[1].Text != "new result" {
		t.Fatalf("new context crossed the reset boundary: %+v", current)
	}
	changed := in
	changed.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "new-work", Text: "different objective"}
	if _, err := st.ResetSubagent(ctx, changed); !errors.Is(err, store.ErrSubagentResetConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
}

func TestSubagentResetEnqueueFailureRollsBackTheWholeOperation(t *testing.T) {
	ctx := context.Background()
	st, in := resetLifecycleFixture(t)
	before, err := st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := st.ListSubtaskArchives(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	// This identity already belongs to the original child turn. Enqueue sees a
	// duplicate only after the reset checkpoint has been tentatively written.
	in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "child", Text: "new objective"}
	if _, err := st.ResetSubagent(ctx, in); !errors.Is(err, store.ErrSubagentResetConflict) {
		t.Fatalf("expected enqueue conflict, got %v", err)
	}
	after, err := st.ListMessages(ctx, "child", 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("history changed on failure: %v", err)
	}
	afterArchives, err := st.ListSubtaskArchives(ctx, "parent")
	if err != nil || !reflect.DeepEqual(archives, afterArchives) {
		t.Fatalf("archive changed on failure: %v", err)
	}
	if queue, err := st.ListQueuedInputs(ctx, "child"); err != nil || len(queue) != 0 {
		t.Fatalf("failed reset left queued work: %v %v", queue, err)
	}
	if used, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || used != 0 {
		t.Fatalf("failed reset consumed quota: %d %v", used, err)
	}
}

func TestSubagentResetAndDispatchUseTheSameRequestBudget(t *testing.T) {
	ctx := context.Background()
	st, in := resetLifecycleFixture(t)
	reset, err := st.ResetSubagent(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", "over")); !errors.Is(err, store.ErrSubtaskLimit) {
		t.Fatalf("dispatch escaped reset budget: %v", err)
	}
	another := in
	another.CallID, another.ExpectedTurnID = "over-reset", reset.ResetTurnID
	if _, err := st.ResetSubagent(ctx, another); !errors.Is(err, store.ErrSubtaskLimit) {
		t.Fatalf("reset escaped dispatch budget: %v", err)
	}
	if _, err := st.ResetSubagent(ctx, in); err != nil {
		t.Fatalf("duplicate reset rejected at full quota: %v", err)
	}
	if _, err := st.QueueChildInput(ctx, "parent", "request", store.QueueInputInput{SessionID: "child", ClientMessageID: "continue", Text: "related work without reset"}); err != nil {
		t.Fatalf("ordinary send consumed quota: %v", err)
	}
	if used, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || used != 3 {
		t.Fatalf("shared quota: %d %v", used, err)
	}
}

func TestSubagentResetCheckpointIsNotACompletedTaskResult(t *testing.T) {
	ctx := context.Background()
	st, in := resetLifecycleFixture(t)
	reset, err := st.ResetSubagent(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Queued {
		t.Fatal("standalone reset dispatched work")
	}
	if archives, err := st.ListSubtaskArchives(ctx, "parent"); err != nil || len(archives) != 1 {
		t.Fatalf("standalone reset restored archive: %v", err)
	}
	if pending, err := st.HasUncollectedChildResults(ctx, "parent"); err != nil || pending {
		t.Fatalf("reset checkpoint became a pending task result: %v", err)
	}
	if events, err := st.CollectChildResults(ctx, "parent"); err != nil || len(events) != 0 {
		t.Fatalf("reset checkpoint emitted task results: %v", err)
	}
	if _, err := st.GetMessage(ctx, "parent", "collaboration_result_"+reset.ResetTurnID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reset manufactured a completed task receipt: %v", err)
	}
}
