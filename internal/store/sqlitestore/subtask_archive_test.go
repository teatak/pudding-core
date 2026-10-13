package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func completedSubtask(t *testing.T, st *Store, parent, id string) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateChildSession(ctx, parent, &store.Session{ID: id, Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: id + "-turn", ClientMessageID: id, UserMessageID: id + "-message", UserText: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id + "-turn", Status: store.TurnCompleted, AssistantParts: []store.ContentPart{{Type: store.ContentPartText, Text: "result"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestSubtaskArchivePersistenceRetentionAndReuse(t *testing.T) {
	ctx := context.Background()
	st, path := openTestStore(t)
	createTestSession(t, st, "parent")
	createTestSession(t, st, "other")
	completedSubtask(t, st, "parent", "child")
	if err := st.SetSubtaskArchived(ctx, "other", "child", "child-turn", true); !errors.Is(err, store.ErrInvalidSessionRelation) {
		t.Fatalf("cross-parent: %v", err)
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "stale", true); !errors.Is(err, store.ErrSubtaskArchiveBusy) {
		t.Fatalf("stale snapshot: %v", err)
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	archives, err := st.ListSubtaskArchives(ctx, "parent")
	if err != nil || len(archives) != 1 {
		t.Fatalf("archive lost after restart: %v %v", archives, err)
	}
	if sess, err := st.GetSession(ctx, "child"); err != nil || sess.ArchivedAt != nil {
		t.Fatalf("child was placed in ordinary archive: %+v %v", sess, err)
	}
	if ids, err := st.ListExpiredArchivedSessionIDs(ctx, time.Now().Add(90*24*time.Hour)); err != nil || len(ids) != 0 {
		t.Fatalf("subtask entered retention: %v %v", ids, err)
	}
	if sessions, err := st.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListArchived}); err != nil || len(sessions) != 0 {
		t.Fatalf("subtask appeared in settings archive: %v %v", sessions, err)
	}
	if messages, err := st.ListMessages(ctx, "child", 0); err != nil || len(messages) != 2 {
		t.Fatalf("history lost: %d %v", len(messages), err)
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "followup", Text: "more work"}); err != nil {
		t.Fatal(err)
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "parent"); len(archives) != 0 {
		t.Fatal("new work did not restore the child")
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "child", "child-turn", true); !errors.Is(err, store.ErrSubtaskArchiveBusy) {
		t.Fatalf("queued work archived: %v", err)
	}
	completedSubtask(t, st, "parent", "archived-child")
	if err := st.SetSubtaskArchived(ctx, "parent", "archived-child", "archived-child-turn", true); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if archives, _ := st.ListSubtaskArchives(ctx, "parent"); len(archives) != 0 {
		t.Fatal("deleted group retained subtask archives")
	}
	for _, id := range []string{"parent", "child", "archived-child"} {
		if _, err := st.ParentSessionID(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("retained %s: %v", id, err)
		}
	}
}

func TestSubtaskBudgetSurvivesArchiveContinuationRetryAndRestart(t *testing.T) {
	ctx := context.Background()
	st, path := openTestStore(t)
	createTestSession(t, st, "parent")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-message", UserText: "work"}); err != nil {
		t.Fatal(err)
	}
	dispatch := func(turn, id string) (*store.DispatchChildResult, error) {
		return st.DispatchChild(ctx, store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: turn, CallID: id, Child: &store.Session{ID: id, Provider: "mock", Model: "model"}, Input: store.QueueInputInput{SessionID: id, ClientMessageID: id, Text: "work"}})
	}
	for i := 0; i < store.SubtaskDispatchLimit; i++ {
		if _, err := dispatch("request", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := dispatch("request", "0"); err != nil || !result.Duplicate {
		t.Fatalf("idempotent dispatch at limit: %+v %v", result, err)
	}
	if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "0", TurnID: "child-turn", UserMessageID: "child-message"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "child-turn", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "0", "child-turn", true); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch("request", "over"); !errors.Is(err, store.ErrSubtaskLimit) {
		t.Fatalf("archiving refunded budget: %v", err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "request", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	var err error
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, turn := range []string{"wake", "retry", "answer"} {
		if turn == "answer" {
			_, err = st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: turn, ClientMessageID: turn, UserMessageID: turn + "-message", UserText: "answer", UserParts: []store.ContentPart{{Type: store.ContentPartFormResult, Title: "Question", Schema: json.RawMessage(`{"type":"object"}`), Result: json.RawMessage(`{"answer":"yes"}`)}}})
		} else {
			retry := ""
			if turn == "retry" {
				retry = "wake"
			}
			_, err = st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "parent", TurnID: turn, ClientMessageID: turn, SystemMessageID: turn + "-message", Text: "continue", RetryOfTurnID: retry})
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dispatch(turn, "over-"+turn); !errors.Is(err, store.ErrSubtaskLimit) {
			t.Fatalf("%s reset budget: %v", turn, err)
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: turn, Status: store.TurnFailed}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "text-answer", ClientMessageID: "input-flow-question", UserMessageID: "text-answer-message", UserText: "yes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch("text-answer", "over-text-answer"); !errors.Is(err, store.ErrSubtaskLimit) {
		t.Fatalf("text-only question reply reset budget: %v", err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "text-answer", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "next", ClientMessageID: "next", UserMessageID: "next-message", UserText: "new request"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch("next", "next-child"); err != nil {
		t.Fatalf("new user request did not reset budget: %v", err)
	}
}

func TestSubtaskDispatchBudgetIsAtomic(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "parent")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "turn", ClientMessageID: "input", UserMessageID: "message", UserText: "work"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			id := fmt.Sprint(i)
			_, err := st.DispatchChild(ctx, store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: "turn", CallID: id, Child: &store.Session{ID: id, Provider: "mock", Model: "model"}, Input: store.QueueInputInput{SessionID: id, ClientMessageID: id, Text: "work"}})
			if err != nil && !errors.Is(err, store.ErrSubtaskLimit) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if children, err := st.ListChildSessions(ctx, "parent"); err != nil || len(children) != store.SubtaskDispatchLimit {
		t.Fatalf("budget race: %d %v", len(children), err)
	}
}
