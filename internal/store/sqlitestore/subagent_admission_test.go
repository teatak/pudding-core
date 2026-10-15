package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func seedSettledSubagent(t *testing.T, st *Store, parent, id string, status store.TurnStatus) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateChildSession(ctx, parent, &store.Session{ID: id, Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: id + "-turn", ClientMessageID: id, UserMessageID: id + "-input", UserText: "original task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id + "-turn", Status: status, AssistantParts: []store.ContentPart{{Type: store.ContentPartText, Text: "preserved result"}}}); err != nil {
		t.Fatal(err)
	}
}

func admissionDispatch(parent, turn, id string) store.DispatchChildInput {
	return store.DispatchChildInput{
		ParentSessionID: parent, ParentTurnID: turn, CallID: id,
		Child: &store.Session{ID: id, Provider: "mock", Model: "model"},
		Input: store.QueueInputInput{SessionID: id, ClientMessageID: id, Text: "independent task"},
	}
}

func TestSubagentTotalAdmissionIsAtomicAndCountsArchives(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "parent")
	// Seed historical work without consuming the current request's budget.
	for i := 0; i < store.SubagentTotalLimit-1; i++ {
		id := fmt.Sprintf("history-%02d", i)
		status := store.TurnCompleted
		if i == 0 {
			status = store.TurnFailed
		} else if i == 1 {
			status = store.TurnCancelled
		}
		seedSettledSubagent(t, st, "parent", id, status)
		if err := st.SetSubtaskArchived(ctx, "parent", id, id+"-turn", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "request", ClientMessageID: "request", UserMessageID: "request-input", UserText: "new request"}); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan string, 16)
	failures := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("candidate-%02d", i)
			_, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", id))
			if err == nil {
				accepted <- id
			} else if !errors.Is(err, store.ErrSubagentTotalLimit) {
				failures <- err
			}
		}(i)
	}
	wg.Wait()
	close(accepted)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var winners []string
	for id := range accepted {
		winners = append(winners, id)
	}
	if len(winners) != 1 {
		t.Fatalf("one remaining identity slot admitted %d children", len(winners))
	}
	children, err := st.ListChildSessions(ctx, "parent")
	if err != nil || len(children) != store.SubagentTotalLimit {
		t.Fatalf("total admission race: %d %v", len(children), err)
	}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("candidate-%02d", i)
		if id == winners[0] {
			continue
		}
		if _, err := st.GetSession(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rejected dispatch left a child session %s: %v", id, err)
		}
	}
	result, err := st.DispatchChild(ctx, admissionDispatch("parent", "request", winners[0]))
	if err != nil || !result.Duplicate || result.Session.ID != winners[0] {
		t.Fatalf("full group broke dispatch replay: %+v %v", result, err)
	}
	if err := st.SetSubtaskArchived(ctx, "parent", "history-00", "history-00-turn", false); err != nil {
		t.Fatalf("full group blocked restore: %v", err)
	}
	if _, err := st.QueueChildInput(ctx, "parent", "request", store.QueueInputInput{SessionID: "history-01", ClientMessageID: "reuse", Text: "related follow-up"}); err != nil {
		t.Fatalf("full group blocked reuse of an archived child: %v", err)
	}
	archives, err := st.ListSubtaskArchives(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := archives["history-01"]; exists {
		t.Fatal("send did not restore the archived child")
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "request", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "next", ClientMessageID: "next", UserMessageID: "next-input", UserText: "another request"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DispatchChild(ctx, admissionDispatch("parent", "next", "over-next")); !errors.Is(err, store.ErrSubagentTotalLimit) {
		t.Fatalf("a new user request reset the total capacity: %v", err)
	}
}

func TestSubagentTotalLimitIsPerParentAndPreservesExistingExcess(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	for _, parent := range []string{"existing", "other"} {
		createTestSession(t, st, parent)
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: parent, TurnID: parent + "-request", ClientMessageID: parent, UserMessageID: parent + "-input", UserText: "work"}); err != nil {
			t.Fatal(err)
		}
	}
	// Existing databases may exceed the new limit. Reading them must not prune
	// history, and their old children remain available for ordinary reuse.
	for i := 0; i < store.SubagentTotalLimit+2; i++ {
		seedSettledSubagent(t, st, "existing", fmt.Sprintf("old-%02d", i), store.TurnCompleted)
	}
	if _, err := st.DispatchChild(ctx, admissionDispatch("existing", "existing-request", "over")); !errors.Is(err, store.ErrSubagentTotalLimit) {
		t.Fatalf("existing excess admitted another child: %v", err)
	}
	if _, err := st.QueueChildInput(ctx, "existing", "existing-request", store.QueueInputInput{SessionID: "old-00", ClientMessageID: "reuse-old", Text: "related work"}); err != nil {
		t.Fatalf("existing excess blocked reuse: %v", err)
	}
	children, err := st.ListChildSessions(ctx, "existing")
	if err != nil || len(children) != store.SubagentTotalLimit+2 {
		t.Fatalf("existing excess was pruned: %d %v", len(children), err)
	}
	if _, err := st.DispatchChild(ctx, admissionDispatch("other", "other-request", "other-child")); err != nil {
		t.Fatalf("another parent's children consumed this parent's capacity: %v", err)
	}
}

func TestSubagentArchiveKeepsTerminalStatusAndMessages(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	createTestSession(t, st, "parent")
	for _, status := range []store.TurnStatus{store.TurnCompleted, store.TurnFailed, store.TurnCancelled} {
		id := string(status)
		seedSettledSubagent(t, st, "parent", id, status)
		before, err := st.ListMessages(ctx, id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSubtaskArchived(ctx, "parent", id, id+"-turn", true); err != nil {
			t.Fatalf("cannot archive settled %s: %v", status, err)
		}
		page, err := st.ListTurnsPage(ctx, id, "", 1)
		if err != nil || len(page.Turns) != 1 || page.Turns[0].Status != status {
			t.Fatalf("archive changed terminal status %s: %+v %v", status, page, err)
		}
		after, err := st.ListMessages(ctx, id, 0)
		if err != nil || len(before) != len(after) {
			t.Fatalf("archive lost history: %v", err)
		}
		for i := range before {
			if before[i].ID != after[i].ID || before[i].Text != after[i].Text {
				t.Fatal("archive rewrote canonical history")
			}
		}
	}
}
