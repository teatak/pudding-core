package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func resetFixture(t *testing.T) (*Store, string, store.SubagentResetInput) {
	t.Helper()
	ctx := context.Background()
	st, path := openTestStore(t)
	createTestSession(t, st, "parent")
	completedSubtask(t, st, "parent", "child")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-message", UserText: "new work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CollectChildResults(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	return st, path, store.SubagentResetInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: "reset-call", SessionID: "child", ExpectedTurnID: "child-turn"}
}

func TestSubagentResetAndSendIsAtomicAndIdempotentAcrossRestart(t *testing.T) {
	ctx := context.Background()
	st, path, in := resetFixture(t)
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "next-task", Text: "Independent task with complete new context", Provider: "mock", Model: "model", Mode: store.ModeWork}
	result, err := st.ResetSubagent(ctx, in)
	if err != nil || !result.Queued || result.Duplicate || !result.ContextReset {
		t.Fatalf("reset-and-send: %+v %v", result, err)
	}
	messages, err := st.ListMessages(ctx, "child", 0)
	if err != nil || len(messages) != len(before)+1 || !reflect.DeepEqual(before, messages[:len(before)]) {
		t.Fatalf("reset altered historical messages: %v", err)
	}
	if _, ok := store.SubagentResetFromMessage(messages[len(messages)-1]); !ok {
		t.Fatal("no canonical reset marker")
	}
	if len(store.SubagentContextMessages(messages)) != 0 {
		t.Fatal("old context survived reset before queued task starts")
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "parent"); len(archives) != 0 {
		t.Fatal("reset-and-send did not restore archive")
	}
	if queued, _ := st.ListQueuedInputs(ctx, "child"); len(queued) != 1 || queued[0].Text != in.Input.Text {
		t.Fatalf("new task was not queued exactly once: %+v", queued)
	}
	if n, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || n != 1 {
		t.Fatalf("reset-and-send charged %d fresh contexts: %v", n, err)
	}
	if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "next-turn", UserMessageID: "next-message"}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	replayed, err := st.ResetSubagent(ctx, in)
	if err != nil || !replayed.Duplicate || replayed.ResetTurnID != result.ResetTurnID {
		t.Fatalf("replay after task start/restart: %+v %v", replayed, err)
	}
	if n, _ := st.SubagentFreshContextCount(ctx, "parent", "request"); n != 1 {
		t.Fatal("replay charged a second reset")
	}
	changed := in
	changed.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "next-task", Text: "different task"}
	if _, err := st.ResetSubagent(ctx, changed); !errors.Is(err, store.ErrSubagentResetConflict) {
		t.Fatalf("same identity accepted different reset arguments: %v", err)
	}
	messages, _ = st.ListMessages(ctx, "child", 0)
	current := store.SubagentContextMessages(messages)
	if len(current) != 1 || current[0].Text != in.Input.Text {
		t.Fatalf("new context contains old task: %+v", current)
	}
}

