package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestCollaborationPersistence(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var st store.Store = memstore.New()
			if kind == "sqlite" {
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				st = db
			}
			newSession := func(id string) *store.Session {
				return &store.Session{ID: id, Title: id, Provider: "mock", Model: "model"}
			}
			for _, id := range []string{"parent", "other"} {
				if err := st.CreateSession(ctx, newSession(id)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "parent_turn", ClientMessageID: "parent_input", UserMessageID: "parent_message", UserText: "delegate"}); err != nil {
				t.Fatal(err)
			}
			input := store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: "parent_turn", CallID: "call", Child: newSession("child"), Input: store.QueueInputInput{SessionID: "child", ClientMessageID: "dispatch", Text: "do the work", Provider: "mock", Model: "model"}}
			first, err := st.DispatchChild(ctx, input)
			if err != nil || first.Duplicate || len(first.Events) != 2 {
				t.Fatalf("dispatch: %+v %v", first, err)
			}
			input.Child = newSession("should_not_exist")
			input.Input.SessionID = input.Child.ID
			replay, err := st.DispatchChild(ctx, input)
			if err != nil || !replay.Duplicate || replay.Session.ID != "child" {
				t.Fatalf("replay: %+v %v", replay, err)
			}
			if _, err := st.GetSession(ctx, "should_not_exist"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("duplicate created another child")
			}
			input.ParentSessionID = "other"
			if _, err := st.DispatchChild(ctx, input); err == nil {
				t.Fatal("cross-parent dispatch identity leaked")
			}
			promoted, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "child", TurnID: "child_turn", UserMessageID: "child_message"})
			if err != nil || promoted == nil {
				t.Fatalf("promote: %v", err)
			}
			finished, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "child_turn", Status: store.TurnCompleted, AssistantParts: []store.ContentPart{{Type: store.ContentPartText, Text: "version one"}}})
			if err != nil {
				t.Fatal(err)
			}
			if finished.CollaborationEvent == nil || finished.CollaborationEvent.SessionID != "parent" || finished.CollaborationEvent.Seq <= 0 {
				t.Fatalf("parent wake not durable: %+v", finished)
			}
			pending, err := st.HasUncollectedChildResults(ctx, "parent")
			if err != nil || !pending {
				t.Fatal("missing pending result")
			}
			events, err := st.CollectChildResults(ctx, "parent")
			if err != nil || len(events) != 1 {
				t.Fatalf("collect: %v %v", events, err)
			}
			msg, err := st.GetMessage(ctx, "parent", "collaboration_result_child_turn")
			if err != nil || !strings.Contains(msg.Text, "version one") {
				t.Fatalf("canonical result: %+v %v", msg, err)
			}
			if events, err := st.CollectChildResults(ctx, "parent"); err != nil || len(events) != 0 {
				t.Fatal("duplicate result collection")
			}
			// A new queued input invalidates the previous result; only its eventual turn is delivered.
			if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "revision", Text: "revise", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			if pending, _ := st.HasUncollectedChildResults(ctx, "parent"); pending {
				t.Fatal("old result redelivered")
			}
			input.ParentSessionID = "parent"
			input.CallID = "queued_call"
			input.Child = newSession("queued_child")
			input.Input.SessionID = input.Child.ID
			if _, err := st.DispatchChild(ctx, input); err != nil {
				t.Fatal(err)
			}
			if _, err := st.StopCollaboration(ctx, "parent"); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"child", "queued_child"} {
				queued, err := st.HasQueuedInputs(ctx, id)
				if err != nil || queued {
					t.Fatalf("queue not cancelled: %s %v", id, err)
				}
			}
			input.CallID = "late"
			input.Child = newSession("late_child")
			input.Input.SessionID = input.Child.ID
			if _, err := st.DispatchChild(ctx, input); !errors.Is(err, store.ErrCollaborationStopped) {
				t.Fatalf("late dispatch: %v", err)
			}
			if _, err := st.GetSession(ctx, "late_child"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("late dispatch orphaned a child")
			}
		})
	}
}
