package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestSessionDeletionAdmissionStores(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		for _, operation := range []string{"archive", "delete_parent", "delete_child"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				ctx := context.Background()
				var st store.Store = memstore.New()
				if kind == "sqlite" {
					db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "deletion.db"))
					if err != nil {
						t.Fatal(err)
					}
					st = db
					t.Cleanup(func() { db.Close() })
				}
				for _, id := range []string{"parent", "unrelated", "child", "sibling"} {
					session := &store.Session{ID: id, Title: id, Provider: "mock", Model: "m"}
					var err error
					if id == "child" || id == "sibling" {
						err = st.CreateChildSession(ctx, "parent", session)
					} else {
						err = st.CreateSession(ctx, session)
					}
					if err != nil {
						t.Fatal(err)
					}
					if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: id, ClientMessageID: "queued", Text: "later"}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := st.ArchiveSession(ctx, "child"); !errors.Is(err, store.ErrInvalidSessionRelation) {
					t.Fatalf("public archive accepted child: %v", err)
				}
				if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "running", ClientMessageID: "start", UserMessageID: "user", UserText: "work"}); err != nil {
					t.Fatal(err)
				}
				target := "parent"
				blocked := map[string]bool{"parent": true, "child": true, "sibling": true}
				if operation == "delete_child" {
					target = "child"
					blocked = map[string]bool{"child": true}
				}
				if operation == "archive" {
					if _, err := st.ArchiveSession(ctx, target); err != nil {
						t.Fatal(err)
					}
				} else {
					for i := 0; i < 2; i++ {
						if err := st.PrepareSessionDeletion(ctx, target); err != nil {
							t.Fatalf("prepare deletion attempt %d: %v", i, err)
						}
					}
				}
				for _, id := range []string{"parent", "child", "sibling", "unrelated"} {
					if !blocked[id] {
						if _, err := st.GetSession(ctx, id); err != nil {
							t.Fatalf("unrelated %s was closed: %v", id, err)
						}
						inputs, err := st.ListQueuedInputs(ctx, id)
						if err != nil || len(inputs) != 1 {
							t.Fatalf("unrelated %s queue changed: %+v %v", id, inputs, err)
						}
						continue
					}
					if _, err := st.GetSession(ctx, id); !errors.Is(err, store.ErrNotFound) {
						t.Fatalf("closed %s remains active: %v", id, err)
					}
					_, userErr := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: "late-user", ClientMessageID: "late-user", UserMessageID: "late-user", UserText: "late"})
					_, systemErr := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: id, TurnID: "late-system", ClientMessageID: "late-system", SystemMessageID: "late-system", Text: "late"})
					_, queueErr := st.QueueInput(ctx, store.QueueInputInput{SessionID: id, ClientMessageID: "late-queue", Text: "late"})
					_, promoteErr := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: id, TurnID: "late-promote", UserMessageID: "late-promote"})
					for entry, err := range map[string]error{"user": userErr, "system": systemErr, "queue": queueErr, "promotion": promoteErr} {
						if !errors.Is(err, store.ErrNotFound) {
							t.Fatalf("closed %s accepted %s admission: %v", id, entry, err)
						}
					}
				}
				// Preparation must leave the active turn available for canonical
				// finalization, even though all future admission is closed.
				if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "running", Status: store.TurnCancelled}); err != nil {
					t.Fatalf("preparation removed the running turn: %v", err)
				}
				if operation == "archive" {
					if _, err := st.RestoreSession(ctx, target); err != nil {
						t.Fatal(err)
					}
					for id := range blocked {
						if inputs, err := st.ListQueuedInputs(ctx, id); err != nil || len(inputs) != 0 {
							t.Fatalf("restored %s retained cancelled queue: %+v %v", id, inputs, err)
						}
					}
				} else {
					if err := st.DeleteSession(ctx, target); err != nil {
						t.Fatal(err)
					}
					for id := range blocked {
						if _, err := st.ParentSessionID(ctx, id); !errors.Is(err, store.ErrNotFound) {
							t.Fatalf("deleted member %s remains: %v", id, err)
						}
					}
				}
				if err := st.PrepareSessionDeletion(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("missing deletion preparation: %v", err)
				}
			})
		}
	}
}