func TestSubagentResetOnlyPreservesArchiveAndHasNoTaskResult(t *testing.T) {
	ctx := context.Background()
	st, _, in := resetFixture(t)
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); err != nil {
		t.Fatal(err)
	}
	result, err := st.ResetSubagent(ctx, in)
	if err != nil || result.Queued {
		t.Fatalf("reset only: %+v %v", result, err)
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "parent"); len(archives) != 1 {
		t.Fatal("reset-only changed archive state")
	}
	if queued, _ := st.ListQueuedInputs(ctx, "child"); len(queued) != 0 {
		t.Fatal("reset-only started work")
	}
	if pending, err := st.HasUncollectedChildResults(ctx, "parent"); err != nil || pending {
		t.Fatalf("checkpoint treated as uncollected task result: %v", err)
	}
	if events, err := st.CollectChildResults(ctx, "parent"); err != nil || len(events) != 0 {
		t.Fatalf("checkpoint generated a parent result: %v", err)
	}
	// Explicit standalone reset followed by normal send costs only one reset.
	if _, err := st.QueueChildInput(ctx, "parent", "request", store.QueueInputInput{SessionID: "child", ClientMessageID: "followup", Text: "new task"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.SubagentFreshContextCount(ctx, "parent", "request"); n != 1 {
		t.Fatalf("normal send charged an extra fresh context: %d", n)
	}
}

func TestSubagentResetFailuresLeaveCheckpointArchiveAndQueueUnchanged(t *testing.T) {
	for _, reason := range []string{"stale", "queued", "running", "uncollected", "wrong-parent", "stopped", "budget", "bad-input"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			st, _, in := resetFixture(t)
			in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "new", Text: "new work"}
			if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); err != nil {
				t.Fatal(err)
			}
			var want error
			switch reason {
			case "stale":
				in.ExpectedTurnID, want = "stale", store.ErrSubagentStateChanged
			case "queued":
				_, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "existing", Text: "already queued"})
				if err != nil { t.Fatal(err) }
				want = store.ErrSubagentResetBusy
			case "running":
				_, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "running", ClientMessageID: "running", UserMessageID: "running-input", UserText: "running task"})
				if err != nil { t.Fatal(err) }
				in.ExpectedTurnID, want = "running", store.ErrSubagentResetBusy
			case "uncollected":
				_, err := st.db.ExecContext(ctx, `DELETE FROM messages WHERE id='collaboration_result_child-turn'`)
				if err != nil { t.Fatal(err) }
				want = store.ErrSubagentResultUncollected
			case "wrong-parent":
				createTestSession(t, st, "other")
				in.ParentSessionID, want = "other", store.ErrInvalidSessionRelation
			case "stopped":
				if _, err := st.StopCollaboration(ctx, "parent"); err != nil { t.Fatal(err) }
				want = store.ErrCollaborationStopped
			case "budget":
				for i := 0; i < 3; i++ {
					id := fmt.Sprintf("budget-%d", i)
					_, err := st.DispatchChild(ctx, store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: id, Child: &store.Session{ID: id, Provider: "mock", Model: "model"}, Input: store.QueueInputInput{SessionID: id, ClientMessageID: id, Text: "work"}})
					if err != nil { t.Fatal(err) }
				}
				want = store.ErrSubtaskLimit
			case "bad-input":
				in.Input.SessionID, want = "wrong-child", store.ErrInvalidSessionRelation
			}
			messages, _ := st.ListMessages(ctx, "child", 0)
			archives, _ := st.ListSubtaskArchives(ctx, "parent")
			queue, _ := st.ListQueuedInputs(ctx, "child")
			if _, err := st.ResetSubagent(ctx, in); !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
			afterMessages, _ := st.ListMessages(ctx, "child", 0)
			afterArchives, _ := st.ListSubtaskArchives(ctx, "parent")
			afterQueue, _ := st.ListQueuedInputs(ctx, "child")
			if !reflect.DeepEqual(messages, afterMessages) || !reflect.DeepEqual(archives, afterArchives) || !reflect.DeepEqual(queue, afterQueue) {
				t.Fatal("rejected reset partially changed persistent state")
			}
		})
	}
}

func TestSubagentTotalLimitIncludesArchivesButDoesNotBlockResetReuse(t *testing.T) {
	ctx := context.Background()
	st, _, in := resetFixture(t)
	for i := 1; i < store.SubagentTotalLimit; i++ {
		id := fmt.Sprintf("old-%02d", i)
		completedSubtask(t, st, "parent", id)
		if err := st.SetSubtaskArchived(ctx, "parent", id, id+"-turn", true); err != nil { t.Fatal(err) }
	}
	dispatch := store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: "overflow", Child: &store.Session{ID: "overflow", Provider: "mock", Model: "model"}, Input: store.QueueInputInput{SessionID: "overflow", ClientMessageID: "overflow", Text: "work"}}
	if _, err := st.DispatchChild(ctx, dispatch); !errors.Is(err, store.ErrSubagentTotalLimit) {
		t.Fatalf("total limit ignored archives: %v", err)
	}
	in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "reuse", Text: "fresh work at capacity"}
	if _, err := st.ResetSubagent(ctx, in); err != nil {
		t.Fatalf("capacity blocked existing identity reset: %v", err)
	}
	if children, _ := st.ListChildSessions(ctx, "parent"); len(children) != store.SubagentTotalLimit {
		t.Fatalf("reset allocated or removed a child: %d", len(children))
	}
}

func TestSubagentResetConcurrentAdmissionConsumesOneSharedBudget(t *testing.T) {
	ctx := context.Background()
	st, _, first := resetFixture(t)
	for i := 1; i < 8; i++ { completedSubtask(t, st, "parent", fmt.Sprintf("child-%d", i)) }
	if _, err := st.CollectChildResults(ctx, "parent"); err != nil { t.Fatal(err) }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := first
			in.CallID = fmt.Sprintf("reset-%d", i)
			if i > 0 { in.SessionID = fmt.Sprintf("child-%d", i); in.ExpectedTurnID = in.SessionID + "-turn" }
			if _, err := st.ResetSubagent(ctx, in); err != nil && !errors.Is(err, store.ErrSubtaskLimit) { t.Error(err) }
		}()
	}
	wg.Wait()
	if n, err := st.SubagentFreshContextCount(ctx, "parent", "request"); err != nil || n != 3 {
		t.Fatalf("concurrent resets exceeded shared budget: %d %v", n, err)
	}
	id := "new-after-resets"
	_, err := st.DispatchChild(ctx, store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: "request", CallID: id, Child: &store.Session{ID: id, Provider: "mock", Model: "model"}, Input: store.QueueInputInput{SessionID: id, ClientMessageID: id, Text: "work"}})
	if !errors.Is(err, store.ErrSubtaskLimit) { t.Fatalf("dispatch ignored resets in shared budget: %v", err) }
}
