package sqlitestore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestCreateSessionOnceConcurrentAndRestart(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateProject(ctx, &store.Project{ID: "project", Name: "Project"}); err != nil {
		t.Fatal(err)
	}
	const count = 12
	ids := make(chan string, count)
	errs := make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s, err := st.CreateSessionOnce(ctx, "stable-request", &store.Session{Provider: "mock", Model: "model", ProjectID: "project"})
			if err != nil {
				errs <- err
				return
			}
			ids <- s.ID
		}()
	}
	workers.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatalf("different sessions: %s %s", id, got)
		}
		id = got
	}
	sessions, err := st.ListSessions(ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions: %v %v", sessions, err)
	}
	title := "renamed after creation"
	if _, err := st.UpdateSession(ctx, id, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: "turn", UserMessageID: "message", ClientMessageID: "first", UserText: "preserved", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	replay, err := st.CreateSessionOnce(ctx, "stable-request", &store.Session{Title: "overwrite", Provider: "other", Model: "different", ProjectID: "missing-project"})
	if err != nil || replay.ID != id || replay.Title != title || replay.ProjectID != "project" || replay.Model != "model" {
		t.Fatalf("replay changed canonical state: %+v %v", replay, err)
	}
	if _, err := st.GetConversationTurn(ctx, id, "turn"); err != nil {
		t.Fatalf("lost history: %v", err)
	}
	if _, err := st.ArchiveSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSessionOnce(ctx, "stable-request", &store.Session{Provider: "mock", Model: "model"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revived archive: %v", err)
	}
}

func TestCreateSessionOnceFailedCreationCanRetry(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	create := func() (*store.Session, error) {
		return st.CreateSessionOnce(ctx, "retryable", &store.Session{Provider: "mock", Model: "model", ProjectID: "project"})
	}
	if _, err := create(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	if err := st.CreateProject(ctx, &store.Project{ID: "project", Name: "Project"}); err != nil {
		t.Fatal(err)
	}
	first, err := create()
	if err != nil {
		t.Fatal(err)
	}
	second, err := create()
	if err != nil || first.ID != second.ID {
		t.Fatalf("retry: %+v %v", second, err)
	}
	other, err := st.CreateSessionOnce(ctx, "another-request", &store.Session{Provider: "mock", Model: "model"})
	if err != nil || other.ID == first.ID {
		t.Fatalf("independent request: %+v %v", other, err)
	}
}
